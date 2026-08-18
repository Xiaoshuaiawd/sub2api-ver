//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIProxySettings(t *testing.T) {
	got, err := NormalizeOpenAIProxySettings(OpenAIProxySettings{
		Enabled:       true,
		ProxyURL:      " socks5://warp-proxy:1080 ",
		FailurePolicy: OpenAIProxyFailurePolicyFallbackDirect,
	})

	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "socks5h://warp-proxy:1080", got.ProxyURL)
	require.Equal(t, OpenAIProxyFailurePolicyFallbackDirect, got.FailurePolicy)
}

func TestOpenAIProxyPolicyResolveOrdersAndDeduplicatesCandidates(t *testing.T) {
	now := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	backupID := int64(2)
	policy := newOpenAIProxyPolicyForTest(OpenAIProxySettings{
		Enabled:       true,
		ProxyURL:      DefaultOpenAIDefaultProxyURL,
		FailurePolicy: OpenAIProxyFailurePolicyFallbackDirect,
	}, []Proxy{
		{ID: 1, Protocol: "http", Host: "primary", Port: 8080, Status: StatusActive, FallbackMode: FallbackModeProxy, BackupProxyID: &backupID},
		{ID: 2, Protocol: "socks5", Host: "backup", Port: 1080, Status: StatusActive},
	}, now)

	plan := policy.Resolve(context.Background(), "http://primary:8080")

	require.Equal(t, []string{
		"http://primary:8080",
		"socks5h://backup:1080",
		DefaultOpenAIDefaultProxyURL,
		"",
	}, plan.URLs())
	require.Equal(t, []OpenAIProxyCandidateSource{
		OpenAIProxyCandidateAccount,
		OpenAIProxyCandidateBackup,
		OpenAIProxyCandidateNode,
		OpenAIProxyCandidateDirect,
	}, plan.Sources())
}

func newOpenAIProxyPolicyForTest(settings OpenAIProxySettings, proxies []Proxy, now time.Time) *OpenAIProxyPolicyService {
	policy := newOpenAIProxyPolicyService(nil, nil, nil, time.Hour)
	policy.now = func() time.Time { return now }
	policy.snapshot.Store(buildOpenAIProxySnapshot(settings, proxies))
	return policy
}

func TestOpenAIProxyPolicyResolveUsesNodeProxyWithoutAccountProxy(t *testing.T) {
	policy := newOpenAIProxyPolicyForTest(DefaultOpenAIProxySettings(), nil, time.Now())

	plan := policy.Resolve(context.Background(), "")

	require.Equal(t, []string{DefaultOpenAIDefaultProxyURL}, plan.URLs())
}

func TestOpenAIProxyPolicyResolveDisabledPreservesExistingRouting(t *testing.T) {
	settings := DefaultOpenAIProxySettings()
	settings.Enabled = false
	policy := newOpenAIProxyPolicyForTest(settings, nil, time.Now())

	require.Equal(t, []string{"http://account:8080"}, policy.Resolve(context.Background(), "http://account:8080").URLs())
	require.Equal(t, []string{""}, policy.Resolve(context.Background(), "").URLs())
}

func TestOpenAIProxyPolicyResolveFailClosedNeverAppendsDirect(t *testing.T) {
	policy := newOpenAIProxyPolicyForTest(DefaultOpenAIProxySettings(), nil, time.Now())

	plan := policy.Resolve(context.Background(), "http://account:8080")

	require.Equal(t, []string{"http://account:8080", DefaultOpenAIDefaultProxyURL}, plan.URLs())
}

func TestOpenAIProxyPolicyResolveSkipsUnavailableBackupsAndStopsCycles(t *testing.T) {
	now := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-time.Minute)
	id2, id3, id4 := int64(2), int64(3), int64(4)
	policy := newOpenAIProxyPolicyForTest(DefaultOpenAIProxySettings(), []Proxy{
		{ID: 1, Protocol: "http", Host: "primary", Port: 8080, Status: StatusActive, FallbackMode: FallbackModeProxy, BackupProxyID: &id2},
		{ID: 2, Protocol: "http", Host: "expired", Port: 8080, Status: StatusActive, ExpiresAt: &expiredAt, FallbackMode: FallbackModeProxy, BackupProxyID: &id3},
		{ID: 3, Protocol: "http", Host: "disabled", Port: 8080, Status: StatusDisabled, FallbackMode: FallbackModeProxy, BackupProxyID: &id4},
		{ID: 4, Protocol: "http", Host: "healthy", Port: 8080, Status: StatusActive, FallbackMode: FallbackModeProxy, BackupProxyID: &id2},
	}, now)

	plan := policy.Resolve(context.Background(), "http://primary:8080")

	require.Equal(t, []string{"http://primary:8080", "http://healthy:8080", DefaultOpenAIDefaultProxyURL}, plan.URLs())
}

