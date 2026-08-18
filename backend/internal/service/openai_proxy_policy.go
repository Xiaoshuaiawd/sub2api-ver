package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

const DefaultOpenAIDefaultProxyURL = "socks5h://warp-proxy:1080"

const defaultOpenAIProxySnapshotTTL = 30 * time.Second

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
	settings  OpenAIProxySettings
	proxies   map[int64]Proxy
	byURL     map[string]int64
	expiresAt time.Time
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
	service := &OpenAIProxyPolicyService{
		settingRepo: settingRepo,
		proxyRepo:   proxyRepo,
		bus:         bus,
		ttl:         ttl,
		now:         time.Now,
	}
	service.snapshot.Store(buildOpenAIProxySnapshot(DefaultOpenAIProxySettings(), nil, time.Now().Add(ttl)))
	return service
}

func NewOpenAIProxyPolicyService(settingRepo SettingRepository, proxyRepo ProxyRepository, bus OpenAIProxySettingsBus) *OpenAIProxyPolicyService {
	service := newOpenAIProxyPolicyService(settingRepo, proxyRepo, bus, defaultOpenAIProxySnapshotTTL)
	if err := service.Refresh(context.Background()); err != nil {
		slog.Warn("load OpenAI proxy policy failed; retaining safe defaults", "error", err)
	}
	if bus != nil {
		ctx, cancel := context.WithCancel(context.Background())
		service.cancel = cancel
		bus.Subscribe(ctx, func() {
			if err := service.Refresh(context.Background()); err != nil {
				slog.Warn("refresh OpenAI proxy policy after notification failed", "error", err)
			}
		})
	}
	return service
}

func (s *OpenAIProxyPolicyService) Stop() {
	if s != nil && s.cancel != nil {
		s.cancel()
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

	now := s.now()
	s.snapshot.Store(buildOpenAIProxySnapshot(parseOpenAIProxySettings(values), proxies, now.Add(s.ttl)))
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
		return buildOpenAIProxySnapshot(DefaultOpenAIProxySettings(), nil, time.Time{}).resolve(primaryProxyURL, time.Now())
	}
	snapshot := s.snapshot.Load()
	if snapshot == nil {
		snapshot = buildOpenAIProxySnapshot(DefaultOpenAIProxySettings(), nil, s.now().Add(s.ttl))
		s.snapshot.Store(snapshot)
	}
	if !snapshot.expiresAt.After(s.now()) {
		if err := s.Refresh(ctx); err != nil {
			slog.Warn("refresh expired OpenAI proxy policy failed; retaining last snapshot", "error", err)
		}
		snapshot = s.snapshot.Load()
	}
	return snapshot.resolve(primaryProxyURL, s.now())
}

func buildOpenAIProxySnapshot(settings OpenAIProxySettings, proxies []Proxy, expiresAt time.Time) *openAIProxySnapshot {
	snapshot := &openAIProxySnapshot{
		settings:  settings,
		proxies:   make(map[int64]Proxy, len(proxies)),
		byURL:     make(map[string]int64, len(proxies)),
		expiresAt: expiresAt,
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
