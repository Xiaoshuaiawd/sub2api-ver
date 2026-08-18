package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
)

const DefaultOpenAIDefaultProxyURL = "socks5h://warp-proxy:1080"

const (
	defaultOpenAIProxySnapshotTTL    = 30 * time.Second
	defaultOpenAIProxyHealthInterval = 30 * time.Second
	defaultOpenAIProxyHealthTimeout  = 10 * time.Second
	defaultOpenAIProxyHealthTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"
	maxOpenAIProxyHealthBodyBytes    = 8 << 10
)

var ErrOpenAIProxyRequestNotReplayable = errors.New("OpenAI proxy fallback requires a replayable request body")

type OpenAIProxyFailurePolicy string

const (
	OpenAIProxyFailurePolicyFailClosed     OpenAIProxyFailurePolicy = "fail_closed"
	OpenAIProxyFailurePolicyFallbackDirect OpenAIProxyFailurePolicy = "fallback_direct"
)

type OpenAIProxySettings struct {
	Enabled       bool
	ProxyURL      string
	FailurePolicy OpenAIProxyFailurePolicy
}

type OpenAIProxyCandidateSource string

const (
	OpenAIProxyCandidateAccount OpenAIProxyCandidateSource = "account"
	OpenAIProxyCandidateBackup  OpenAIProxyCandidateSource = "backup"
	OpenAIProxyCandidateNode    OpenAIProxyCandidateSource = "node"
	OpenAIProxyCandidateDirect  OpenAIProxyCandidateSource = "direct"
)

type OpenAIProxyCandidate struct {
	URL    string
	Source OpenAIProxyCandidateSource
}

type OpenAIProxyPlan struct {
	Candidates []OpenAIProxyCandidate
}

type OpenAIProxyTransport string

const (
	OpenAIProxyTransportHTTP      OpenAIProxyTransport = "http"
	OpenAIProxyTransportWebSocket OpenAIProxyTransport = "websocket"
)

type OpenAIProxyAttemptsBySource struct {
	Account uint64 `json:"account"`
	Backup  uint64 `json:"backup"`
	Node    uint64 `json:"node"`
	Direct  uint64 `json:"direct"`
}

type OpenAIProxyMetricsSnapshot struct {
	AttemptsBySource           OpenAIProxyAttemptsBySource `json:"attempts_by_source"`
	CandidateSwitches          uint64                      `json:"candidate_switches"`
	FailClosedExhaustions      uint64                      `json:"fail_closed_exhaustions"`
	DirectFallbacks            uint64                      `json:"direct_fallbacks"`
	HTTPTransportFailures      uint64                      `json:"http_transport_failures"`
	WebSocketTransportFailures uint64                      `json:"websocket_transport_failures"`
}

type OpenAIProxyHealthStatus struct {
	InstanceID string                     `json:"instance_id"`
	Healthy    bool                       `json:"healthy"`
	EgressIP   string                     `json:"egress_ip"`
	CheckedAt  time.Time                  `json:"checked_at"`
	Error      string                     `json:"error"`
	Metrics    OpenAIProxyMetricsSnapshot `json:"metrics"`
}

type OpenAIProxyMetricsRecorder interface {
	RecordAttempt(source OpenAIProxyCandidateSource)
	RecordCandidateSwitch()
	RecordFailClosedExhaustion()
	RecordDirectFallback()
	RecordTransportFailure(transport OpenAIProxyTransport)
}

type openAIProxyMetrics struct {
	accountAttempts            atomic.Uint64
	backupAttempts             atomic.Uint64
	nodeAttempts               atomic.Uint64
	directAttempts             atomic.Uint64
	candidateSwitches          atomic.Uint64
	failClosedExhaustions      atomic.Uint64
	directFallbacks            atomic.Uint64
	httpTransportFailures      atomic.Uint64
	webSocketTransportFailures atomic.Uint64
}

func (p OpenAIProxyPlan) URLs() []string {
	urls := make([]string, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		urls = append(urls, candidate.URL)
	}
	return urls
}

func (p OpenAIProxyPlan) Sources() []OpenAIProxyCandidateSource {
	sources := make([]OpenAIProxyCandidateSource, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		sources = append(sources, candidate.Source)
	}
	return sources
}

func (p OpenAIProxyPlan) IsFailClosed() bool {
	hasNode := false
	for _, candidate := range p.Candidates {
		switch candidate.Source {
		case OpenAIProxyCandidateNode:
			hasNode = true
		case OpenAIProxyCandidateDirect:
			return false
		}
	}
	return hasNode
}