func TestOpenAIProxyPolicyResolveDeduplicatesNodeProxy(t *testing.T) {
	policy := newOpenAIProxyPolicyForTest(DefaultOpenAIProxySettings(), nil, time.Now())

	plan := policy.Resolve(context.Background(), "socks5://warp-proxy:1080")

	require.Equal(t, []string{DefaultOpenAIDefaultProxyURL}, plan.URLs())
}

type openAIProxySettingRepoStub struct {
	SettingRepository
	values map[string]string
	err    error
	calls  atomic.Int64
}

func (s *openAIProxySettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	s.calls.Add(1)
	return s.values, s.err
}

type openAIProxyRepoStub struct {
	ProxyRepository
	values []Proxy
	err    error
	calls  int
}

type openAIProxySettingsBusStub struct {
	publishCalls int
	publishErr   error
	handler      func()
}

func (s *openAIProxySettingsBusStub) Publish(context.Context) error {
	s.publishCalls++
	return s.publishErr
}

func (s *openAIProxySettingsBusStub) Subscribe(_ context.Context, handler func()) {
	s.handler = handler
}

type openAIProxyWritableSettingRepo struct {
	SettingRepository
	values map[string]string
	getErr error
}

func (s *openAIProxyWritableSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (s *openAIProxyWritableSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}

func (s *openAIProxyRepoStub) ListAllForFallback(context.Context) ([]Proxy, error) {
	s.calls++
	return s.values, s.err
}

func TestOpenAIProxyPolicyRefreshFailureRetainsLastGoodSnapshot(t *testing.T) {
	settingsRepo := &openAIProxySettingRepoStub{values: map[string]string{
		SettingKeyOpenAIDefaultProxyEnabled:       "true",
		SettingKeyOpenAIDefaultProxyURL:           "socks5://node-proxy:1080",
		SettingKeyOpenAIDefaultProxyFailurePolicy: "fail_closed",
	}}
	proxyRepo := &openAIProxyRepoStub{}
	policy := newOpenAIProxyPolicyService(settingsRepo, proxyRepo, nil, time.Minute)
	require.NoError(t, policy.Refresh(context.Background()))
	require.Equal(t, []string{"socks5h://node-proxy:1080"}, policy.Resolve(context.Background(), "").URLs())

	settingsRepo.err = errors.New("database unavailable")
	require.Error(t, policy.Refresh(context.Background()))

	require.Equal(t, []string{"socks5h://node-proxy:1080"}, policy.Resolve(context.Background(), "").URLs())
}

func TestOpenAIProxyPolicyResolveExpiredSnapshotDoesNotReadRepositories(t *testing.T) {
	now := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	settingsRepo := &openAIProxySettingRepoStub{values: map[string]string{
		SettingKeyOpenAIDefaultProxyEnabled:       "true",
		SettingKeyOpenAIDefaultProxyURL:           DefaultOpenAIDefaultProxyURL,
		SettingKeyOpenAIDefaultProxyFailurePolicy: "fail_closed",
	}}
	proxyRepo := &openAIProxyRepoStub{}
	policy := newOpenAIProxyPolicyService(settingsRepo, proxyRepo, nil, time.Minute)
	policy.now = func() time.Time { return now }
	require.NoError(t, policy.Refresh(context.Background()))
	require.Equal(t, int64(1), settingsRepo.calls.Load())

	now = now.Add(2 * time.Minute)
	policy.Resolve(context.Background(), "")

	require.Equal(t, int64(1), settingsRepo.calls.Load())
}

func TestOpenAIProxyPolicySnapshotRefreshWorkerRefreshesOnTTL(t *testing.T) {
	settingsRepo := &openAIProxySettingRepoStub{values: map[string]string{
		SettingKeyOpenAIDefaultProxyEnabled:       "true",
		SettingKeyOpenAIDefaultProxyURL:           DefaultOpenAIDefaultProxyURL,
		SettingKeyOpenAIDefaultProxyFailurePolicy: "fail_closed",
	}}
	policy := newOpenAIProxyPolicyService(settingsRepo, &openAIProxyRepoStub{}, nil, 10*time.Millisecond)
	require.NoError(t, policy.Refresh(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	policy.startSnapshotRefreshWorker(ctx)
	require.Eventually(t, func() bool {
		return settingsRepo.calls.Load() >= 2
	}, time.Second, 5*time.Millisecond)
	cancel()
	policy.refreshWG.Wait()
}

func TestSettingServiceUpdateRefreshesAndPublishesOpenAIProxyPolicy(t *testing.T) {
	settingsRepo := &openAIProxyWritableSettingRepo{values: map[string]string{
		SettingKeyOpenAIDefaultProxyEnabled:       "true",
		SettingKeyOpenAIDefaultProxyURL:           DefaultOpenAIDefaultProxyURL,
		SettingKeyOpenAIDefaultProxyFailurePolicy: "fail_closed",
	}}
	proxyRepo := &openAIProxyRepoStub{}
	bus := &openAIProxySettingsBusStub{publishErr: errors.New("Redis unavailable")}
	policy := newOpenAIProxyPolicyService(settingsRepo, proxyRepo, bus, time.Minute)
	require.NoError(t, policy.Refresh(context.Background()))
	settingService := NewSettingService(settingsRepo, &config.Config{})
	settingService.SetOpenAIProxyPolicyService(policy)
	settings := settingService.parseSettings(settingsRepo.values)
	settings.OpenAIDefaultProxyURL = "socks5://replacement:1080"

	err := settingService.UpdateSettings(context.Background(), settings)

	require.NoError(t, err)
	require.Equal(t, 1, bus.publishCalls)
	require.Equal(t, []string{"socks5h://replacement:1080"}, policy.Resolve(context.Background(), "").URLs())
}

func TestSettingServiceUpdatePublishesWhenLocalOpenAIProxyRefreshFails(t *testing.T) {
	settingsRepo := &openAIProxyWritableSettingRepo{values: map[string]string{
		SettingKeyOpenAIDefaultProxyEnabled:       "true",
		SettingKeyOpenAIDefaultProxyURL:           DefaultOpenAIDefaultProxyURL,
		SettingKeyOpenAIDefaultProxyFailurePolicy: "fail_closed",
	}}
	proxyRepo := &openAIProxyRepoStub{}
	bus := &openAIProxySettingsBusStub{}
	policy := newOpenAIProxyPolicyService(settingsRepo, proxyRepo, bus, time.Minute)
	require.NoError(t, policy.Refresh(context.Background()))
	settingService := NewSettingService(settingsRepo, &config.Config{})
	settingService.SetOpenAIProxyPolicyService(policy)
	settings := settingService.parseSettings(settingsRepo.values)
	settings.OpenAIDefaultProxyURL = "socks5://replacement:1080"
	settingsRepo.getErr = errors.New("database unavailable")

	err := settingService.UpdateSettings(context.Background(), settings)

	require.NoError(t, err)
	require.Equal(t, 1, bus.publishCalls)
	require.Equal(t, []string{DefaultOpenAIDefaultProxyURL}, policy.Resolve(context.Background(), "").URLs())
}

func TestNormalizeOpenAIProxySettingsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		settings OpenAIProxySettings
	}{
		{
			name: "unsupported proxy scheme",
			settings: OpenAIProxySettings{
				Enabled:       true,
				ProxyURL:      "ftp://warp-proxy:21",
				FailurePolicy: OpenAIProxyFailurePolicyFailClosed,
			},
		},
		{
			name: "enabled proxy requires URL",
			settings: OpenAIProxySettings{
				Enabled:       true,
				FailurePolicy: OpenAIProxyFailurePolicyFailClosed,
			},
		},
		{
			name: "unknown failure policy",
			settings: OpenAIProxySettings{
				Enabled:       true,
				ProxyURL:      DefaultOpenAIDefaultProxyURL,
				FailurePolicy: OpenAIProxyFailurePolicy("always_direct"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeOpenAIProxySettings(tt.settings)
			require.Error(t, err)
		})
	}
}

func TestSettingServiceParseSettingsDefaultsOpenAIProxyFailClosed(t *testing.T) {
	svc := NewSettingService(&settingUpdateRepoStub{}, &config.Config{})

	settings := svc.parseSettings(map[string]string{})

	require.True(t, settings.OpenAIDefaultProxyEnabled)
	require.Equal(t, DefaultOpenAIDefaultProxyURL, settings.OpenAIDefaultProxyURL)
	require.Equal(t, OpenAIProxyFailurePolicyFailClosed, settings.OpenAIDefaultProxyFailurePolicy)
}

func TestSettingServiceUpdateSettingsPersistsNormalizedOpenAIProxy(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	settings := svc.parseSettings(map[string]string{})
	settings.OpenAIDefaultProxyEnabled = true
	settings.OpenAIDefaultProxyURL = "socks5://warp-proxy:1080"
	settings.OpenAIDefaultProxyFailurePolicy = OpenAIProxyFailurePolicyFallbackDirect

	err := svc.UpdateSettings(context.Background(), settings)

	require.NoError(t, err)
	require.Equal(t, "true", repo.updates[SettingKeyOpenAIDefaultProxyEnabled])
	require.Equal(t, "socks5h://warp-proxy:1080", repo.updates[SettingKeyOpenAIDefaultProxyURL])
	require.Equal(t, "fallback_direct", repo.updates[SettingKeyOpenAIDefaultProxyFailurePolicy])
}

func TestSettingServiceUpdateSettingsRejectsInvalidOpenAIProxyBeforeWrite(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	settings := svc.parseSettings(map[string]string{})
	settings.OpenAIDefaultProxyURL = "ftp://warp-proxy:21"

	err := svc.UpdateSettings(context.Background(), settings)

	require.Error(t, err)
	require.Nil(t, repo.updates)
}

func TestOpenAIProxyPolicyHealthProbeParsesCloudflareTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ip=203.0.113.4\nwarp=on\n"))
	}))
	defer server.Close()

	policy := newOpenAIProxyPolicyForTest(DefaultOpenAIProxySettings(), nil, time.Now())
	policy.instanceID = "node-a"
	policy.healthTraceURL = server.URL
	var usedProxy string
	policy.healthClientFactory = func(proxyURL string) (*http.Client, error) {
		usedProxy = proxyURL
		return server.Client(), nil
	}

	status := policy.checkHealth(context.Background())

	require.Equal(t, DefaultOpenAIDefaultProxyURL, usedProxy)
	require.Equal(t, "node-a", status.InstanceID)
	require.True(t, status.Healthy)
	require.Equal(t, "203.0.113.4", status.EgressIP)
	require.NotZero(t, status.CheckedAt)
	require.Empty(t, status.Error)
	require.Equal(t, status, policy.Status())
}

