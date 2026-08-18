# OpenAI WARP Egress Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route every OpenAI/ChatGPT/Codex HTTP, WebSocket, and OAuth request through an account proxy chain followed by the node-local WARP SOCKS5 proxy, with an admin-controlled direct-fallback policy shared across all instances.

**Architecture:** A service-layer `OpenAIProxyPolicyService` owns an atomic, immutable snapshot of PostgreSQL settings and configured proxy chains. Redis Pub/Sub requests immediate cross-instance refresh while a short snapshot TTL provides eventual convergence; the common HTTP upstream, OpenAI WebSocket dialer, and OAuth client consume the same ordered candidates and only advance after pre-response transport failures.

**Tech Stack:** Go 1.24, Gin, Ent/PostgreSQL, go-redis v9, `net/http`, coder/websocket, Vue 3, TypeScript, Vitest, Docker Compose, `caomingjun/warp`.

## Global Constraints

- Affect only OpenAI/ChatGPT/Codex and OpenAI-compatible accounts; do not proxy Claude, Gemini, Grok, PostgreSQL, Redis, storage, updates, or the container default route.
- Candidate order is account proxy, valid configured backup proxy chain, node WARP, then direct only for `failure_policy=fallback_direct`.
- When the global proxy is disabled, retain current behavior: account proxy when present, otherwise direct.
- Defaults are `enabled=true`, `proxy_url=socks5h://warp-proxy:1080`, and `failure_policy=fail_closed`.
- Only connection, DNS, proxy handshake, TLS, or pre-response WebSocket handshake errors may advance candidates; HTTP status responses never trigger proxy fallback.
- A request with a body and more than one candidate must have `Request.GetBody`; reject it before the first attempt when it cannot be replayed.
- Reuse the original request context and timeout budget for every candidate; never grant a fresh total timeout.
- Proxy URLs in logs and status responses must be redacted; invalid configuration must fail at save time.
- PostgreSQL is authoritative, Redis Pub/Sub is an invalidation signal, and request hot paths read only an atomic snapshot.
- Each Compose node gets its own WARP sidecar, with no host port publication and no startup dependency that prevents Sub2API from serving non-OpenAI traffic.
- Preserve all unrelated existing worktree changes and stage only OpenAI WARP egress hunks.

---

### Task 1: Runtime settings model and validation

**Files:**
- Modify: `backend/internal/service/domain_constants.go`
- Modify: `backend/internal/service/settings_view.go`
- Modify: `backend/internal/service/setting_parse.go`
- Modify: `backend/internal/service/setting_update.go`
- Create: `backend/internal/service/openai_proxy_policy.go`
- Create: `backend/internal/service/openai_proxy_policy_test.go`
- Modify: `backend/internal/service/setting_service_update_test.go`

**Interfaces:**
- Produces: `OpenAIProxyFailurePolicy`, `OpenAIProxySettings`, `NormalizeOpenAIProxySettings(OpenAIProxySettings) (OpenAIProxySettings, error)`.
- Produces setting keys `openai_default_proxy_enabled`, `openai_default_proxy_url`, and `openai_default_proxy_failure_policy`.
- Later tasks consume the normalized settings and safe defaults from this task.

- [ ] **Step 1: Write failing normalization and settings persistence tests**

```go
func TestNormalizeOpenAIProxySettings(t *testing.T) {
	got, err := NormalizeOpenAIProxySettings(OpenAIProxySettings{
		Enabled: true, ProxyURL: " socks5://warp-proxy:1080 ", FailurePolicy: "fallback_direct",
	})
	require.NoError(t, err)
	require.Equal(t, "socks5h://warp-proxy:1080", got.ProxyURL)
	require.Equal(t, OpenAIProxyFailurePolicyFallbackDirect, got.FailurePolicy)
}

func TestNormalizeOpenAIProxySettingsRejectsInvalidPolicyAndURL(t *testing.T) {
	_, err := NormalizeOpenAIProxySettings(OpenAIProxySettings{Enabled: true, ProxyURL: "ftp://proxy:21", FailurePolicy: "open"})
	require.Error(t, err)
}
```

Add an update test asserting that the three normalized values are written by `SettingService.UpdateSettings` and that invalid values do not call `SetMultiple`.

- [ ] **Step 2: Run tests and verify the missing API fails**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestNormalizeOpenAIProxySettings|TestSettingService_UpdateSettings_.*OpenAIProxy' -count=1`