type OpenAIProxyPolicyProvider interface {
	Resolve(ctx context.Context, primaryProxyURL string) OpenAIProxyPlan
}

type OpenAIProxySettingsBus interface {
	Publish(ctx context.Context) error
	Subscribe(ctx context.Context, handler func())
}

type openAIProxySettingReader interface {
	GetMultiple(ctx context.Context, keys []string) (map[string]string, error)
}

type openAIProxyReader interface {
	ListAllForFallback(ctx context.Context) ([]Proxy, error)
}

type openAIProxySnapshot struct {
	settings OpenAIProxySettings
	proxies  map[int64]Proxy
	byURL    map[string]int64
}

type OpenAIProxyPolicyService struct {
	settingRepo openAIProxySettingReader
	proxyRepo   openAIProxyReader
	bus         OpenAIProxySettingsBus
	ttl         time.Duration
	now         func() time.Time

	snapshot  atomic.Pointer[openAIProxySnapshot]
	refreshMu sync.Mutex
	cancel    context.CancelFunc

	instanceID          string
	healthTraceURL      string
	healthInterval      time.Duration
	healthTimeout       time.Duration
	healthClientFactory func(proxyURL string) (*http.Client, error)
	healthMu            sync.RWMutex
	health              OpenAIProxyHealthStatus
	healthWG            sync.WaitGroup
	refreshWG           sync.WaitGroup
	metrics             openAIProxyMetrics
}

func DefaultOpenAIProxySettings() OpenAIProxySettings {
	return OpenAIProxySettings{
		Enabled:       true,
		ProxyURL:      DefaultOpenAIDefaultProxyURL,
		FailurePolicy: OpenAIProxyFailurePolicyFailClosed,
	}
}

func NormalizeOpenAIProxySettings(value OpenAIProxySettings) (OpenAIProxySettings, error) {
	proxyURL, _, err := proxyurl.Parse(value.ProxyURL)
	if err != nil {
		return OpenAIProxySettings{}, fmt.Errorf("invalid OpenAI default proxy URL: %w", err)
	}
	if value.Enabled && proxyURL == "" {
		return OpenAIProxySettings{}, fmt.Errorf("OpenAI default proxy URL is required when proxying is enabled")
	}
	if value.FailurePolicy != OpenAIProxyFailurePolicyFailClosed && value.FailurePolicy != OpenAIProxyFailurePolicyFallbackDirect {
		return OpenAIProxySettings{}, fmt.Errorf("invalid OpenAI proxy failure policy %q", value.FailurePolicy)
	}
	value.ProxyURL = proxyURL
	return value, nil
}

func parseOpenAIProxySettings(settings map[string]string) OpenAIProxySettings {
	value := DefaultOpenAIProxySettings()
	if raw, ok := settings[SettingKeyOpenAIDefaultProxyEnabled]; ok {
		if enabled, err := strconv.ParseBool(strings.TrimSpace(raw)); err == nil {
			value.Enabled = enabled
		}
	}
	if raw, ok := settings[SettingKeyOpenAIDefaultProxyURL]; ok {
		value.ProxyURL = raw
	}
	if raw, ok := settings[SettingKeyOpenAIDefaultProxyFailurePolicy]; ok {
		value.FailurePolicy = OpenAIProxyFailurePolicy(strings.TrimSpace(raw))
	}

	normalized, err := NormalizeOpenAIProxySettings(value)
	if err != nil {
		return DefaultOpenAIProxySettings()
	}
	return normalized
}

func newOpenAIProxyPolicyService(settingRepo openAIProxySettingReader, proxyRepo openAIProxyReader, bus OpenAIProxySettingsBus, ttl time.Duration) *OpenAIProxyPolicyService {
	if ttl <= 0 {
		ttl = defaultOpenAIProxySnapshotTTL
	}
	instanceID, _ := os.Hostname()
	service := &OpenAIProxyPolicyService{
		settingRepo:         settingRepo,
		proxyRepo:           proxyRepo,
		bus:                 bus,
		ttl:                 ttl,
		now:                 time.Now,
		instanceID:          instanceID,
		healthTraceURL:      defaultOpenAIProxyHealthTraceURL,
		healthInterval:      defaultOpenAIProxyHealthInterval,
		healthTimeout:       defaultOpenAIProxyHealthTimeout,
		healthClientFactory: newOpenAIProxyHealthClient,
	}
	service.health = OpenAIProxyHealthStatus{InstanceID: instanceID}
	service.snapshot.Store(buildOpenAIProxySnapshot(DefaultOpenAIProxySettings(), nil))
	return service
}