func TestOpenAIProxyPolicyMetricsSnapshot(t *testing.T) {
	policy := newOpenAIProxyPolicyForTest(DefaultOpenAIProxySettings(), nil, time.Now())

	policy.RecordAttempt(OpenAIProxyCandidateAccount)
	policy.RecordAttempt(OpenAIProxyCandidateNode)
	policy.RecordCandidateSwitch()
	policy.RecordFailClosedExhaustion()
	policy.RecordDirectFallback()
	policy.RecordTransportFailure(OpenAIProxyTransportHTTP)
	policy.RecordTransportFailure(OpenAIProxyTransportWebSocket)

	metrics := policy.Metrics()
	require.Equal(t, uint64(1), metrics.AttemptsBySource.Account)
	require.Equal(t, uint64(1), metrics.AttemptsBySource.Node)
	require.Equal(t, uint64(1), metrics.CandidateSwitches)
	require.Equal(t, uint64(1), metrics.FailClosedExhaustions)
	require.Equal(t, uint64(1), metrics.DirectFallbacks)
	require.Equal(t, uint64(1), metrics.HTTPTransportFailures)
	require.Equal(t, uint64(1), metrics.WebSocketTransportFailures)
}

func TestOpenAIProxyPlanIsFailClosedOnlyWithNodeAndWithoutDirect(t *testing.T) {
	require.True(t, (OpenAIProxyPlan{Candidates: []OpenAIProxyCandidate{
		{Source: OpenAIProxyCandidateAccount},
		{Source: OpenAIProxyCandidateNode},
	}}).IsFailClosed())
	require.False(t, (OpenAIProxyPlan{Candidates: []OpenAIProxyCandidate{
		{Source: OpenAIProxyCandidateAccount},
	}}).IsFailClosed())
	require.False(t, (OpenAIProxyPlan{Candidates: []OpenAIProxyCandidate{
		{Source: OpenAIProxyCandidateNode},
		{Source: OpenAIProxyCandidateDirect},
	}}).IsFailClosed())
}