Expected: FAIL because the types, constants, and persistence mapping do not exist.

- [ ] **Step 3: Add settings types, defaults, parsing, and validation**

```go
const (
	SettingKeyOpenAIDefaultProxyEnabled       = "openai_default_proxy_enabled"
	SettingKeyOpenAIDefaultProxyURL           = "openai_default_proxy_url"
	SettingKeyOpenAIDefaultProxyFailurePolicy = "openai_default_proxy_failure_policy"
	DefaultOpenAIDefaultProxyURL               = "socks5h://warp-proxy:1080"
)

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

func DefaultOpenAIProxySettings() OpenAIProxySettings {
	return OpenAIProxySettings{Enabled: true, ProxyURL: DefaultOpenAIDefaultProxyURL, FailurePolicy: OpenAIProxyFailurePolicyFailClosed}
}

func NormalizeOpenAIProxySettings(value OpenAIProxySettings) (OpenAIProxySettings, error) {
	proxyURL, _, err := proxyurl.Parse(value.ProxyURL)
	if err != nil || (value.Enabled && proxyURL == "") {
		return OpenAIProxySettings{}, fmt.Errorf("invalid OpenAI default proxy URL: %w", err)
	}
	if value.FailurePolicy != OpenAIProxyFailurePolicyFailClosed && value.FailurePolicy != OpenAIProxyFailurePolicyFallbackDirect {
		return OpenAIProxySettings{}, fmt.Errorf("invalid OpenAI proxy failure policy %q", value.FailurePolicy)
	}
	value.ProxyURL = proxyURL
	return value, nil
}
```

Add the three fields to `SystemSettings`, initialize them on a new installation, parse missing/invalid stored values to the safe defaults, normalize in `buildSystemSettingsUpdates`, and persist all three keys.

- [ ] **Step 4: Run focused settings tests**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestNormalizeOpenAIProxySettings|TestSettingService_UpdateSettings_.*OpenAIProxy' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the settings model**

```bash
git add backend/internal/service/domain_constants.go backend/internal/service/settings_view.go backend/internal/service/setting_parse.go backend/internal/service/setting_update.go backend/internal/service/openai_proxy_policy.go backend/internal/service/openai_proxy_policy_test.go backend/internal/service/setting_service_update_test.go
git commit -m "feat: add OpenAI default proxy settings"
```

### Task 2: Atomic proxy policy and distributed refresh

**Files:**
- Modify: `backend/internal/service/openai_proxy_policy.go`
- Modify: `backend/internal/service/openai_proxy_policy_test.go`
- Create: `backend/internal/repository/openai_proxy_settings_bus.go`
- Create: `backend/internal/repository/openai_proxy_settings_bus_test.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/internal/service/setting_service.go`
- Modify: `backend/internal/service/setting_update.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire.go`
- Generate: `backend/cmd/server/wire_gen.go`

**Interfaces:**
- Consumes: `SettingRepository.GetMultiple`, `ProxyRepository.ListAllForFallback`, and Task 1 settings.
- Produces: `OpenAIProxyCandidate`, `OpenAIProxyPlan`, `OpenAIProxyPolicyProvider.Resolve(ctx context.Context, primaryProxyURL string) OpenAIProxyPlan`.
- Produces: `OpenAIProxySettingsBus.Publish(context.Context) error` and `Subscribe(context.Context, func())`.
- Produces: `OpenAIProxyPolicyService.SettingsUpdated(context.Context)` and `Stop()`.

- [ ] **Step 1: Write failing candidate-order tests**

```go
func TestOpenAIProxyPolicyResolveOrdersAndDeduplicatesCandidates(t *testing.T) {
	now := time.Now()
	backupID := int64(2)
	policy := newOpenAIProxyPolicyForTest(OpenAIProxySettings{Enabled: true, ProxyURL: "socks5h://warp-proxy:1080", FailurePolicy: OpenAIProxyFailurePolicyFallbackDirect}, []Proxy{
		{ID: 1, Protocol: "http", Host: "primary", Port: 8080, Status: StatusActive, FallbackMode: FallbackModeProxy, BackupProxyID: &backupID},
		{ID: 2, Protocol: "socks5h", Host: "backup", Port: 1080, Status: StatusActive},
	}, now)
	plan := policy.Resolve(context.Background(), "http://primary:8080")
	require.Equal(t, []string{"http://primary:8080", "socks5h://backup:1080", "socks5h://warp-proxy:1080", ""}, plan.URLs())
}
```