func NewOpenAIProxyPolicyService(settingRepo SettingRepository, proxyRepo ProxyRepository, bus OpenAIProxySettingsBus) *OpenAIProxyPolicyService {
	service := newOpenAIProxyPolicyService(settingRepo, proxyRepo, bus, defaultOpenAIProxySnapshotTTL)
	if err := service.Refresh(context.Background()); err != nil {
		slog.Warn("load OpenAI proxy policy failed; retaining safe defaults", "error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	service.cancel = cancel
	if bus != nil {
		bus.Subscribe(ctx, func() {
			if err := service.Refresh(context.Background()); err != nil {
				slog.Warn("refresh OpenAI proxy policy after notification failed", "error", err)
			}
		})
	}
	service.startSnapshotRefreshWorker(ctx)
	service.startHealthWorker(ctx)
	return service
}

func (s *OpenAIProxyPolicyService) Stop() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.refreshWG.Wait()
	s.healthWG.Wait()
}

func RedactOpenAIProxyURL(raw string) string {
	_, parsed, err := proxyurl.Parse(raw)
	if err != nil || parsed == nil {
		return ""
	}
	return parsed.Redacted()
}

func newOpenAIProxyHealthClient(rawProxyURL string) (*http.Client, error) {
	_, parsed, err := proxyurl.Parse(rawProxyURL)
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, errors.New("node proxy is not configured")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport, Timeout: defaultOpenAIProxyHealthTimeout}, nil
}

func (s *OpenAIProxyPolicyService) startSnapshotRefreshWorker(ctx context.Context) {
	if s == nil || s.ttl <= 0 {
		return
	}
	s.refreshWG.Add(1)
	go func() {
		defer s.refreshWG.Done()
		ticker := time.NewTicker(s.ttl)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
					slog.Warn("refresh OpenAI proxy policy snapshot failed; retaining last snapshot", "error", err)
				}
			}
		}
	}()
}

func (s *OpenAIProxyPolicyService) startHealthWorker(ctx context.Context) {
	if s == nil || s.healthInterval <= 0 {
		return
	}
	s.healthWG.Add(1)
	go func() {
		defer s.healthWG.Done()
		s.checkHealth(ctx)
		ticker := time.NewTicker(s.healthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkHealth(ctx)
			}
		}
	}()
}