Cover no account proxy, disabled global proxy, fail-closed, expired/disabled/missing backup, cycles, duplicate WARP URL, failed refresh retaining the last good snapshot, and TTL refresh.

- [ ] **Step 2: Run policy tests and verify they fail**

Run: `cd backend && go test -tags=unit ./internal/service -run TestOpenAIProxyPolicy -count=1`

Expected: FAIL because snapshot loading and candidate resolution are absent.

- [ ] **Step 3: Implement the immutable snapshot and resolver**

```go
type OpenAIProxyCandidate struct {
	URL    string
	Source OpenAIProxyCandidateSource
}

type OpenAIProxyPlan struct {
	Candidates []OpenAIProxyCandidate
}

type openAIProxySnapshot struct {
	settings  OpenAIProxySettings
	proxies   map[int64]Proxy
	byURL     map[string]int64
	expiresAt time.Time
}

func (s *OpenAIProxyPolicyService) Resolve(ctx context.Context, primary string) OpenAIProxyPlan {
	snapshot := s.currentSnapshot(ctx)
	if !snapshot.settings.Enabled {
		if strings.TrimSpace(primary) == "" { return directOpenAIProxyPlan() }
		return OpenAIProxyPlan{Candidates: []OpenAIProxyCandidate{{URL: primary, Source: OpenAIProxyCandidateAccount}}}
	}
	return snapshot.resolve(primary)
}
```

Normalize and deduplicate every URL using `proxyurl.Parse`. Traverse `FallbackModeProxy` by ID, include only active and unexpired backups, stop on missing entries or cycles, append WARP, and append direct only for `fallback_direct`. Store the safe default snapshot before the first database read and preserve the previous snapshot on refresh failure.

- [ ] **Step 4: Implement Redis invalidation and update wiring**

```go
const openAIProxySettingsChannel = "openai_proxy_settings_updated"

func (b *openAIProxySettingsBus) Publish(ctx context.Context) error {
	return b.rdb.Publish(ctx, openAIProxySettingsChannel, "refresh").Err()
}

func (b *openAIProxySettingsBus) Subscribe(ctx context.Context, handler func()) {
	go func() {
		sub := b.rdb.Subscribe(ctx, openAIProxySettingsChannel)
		defer sub.Close()
		for {
			select {
			case <-ctx.Done(): return
			case msg, ok := <-sub.Channel():
				if !ok { return }
				if msg != nil { handler() }
			}
		}
	}()
}
```

Make `ProvideSettingService` inject the policy service through a setter. After a successful settings write, synchronously refresh the local policy and publish invalidation best-effort. Start the subscriber in the policy provider and add `Stop()` to application cleanup.

- [ ] **Step 5: Test refresh and Pub/Sub behavior**

Run: `cd backend && go test -tags=unit ./internal/service ./internal/repository -run 'TestOpenAIProxyPolicy|TestOpenAIProxySettingsBus' -count=1`

Expected: PASS, including last-good-snapshot behavior when PostgreSQL or Redis is unavailable.

- [ ] **Step 6: Regenerate dependency injection and compile**

Run: `cd backend && go generate ./cmd/server`

Expected: `wire_gen.go` includes one policy service shared by SettingService, HTTP upstream, OAuth, and gateway services.

Run: `cd backend && go test ./cmd/server -run TestWireGeneratedUpToDate -count=1`

Expected: PASS.

- [ ] **Step 7: Commit runtime policy**

```bash
git add backend/internal/service/openai_proxy_policy.go backend/internal/service/openai_proxy_policy_test.go backend/internal/repository/openai_proxy_settings_bus.go backend/internal/repository/openai_proxy_settings_bus_test.go backend/internal/repository/wire.go backend/internal/service/setting_service.go backend/internal/service/setting_update.go backend/internal/service/wire.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat: distribute OpenAI proxy policy"
```

### Task 3: HTTP candidate executor

**Files:**
- Modify: `backend/internal/repository/http_upstream.go`
- Modify: `backend/internal/repository/http_upstream_test.go`
- Create: `backend/internal/repository/http_upstream_openai_proxy_test.go`