func (s *OpenAIProxyPolicyService) checkHealth(ctx context.Context) OpenAIProxyHealthStatus {
	status := OpenAIProxyHealthStatus{InstanceID: s.instanceID, CheckedAt: s.now().UTC()}
	snapshot := s.snapshot.Load()
	proxyURL := ""
	if snapshot != nil {
		proxyURL = snapshot.settings.ProxyURL
	}
	if strings.TrimSpace(proxyURL) == "" {
		return s.storeHealthStatus(status, "node proxy is not configured", proxyURL)
	}
	clientFactory := s.healthClientFactory
	if clientFactory == nil {
		clientFactory = newOpenAIProxyHealthClient
	}
	client, err := clientFactory(proxyURL)
	if err != nil {
		return s.storeHealthStatus(status, "node proxy health client could not be created", proxyURL)
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, s.healthTraceURL, nil)
	if err != nil {
		return s.storeHealthStatus(status, "node proxy health request could not be created", proxyURL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return s.storeHealthStatus(status, "node proxy health request failed", proxyURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return s.storeHealthStatus(status, fmt.Sprintf("health endpoint returned HTTP %d", resp.StatusCode), proxyURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenAIProxyHealthBodyBytes+1))
	if err != nil || len(body) > maxOpenAIProxyHealthBodyBytes {
		return s.storeHealthStatus(status, "node proxy health response could not be read", proxyURL)
	}
	egressIP, warpEnabled := parseOpenAIProxyTrace(string(body))
	if !warpEnabled || net.ParseIP(egressIP) == nil {
		return s.storeHealthStatus(status, "health response did not confirm WARP", proxyURL)
	}
	status.Healthy = true
	status.EgressIP = egressIP
	return s.storeHealthStatus(status, "", proxyURL)
}

func parseOpenAIProxyTrace(body string) (egressIP string, warpEnabled bool) {
	for _, line := range strings.Split(body, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ip":
			egressIP = strings.TrimSpace(value)
		case "warp":
			value = strings.TrimSpace(value)
			warpEnabled = value == "on" || value == "plus"
		}
	}
	return egressIP, warpEnabled
}

func (s *OpenAIProxyPolicyService) storeHealthStatus(status OpenAIProxyHealthStatus, errorMessage, proxyURL string) OpenAIProxyHealthStatus {
	status.Error = errorMessage
	status.Metrics = s.Metrics()
	s.healthMu.Lock()
	s.health = status
	s.healthMu.Unlock()
	if status.Healthy {
		slog.Debug("OpenAI node proxy health check passed", "instance_id", status.InstanceID, "proxy", RedactOpenAIProxyURL(proxyURL), "egress_ip", status.EgressIP)
	} else {
		slog.Warn("OpenAI node proxy health check failed", "instance_id", status.InstanceID, "proxy", RedactOpenAIProxyURL(proxyURL), "error", errorMessage)
	}
	return status
}

func (s *OpenAIProxyPolicyService) Status() OpenAIProxyHealthStatus {
	if s == nil {
		return OpenAIProxyHealthStatus{}
	}
	s.healthMu.RLock()
	status := s.health
	s.healthMu.RUnlock()
	status.Metrics = s.Metrics()
	return status
}

func (s *OpenAIProxyPolicyService) RecordAttempt(source OpenAIProxyCandidateSource) {
	if s == nil {
		return
	}
	switch source {
	case OpenAIProxyCandidateAccount:
		s.metrics.accountAttempts.Add(1)
	case OpenAIProxyCandidateBackup:
		s.metrics.backupAttempts.Add(1)
	case OpenAIProxyCandidateNode:
		s.metrics.nodeAttempts.Add(1)
	case OpenAIProxyCandidateDirect:
		s.metrics.directAttempts.Add(1)
	}
}

func (s *OpenAIProxyPolicyService) RecordCandidateSwitch() {
	if s != nil {
		s.metrics.candidateSwitches.Add(1)
	}
}

func (s *OpenAIProxyPolicyService) RecordFailClosedExhaustion() {
	if s != nil {
		s.metrics.failClosedExhaustions.Add(1)
	}
}

func (s *OpenAIProxyPolicyService) RecordDirectFallback() {
	if s != nil {
		s.metrics.directFallbacks.Add(1)
	}
}

func (s *OpenAIProxyPolicyService) RecordTransportFailure(transport OpenAIProxyTransport) {
	if s == nil {
		return
	}
	switch transport {
	case OpenAIProxyTransportHTTP:
		s.metrics.httpTransportFailures.Add(1)
	case OpenAIProxyTransportWebSocket:
		s.metrics.webSocketTransportFailures.Add(1)
	}
}

func (s *OpenAIProxyPolicyService) Metrics() OpenAIProxyMetricsSnapshot {
	if s == nil {
		return OpenAIProxyMetricsSnapshot{}
	}
	return OpenAIProxyMetricsSnapshot{
		AttemptsBySource: OpenAIProxyAttemptsBySource{
			Account: s.metrics.accountAttempts.Load(),
			Backup:  s.metrics.backupAttempts.Load(),
			Node:    s.metrics.nodeAttempts.Load(),
			Direct:  s.metrics.directAttempts.Load(),
		},
		CandidateSwitches:          s.metrics.candidateSwitches.Load(),
		FailClosedExhaustions:      s.metrics.failClosedExhaustions.Load(),
		DirectFallbacks:            s.metrics.directFallbacks.Load(),
		HTTPTransportFailures:      s.metrics.httpTransportFailures.Load(),
		WebSocketTransportFailures: s.metrics.webSocketTransportFailures.Load(),
	}
}

func (s *OpenAIProxyPolicyService) Refresh(ctx context.Context) error {
	if s == nil || s.settingRepo == nil || s.proxyRepo == nil {
		return fmt.Errorf("OpenAI proxy policy repositories are not configured")
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyOpenAIDefaultProxyEnabled,
		SettingKeyOpenAIDefaultProxyURL,
		SettingKeyOpenAIDefaultProxyFailurePolicy,
	})
	if err != nil {
		return fmt.Errorf("load OpenAI proxy settings: %w", err)
	}
	proxies, err := s.proxyRepo.ListAllForFallback(ctx)
	if err != nil {
		return fmt.Errorf("load OpenAI proxy fallback chain: %w", err)
	}

	s.snapshot.Store(buildOpenAIProxySnapshot(parseOpenAIProxySettings(values), proxies))
	return nil
}