**Interfaces:**
- Consumes: `OpenAIProxyPolicyProvider.Resolve` and `HTTPUpstreamProfileOpenAI`.
- Produces: identical `HTTPUpstream.Do` and `DoWithTLS` signatures with transparent OpenAI candidate fallback.
- Does not change default/Grok upstream behavior.

- [ ] **Step 1: Write failing transport fallback tests**

Use a stub policy returning `proxy-a`, `proxy-b`, and direct, plus an injected attempt function that records candidate URLs.

```go
func TestHTTPUpstreamOpenAIAdvancesOnlyOnTransportError(t *testing.T) {
	req := replayableRequest(t)
	svc := newHTTPUpstreamWithAttempts(stubPlan("http://a:1", "http://b:2", ""), []attemptResult{{err: io.ErrUnexpectedEOF}, {response: &http.Response{StatusCode: 429, Body: http.NoBody}}})
	resp, err := svc.Do(req, "http://account:8080", 7, 1)
	require.NoError(t, err)
	require.Equal(t, 429, resp.StatusCode)
	require.Equal(t, []string{"http://a:1", "http://b:2"}, svc.attempted)
}
```

Also cover non-OpenAI single attempt, `DoWithTLS`, context cancellation, fail-closed exhaustion, direct fallback, and a non-replayable body rejected before any attempt.

- [ ] **Step 2: Run focused tests and verify fallback is absent**

Run: `cd backend && go test ./internal/repository -run 'TestHTTPUpstreamOpenAI' -count=1`

Expected: FAIL because HTTP upstream attempts only the passed proxy URL.

- [ ] **Step 3: Implement replay-safe candidate execution**

```go
func prepareOpenAIProxyAttempts(req *http.Request, plan service.OpenAIProxyPlan) ([]*http.Request, error) {
	if len(plan.Candidates) <= 1 { return []*http.Request{req}, nil }
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return nil, service.ErrOpenAIProxyRequestNotReplayable
	}
	attempts := make([]*http.Request, 0, len(plan.Candidates))
	for i := range plan.Candidates {
		clone := req.Clone(req.Context())
		if i == 0 { clone.Body = req.Body } else if req.GetBody != nil {
			body, err := req.GetBody(); if err != nil { return nil, err }; clone.Body = body
		}
		attempts = append(attempts, clone)
	}
	return attempts, nil
}
```

Factor the existing one-candidate `Do` and `DoWithTLS` logic into attempt helpers. Resolve a plan only when the request profile is OpenAI, return immediately on any non-nil response, and advance only when response is nil and the request context remains active. Wrap exhausted errors with a redacted candidate source and emit a warning only when direct fallback is actually attempted.

- [ ] **Step 4: Run HTTP upstream tests**

Run: `cd backend && go test ./internal/repository -run 'TestHTTPUpstream|TestOpenAI' -count=1`

Expected: PASS with HTTP/1, HTTP/2, TLS fingerprint, redirect, decompression, and client-cache tests unchanged.

- [ ] **Step 5: Commit HTTP fallback**

```bash
git add backend/internal/repository/http_upstream.go backend/internal/repository/http_upstream_test.go backend/internal/repository/http_upstream_openai_proxy_test.go
git commit -m "feat: route OpenAI HTTP through proxy candidates"
```

### Task 4: WebSocket candidate executor

**Files:**
- Modify: `backend/internal/service/openai_ws_client.go`
- Modify: `backend/internal/service/openai_ws_pool.go`
- Modify: `backend/internal/service/openai_ws_forwarder.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Create: `backend/internal/service/openai_ws_proxy_policy_test.go`

**Interfaces:**
- Consumes: `OpenAIProxyPolicyProvider.Resolve`.
- Produces: default OpenAI WebSocket dialers that advance candidates before a successful handshake only.
- Preserves the existing `openAIWSClientDialer` interface so test dialers remain source-compatible.

- [ ] **Step 1: Write failing WebSocket handshake tests**

```go
func TestOpenAIWSDefaultDialerAdvancesOnTransportFailure(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer(stubOpenAIProxyPolicy{"http://a:1", "http://b:2"})
	dialer.dial = stagedWSDial([]wsDialResult{{err: errors.New("connect refused")}, {conn: &fakeConn{}}})
	conn, status, _, err := dialer.Dial(context.Background(), "wss://chatgpt.com/backend-api/codex/responses", nil, "")
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Zero(t, status)
}
```

Cover a 401/429 handshake response stopping immediately, `fail_closed` exhaustion, direct fallback, and proxy URL redaction in errors.

- [ ] **Step 2: Run tests and verify the first failure is returned**

Run: `cd backend && go test -tags=unit ./internal/service -run TestOpenAIWS.*Proxy -count=1`

Expected: FAIL because the dialer attempts one proxy.

- [ ] **Step 3: Add policy-aware dialing**

Give `coderOpenAIWSClientDialer` a policy provider and a testable single-attempt function. For each candidate, reuse the same `ctx`; stop when `resp != nil` or `status != 0`, and advance only when the handshake produced no HTTP response. Inject the shared policy into both the connection pool and passthrough dialer from `OpenAIGatewayService`.

```go
for _, candidate := range d.policy.Resolve(ctx, proxyURL).Candidates {
	conn, status, headers, err := d.dialOnce(ctx, targetURL, headers, candidate.URL)
	if err == nil || status != 0 { return conn, status, headers, err }
	lastErr = err
}
return nil, 0, nil, lastErr
```

- [ ] **Step 4: Run WebSocket suites**

Run: `cd backend && go test -tags=unit ./internal/service -run 'TestOpenAIWS.*(Proxy|Dial|Passthrough|Pool)' -count=1`

Expected: PASS; existing account task recovery and WS pool behavior remain unchanged.

- [ ] **Step 5: Commit WebSocket fallback**

```bash
git add backend/internal/service/openai_ws_client.go backend/internal/service/openai_ws_pool.go backend/internal/service/openai_ws_forwarder.go backend/internal/service/openai_gateway_service.go backend/internal/service/openai_ws_proxy_policy_test.go
git commit -m "feat: route OpenAI WebSockets through proxy candidates"
```

### Task 5: OAuth and auxiliary OpenAI request coverage

**Files:**
- Modify: `backend/internal/repository/openai_oauth_service.go`
- Modify: `backend/internal/repository/openai_oauth_service_test.go`
- Modify: `backend/internal/repository/req_client_pool.go`
- Create: `backend/internal/repository/openai_privacy_proxy_policy_test.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/cmd/server/wire.go`
- Modify: `backend/internal/service/openai_oauth_service.go`
- Modify: `backend/internal/service/openai_codex_models_service.go`
- Add focused tests beside changed service files.

**Interfaces:**
- Consumes: common `HTTPUpstream` candidate execution.
- Consumes: `OpenAIProxyPolicyProvider` from Task 2 for Chrome-impersonated ChatGPT privacy, subscription, and quota clients.
- Keeps the public `OpenAIOAuthClient` and `OpenAIOAuthService` contracts unchanged.

- [ ] **Step 1: Write failing OAuth default-WARP tests**

Construct a recording `HTTPUpstream`, call `ExchangeCode` and `RefreshTokenWithClientID` with an empty account proxy, and assert the request context has the OpenAI profile and the passed proxy remains empty so the shared policy chooses WARP. Add a 400 token response test proving HTTP errors are not retried through another candidate.

- [ ] **Step 2: Run OAuth tests and verify they fail**

Run: `cd backend && go test ./internal/repository ./internal/service -run 'TestOpenAIOAuth.*(Proxy|WARP|Refresh)' -count=1`

Expected: FAIL because OAuth currently creates a standalone req/v3 client.

- [ ] **Step 3: Route OAuth form posts through HTTPUpstream**

```go
req, err := http.NewRequestWithContext(service.WithHTTPUpstreamProfile(ctx, service.HTTPUpstreamProfileOpenAI), http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
req.Header.Set("User-Agent", "codex-cli/0.91.0")
resp, err := s.httpUpstream.Do(req, proxyURL, 0, 1)
```

Decode successful JSON responses, bound and redact error bodies, and keep the existing OpenAI error codes. Wire the common upstream into the OAuth client. Replace any remaining standalone OpenAI model/probe client path with `HTTPUpstream` plus `HTTPUpstreamProfileOpenAI`; Grok branches retain their current clients.

For ChatGPT privacy, subscription, quota, and reset-credit calls that require req/v3 Chrome impersonation, make `providePrivacyClientFactory` return a policy-aware factory. Clone the configured req/v3 transport once per normalized candidate, resolve the immutable plan at `RoundTrip`, and apply the same replay and pre-response-only rules as the common HTTP executor:

```go
func CreatePolicyAwarePrivacyReqClient(policy service.OpenAIProxyPolicyProvider, primaryProxyURL string) (*req.Client, error) {
	client := req.C().SetTimeout(30 * time.Second).ImpersonateChrome()
	base := client.GetTransport().Clone()
	client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
		return func(request *http.Request) (*http.Response, error) {
			return roundTripOpenAIProxyPlan(request, policy.Resolve(request.Context(), primaryProxyURL), base)
		}
	})
	return instrumentReqClient(client), nil
}
```

Cache candidate transports by normalized/redacted-safe URL key so hot-path calls retain connection pooling and TLS impersonation. Tests must prove empty account proxy selects WARP, account proxy precedes WARP, an HTTP 403 stops the chain, and non-replayable PATCH/POST requests fail before sending.

- [ ] **Step 4: Audit marked OpenAI call sites**

Run: `rg -n 'http\.DefaultClient|httpclient\.GetClient|req\.C\(|coderws\.Dial|Proxy\.URL\(\)' backend/internal/service/openai_*.go backend/internal/repository/openai_oauth_service.go`

Expected: every remaining hit is either a test, a Grok-specific branch, a client factory already receiving the resolved proxy, or explicitly documented in code as not performing an OpenAI network call.

- [ ] **Step 5: Run OAuth and OpenAI service tests**

Run: `cd backend && go test ./internal/repository ./internal/service -run 'OpenAI|HTTPUpstream' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit auxiliary coverage**