func (s *OpenAIProxyPolicyService) SettingsUpdated(ctx context.Context) {
	if s == nil {
		return
	}
	if err := s.Refresh(ctx); err != nil {
		slog.Warn("refresh local OpenAI proxy policy after settings update failed", "error", err)
	}
	if s.bus != nil {
		if err := s.bus.Publish(ctx); err != nil {
			slog.Warn("publish OpenAI proxy policy update failed", "error", err)
		}
	}
}

func (s *OpenAIProxyPolicyService) Resolve(ctx context.Context, primaryProxyURL string) OpenAIProxyPlan {
	if s == nil {
		return buildOpenAIProxySnapshot(DefaultOpenAIProxySettings(), nil).resolve(primaryProxyURL, time.Now())
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil {
		snapshot = buildOpenAIProxySnapshot(DefaultOpenAIProxySettings(), nil)
		s.snapshot.Store(snapshot)
	}
	return snapshot.resolve(primaryProxyURL, s.now())
}

func buildOpenAIProxySnapshot(settings OpenAIProxySettings, proxies []Proxy) *openAIProxySnapshot {
	snapshot := &openAIProxySnapshot{
		settings: settings,
		proxies:  make(map[int64]Proxy, len(proxies)),
		byURL:    make(map[string]int64, len(proxies)),
	}
	for _, configuredProxy := range proxies {
		snapshot.proxies[configuredProxy.ID] = configuredProxy
		normalizedURL, _, err := proxyurl.Parse(configuredProxy.URL())
		if err != nil || normalizedURL == "" {
			continue
		}
		if _, exists := snapshot.byURL[normalizedURL]; !exists {
			snapshot.byURL[normalizedURL] = configuredProxy.ID
		}
	}
	return snapshot
}

func (s *openAIProxySnapshot) resolve(primaryProxyURL string, now time.Time) OpenAIProxyPlan {
	if !s.settings.Enabled {
		if strings.TrimSpace(primaryProxyURL) == "" {
			return OpenAIProxyPlan{Candidates: []OpenAIProxyCandidate{{Source: OpenAIProxyCandidateDirect}}}
		}
		normalized, _, err := proxyurl.Parse(primaryProxyURL)
		if err != nil {
			normalized = strings.TrimSpace(primaryProxyURL)
		}
		return OpenAIProxyPlan{Candidates: []OpenAIProxyCandidate{{URL: normalized, Source: OpenAIProxyCandidateAccount}}}
	}

	plan := OpenAIProxyPlan{}
	seenURLs := make(map[string]struct{})
	appendCandidate := func(rawURL string, source OpenAIProxyCandidateSource) string {
		if source == OpenAIProxyCandidateDirect {
			if _, exists := seenURLs[""]; !exists {
				seenURLs[""] = struct{}{}
				plan.Candidates = append(plan.Candidates, OpenAIProxyCandidate{Source: source})
			}
			return ""
		}
		normalized, _, err := proxyurl.Parse(rawURL)
		if err != nil || normalized == "" {
			return ""
		}
		if _, exists := seenURLs[normalized]; exists {
			return normalized
		}
		seenURLs[normalized] = struct{}{}
		plan.Candidates = append(plan.Candidates, OpenAIProxyCandidate{URL: normalized, Source: source})
		return normalized
	}

	primaryURL := appendCandidate(primaryProxyURL, OpenAIProxyCandidateAccount)
	if primaryID, ok := s.byURL[primaryURL]; ok {
		primary := s.proxies[primaryID]
		if primary.FallbackMode == FallbackModeProxy {
			visited := map[int64]struct{}{primary.ID: {}}
			currentID := primary.BackupProxyID
			for currentID != nil {
				if _, exists := visited[*currentID]; exists {
					break
				}
				configuredProxy, exists := s.proxies[*currentID]
				if !exists {
					break
				}
				visited[configuredProxy.ID] = struct{}{}
				if configuredProxy.Status == StatusActive && !configuredProxy.IsExpired(now) {
					appendCandidate(configuredProxy.URL(), OpenAIProxyCandidateBackup)
				}
				if configuredProxy.FallbackMode != FallbackModeProxy {
					break
				}
				currentID = configuredProxy.BackupProxyID
			}
		}
	}

	appendCandidate(s.settings.ProxyURL, OpenAIProxyCandidateNode)
	if s.settings.FailurePolicy == OpenAIProxyFailurePolicyFallbackDirect {
		appendCandidate("", OpenAIProxyCandidateDirect)
	}
	return plan
}