```bash
git add backend/internal/repository/openai_oauth_service.go backend/internal/repository/openai_oauth_service_test.go backend/internal/repository/req_client_pool.go backend/internal/repository/openai_privacy_proxy_policy_test.go backend/internal/repository/wire.go backend/cmd/server/wire.go backend/internal/service/openai_oauth_service.go backend/internal/service/openai_codex_models_service.go backend/internal/service/*openai*test.go
git commit -m "feat: apply OpenAI proxy policy to OAuth and probes"
```

### Task 6: Admin API, audit, and node health status

**Files:**
- Modify: `backend/internal/handler/dto/settings.go`
- Modify: `backend/internal/handler/admin/setting_handler.go`
- Modify: `backend/internal/handler/admin/setting_handler_update.go`
- Modify: `backend/internal/handler/admin/setting_handler_audit.go`
- Create: `backend/internal/handler/admin/setting_handler_openai_proxy_test.go`
- Modify: `backend/internal/service/openai_proxy_policy.go`
- Modify: `backend/internal/service/openai_proxy_policy_test.go`

**Interfaces:**
- Produces admin response fields `openai_default_proxy_enabled`, `openai_default_proxy_url`, `openai_default_proxy_failure_policy`, and `openai_default_proxy_status`.
- `openai_default_proxy_status` contains `instance_id`, `healthy`, `egress_ip`, `checked_at`, a redacted `error`, and an `metrics` snapshot.
- Accepts optional pointer fields for partial updates and records all three persisted keys in the existing settings audit event.
- Produces atomic counters for attempts by source, candidate switches, fail-closed exhaustion, direct fallback, and HTTP/WebSocket transport failures.

- [ ] **Step 1: Write failing handler and health tests**

Assert invalid proxy URLs and policies return 400 without persistence; valid `socks5://` is returned as `socks5h://`; partial requests preserve omitted values; audit diff names all changed fields. For health, use an HTTP test server returning `ip=203.0.113.4\nwarp=on\n` and assert parsed instance status.

- [ ] **Step 2: Run focused tests**

Run: `cd backend && go test -tags=unit ./internal/handler/admin ./internal/service -run 'Test.*OpenAIProxy' -count=1`

Expected: FAIL because DTOs, validation, audit keys, and status are absent.

- [ ] **Step 3: Implement API mapping and health probe**

Validate request values with `NormalizeOpenAIProxySettings` before constructing `SystemSettings`. Add the fields to both response builders and `diffSettings`. The policy service health worker must probe `https://www.cloudflare.com/cdn-cgi/trace` through only the configured node proxy every 30 seconds with a 10-second timeout, parse `warp=on|plus` and `ip=`, and retain only redacted errors. Use `os.Hostname()` for `instance_id` and stop the worker in `Stop()`.

Add `OpenAIProxyMetricsRecorder` with lock-free counters. The HTTP and WebSocket executors record candidate source, protocol, transport failure, candidate switch, fail-closed exhaustion, and direct fallback. Include the snapshot in `openai_default_proxy_status`, and emit structured startup/health/fallback logs with request ID, account ID when available, instance ID, source, and redacted proxy address.

- [ ] **Step 4: Run handler tests**

Run: `cd backend && go test -tags=unit ./internal/handler/admin ./internal/service -run 'Test.*OpenAIProxy' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit admin API and health status**

```bash
git add backend/internal/handler/dto/settings.go backend/internal/handler/admin/setting_handler.go backend/internal/handler/admin/setting_handler_update.go backend/internal/handler/admin/setting_handler_audit.go backend/internal/handler/admin/setting_handler_openai_proxy_test.go backend/internal/service/openai_proxy_policy.go backend/internal/service/openai_proxy_policy_test.go
git commit -m "feat: expose OpenAI proxy controls and health"
```

### Task 7: Admin settings UI

**Files:**
- Modify: `frontend/src/api/admin/settings.ts`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Modify: `frontend/src/views/admin/__tests__/SettingsView.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/settings.ts`
- Modify: `frontend/src/i18n/locales/en/admin/settings.ts`

**Interfaces:**
- Consumes the Task 6 settings DTO.
- Produces a toggle, proxy URL input, fail-closed/direct-fallback selector, and current-node health row in the existing OpenAI Codex settings section.

- [ ] **Step 1: Write a failing UI behavior test**

```ts
it("saves OpenAI default proxy policy", async () => {
  mockGetSettings({ openai_default_proxy_enabled: true, openai_default_proxy_url: "socks5h://warp-proxy:1080", openai_default_proxy_failure_policy: "fail_closed" });
  const wrapper = mountSettings();
  await wrapper.get('[data-testid="openai-default-proxy-toggle"]').trigger("click");
  await wrapper.get('[data-testid="openai-proxy-failure-policy"]').setValue("fallback_direct");
  await wrapper.get('[data-testid="save-gateway-settings"]').trigger("click");
  expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({ openai_default_proxy_enabled: false, openai_default_proxy_failure_policy: "fallback_direct" }));
});
```

Also assert that fail-closed danger copy is visible, credentials in a redacted proxy URL are never rendered, and the current instance ID/health timestamp appear.

- [ ] **Step 2: Run the UI test and verify missing controls fail**

Run: `cd frontend && npm run test -- SettingsView.spec.ts --run`

Expected: FAIL because types and controls are absent.

- [ ] **Step 3: Add API types and controls using existing components**

```ts
export type OpenAIProxyFailurePolicy = "fail_closed" | "fallback_direct";
export interface OpenAIProxyStatus {
  instance_id: string;
  healthy: boolean;
  egress_ip: string;
  checked_at: string;
  error: string;
}
```

Place the controls next to the existing Codex upstream settings. Disable the URL and policy inputs when the toggle is off, use the existing switch/input/select/status styles, and explain `fallback_direct` as an IP-exposure risk in concise Chinese and English copy.

- [ ] **Step 4: Run frontend tests and type checking**

Run: `cd frontend && npm run test -- SettingsView.spec.ts --run`

Expected: PASS.

Run: `cd frontend && npm run type-check`

Expected: PASS.

- [ ] **Step 5: Commit the UI**

```bash
git add frontend/src/api/admin/settings.ts frontend/src/views/admin/SettingsView.vue frontend/src/views/admin/__tests__/SettingsView.spec.ts frontend/src/i18n/locales/zh/admin/settings.ts frontend/src/i18n/locales/en/admin/settings.ts
git commit -m "feat: add OpenAI proxy settings UI"
```

### Task 8: Per-node WARP Compose overlay and deployment guide

**Files:**
- Create: `deploy/docker-compose.warp.yml`
- Create: `deploy/WARP_EGRESS.md`
- Modify: `README.md`

**Interfaces:**
- Produces the service DNS name `warp-proxy:1080` on each Compose node.
- Uses pinned image `caomingjun/warp:2026.6.880.0-2.12.0@sha256:98d048b8996ca2e621d29887169c5c48a4f6878e2a2c647b7ea82d69540420da`.

- [ ] **Step 1: Add the overlay with least required privilege**

```yaml
services:
  warp-proxy:
    image: caomingjun/warp:2026.6.880.0-2.12.0@sha256:98d048b8996ca2e621d29887169c5c48a4f6878e2a2c647b7ea82d69540420da
    restart: unless-stopped
    device_cgroup_rules:
      - "c 10:200 rwm"
    cap_add:
      - NET_ADMIN
    environment:
      WARP_SLEEP: "2"
      WARP_LICENSE_KEY: "${WARP_LICENSE_KEY:-}"
    sysctls:
      net.ipv6.conf.all.disable_ipv6: "0"
      net.ipv4.conf.all.src_valid_mark: "1"
    volumes:
      - warp-data:/var/lib/cloudflare-warp
    healthcheck:
      test: ["CMD-SHELL", "curl -fsS --socks5-hostname 127.0.0.1:1080 https://www.cloudflare.com/cdn-cgi/trace | grep -Eq '^warp=(on|plus)$'"]
      interval: 30s
      timeout: 10s
      retries: 5
      start_period: 45s

volumes:
  warp-data:
```

Do not publish port 1080 and do not add a healthy dependency from Sub2API to WARP.

- [ ] **Step 2: Document three-node rollout and rollback**

Document on each node:

```bash
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml config
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml up -d
docker compose exec warp-proxy curl --socks5-hostname 127.0.0.1:1080 https://www.cloudflare.com/cdn-cgi/trace
```

Explain that shared PostgreSQL settings use the same service DNS but resolve to each node's local sidecar. Include kernel/TUN troubleshooting, image upgrade procedure, `fail_closed` rollout, explicit `fallback_direct` risk, and rollback by disabling the setting before removing the overlay.

- [ ] **Step 3: Validate the merged Compose model**

Run: `cd deploy && docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml config --quiet`

Expected: exit 0; `warp-proxy` has no `ports`, no application secrets, and only `NET_ADMIN` plus TUN device access.

- [ ] **Step 4: Commit deployment artifacts**

```bash
git add deploy/docker-compose.warp.yml deploy/WARP_EGRESS.md README.md
git commit -m "docs: add per-node WARP deployment"
```

### Task 9: End-to-end verification and change review

**Files:**
- Review all files changed by Tasks 1-8.
- Modify only test/docs files needed to close verified gaps.

**Interfaces:**
- Validates the full contract without introducing new production APIs.

- [ ] **Step 1: Run formatting and generated-file checks**

Run: `cd backend && gofmt -w internal/service/openai_proxy_policy*.go internal/repository/openai_proxy_settings_bus*.go internal/repository/http_upstream*.go internal/repository/openai_oauth_service*.go internal/handler/admin/setting_handler_openai_proxy_test.go`

Run: `cd backend && go test ./cmd/server -run TestWireGeneratedUpToDate -count=1`

Expected: PASS.

- [ ] **Step 2: Run backend focused and package suites**

Run: `cd backend && go test -tags=unit ./internal/service ./internal/handler/admin -count=1`

Run: `cd backend && go test ./internal/repository ./cmd/server -count=1`

Expected: PASS.

- [ ] **Step 3: Run frontend verification**

Run: `cd frontend && npm run test -- SettingsView.spec.ts --run && npm run type-check`

Expected: PASS.

- [ ] **Step 4: Run Compose and static routing checks**

Run: `cd deploy && docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml config --quiet`

Run: `rg -n 'HTTPUpstreamProfileOpenAI|openAIWSClientDialer|OpenAIOAuthClient' backend/internal/service backend/internal/repository`

Expected: Compose exits 0 and all three OpenAI transport families resolve the shared policy; Grok-specific paths do not.

- [ ] **Step 5: Inspect the final diff for secrets and unrelated changes**

Run: `git diff --check`

Run: `git diff --stat 23fa65323 -- backend/internal/service/openai_proxy_policy.go backend/internal/repository/http_upstream.go backend/internal/repository/openai_oauth_service.go backend/internal/handler/admin frontend/src/views/admin/SettingsView.vue deploy/docker-compose.warp.yml deploy/WARP_EGRESS.md`

Expected: no whitespace errors, no proxy password in logs/tests, and no unrelated message-storage changes included.

- [ ] **Step 6: Commit verification fixes if any**

```bash
git add <only the proxy-related files changed by verification>
git commit -m "test: verify OpenAI WARP egress"
```
