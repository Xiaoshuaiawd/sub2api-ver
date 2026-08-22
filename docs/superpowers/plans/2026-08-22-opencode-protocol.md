# OpenCode Protocol Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a default-off global Gateway Settings mode that normalizes eligible OpenAI Responses HTTP requests to the confirmed OpenCode identity and supplies one shared session value in `session-id`, `x-session-affinity`, and `x-session-id`.

**Architecture:** Persist the switch and version through the existing `SystemSettings` pipeline, publish a cached runtime policy, prepare one canonical session value before OpenAI account scheduling, and apply the final OpenCode identity after all existing Codex/account header rewrites in both normal and passthrough HTTP builders. The Vue Gateway Settings form edits the same two fields through the existing settings API.

**Tech Stack:** Go 1.26, Gin, `net/http`, crypto/rand, Testify, Vue 3, TypeScript, Vitest, vue-i18n.

## Global Constraints

- `opencode_protocol_enabled` defaults to `false`.
- `opencode_protocol_version` defaults to `1.18.21` and accepts dotted numeric versions only.
- Eligible HTTP requests use `opencode/<version> (darwin 24.6.0; arm64) ai-sdk/provider-utils/4.0.38 runtime/bun/1.3.14` and `originator: opencode`.
- The three session headers are identical; precedence is `session-id`, `x-session-affinity`, then `x-session-id`, otherwise secure `ses_<random>` generation.
- The feature does not alter TLS, HTTP/2, WebSocket, Claude, Gemini, or native Grok transport behavior.
- Never log or persist authorization credentials or request bodies for this feature.

---

### Task 1: Persisted OpenCode Gateway Settings

**Files:**
- Modify: `backend/internal/service/settings_view.go`
- Modify: `backend/internal/service/domain_constants.go`
- Modify: `backend/internal/service/setting_parse.go`
- Modify: `backend/internal/service/setting_update.go`
- Modify: `backend/internal/service/setting_service.go`
- Modify: `backend/internal/service/setting_gateway_runtime.go`
- Test: `backend/internal/service/setting_service_update_test.go`

**Interfaces:**
- Produces: `OpenCodeProtocolSettings{Enabled bool, Version string}` and `(*SettingService).GetOpenCodeProtocolSettings(context.Context) OpenCodeProtocolSettings`.
- Produces: `NormalizeOpenCodeProtocolVersion(string) (string, error)` for API validation and runtime fallback.

- [ ] **Step 1: Write failing settings tests**

Add tests asserting empty storage parses to disabled/`1.18.21`, valid values round-trip, invalid versions such as `"1.18 beta"` make `UpdateSettings` return a bad-request error before persistence, and a successful update refreshes the runtime policy.

```go
func TestOpenCodeProtocolSettingsDefaultsAndPersistence(t *testing.T) {
    repo := &settingUpdateRepoStub{}
    svc := &SettingService{settingRepo: repo}
    got := svc.parseSettings(map[string]string{})
    require.False(t, got.OpenCodeProtocolEnabled)
    require.Equal(t, "1.18.21", got.OpenCodeProtocolVersion)

    err := svc.UpdateSettings(context.Background(), &SystemSettings{
        OpenCodeProtocolEnabled: true,
        OpenCodeProtocolVersion: "1.19.0",
    })
    require.NoError(t, err)
    require.Equal(t, "true", repo.updates[SettingKeyOpenCodeProtocolEnabled])
    require.Equal(t, "1.19.0", repo.updates[SettingKeyOpenCodeProtocolVersion])
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `cd backend && go test ./internal/service -run 'TestOpenCodeProtocolSettings' -count=1`

Expected: compile failure because the OpenCode settings fields and keys do not exist.

- [ ] **Step 3: Add settings fields, keys, defaults, validation, persistence, and cache**

Use these exact contracts:

```go
const (
    SettingKeyOpenCodeProtocolEnabled = "opencode_protocol_enabled"
    SettingKeyOpenCodeProtocolVersion = "opencode_protocol_version"
    DefaultOpenCodeProtocolVersion    = "1.18.21"
)

type OpenCodeProtocolSettings struct {
    Enabled bool
    Version string
}

func (s *SettingService) GetOpenCodeProtocolSettings(ctx context.Context) OpenCodeProtocolSettings
```

Validate versions with an anchored dotted-numeric expression, normalize surrounding whitespace, persist both keys through `buildSystemSettingsUpdates`, and refresh an `atomic.Value` cache after writes. A repository failure returns the default policy.

- [ ] **Step 4: Run focused settings tests and verify GREEN**

Run: `cd backend && go test ./internal/service -run 'TestOpenCodeProtocolSettings' -count=1`

Expected: PASS.

### Task 2: OpenCode Session Preparation and Identity Helper

**Files:**
- Create: `backend/internal/service/openai_opencode_protocol.go`
- Create: `backend/internal/service/openai_opencode_protocol_test.go`
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Modify: `backend/internal/service/session_id.go`

**Interfaces:**
- Produces: `prepareOpenCodeProtocolRequest(*gin.Context, OpenCodeProtocolSettings) error`.
- Produces: `applyOpenCodeProtocolHeaders(http.Header, string, OpenCodeProtocolSettings)`.
- Produces request-context retrieval of the prepared canonical session value.

- [ ] **Step 1: Write failing pure-behavior tests**

Cover disabled no-op, generated `ses_` identity, each single existing header, conflicts using defined precedence, invalid control-character input, all-three equality, and exact `User-Agent`/`originator` overwrite.

```go
func TestPrepareOpenCodeProtocolRequestGeneratesSharedSession(t *testing.T) {
    c := newOpenCodeProtocolTestContext(t, nil)
    err := prepareOpenCodeProtocolRequest(c, OpenCodeProtocolSettings{Enabled: true, Version: "1.18.21"})
    require.NoError(t, err)
    sessionID := c.GetHeader(openCodeSessionHeader)
    require.True(t, strings.HasPrefix(sessionID, "ses_"))
    require.Equal(t, sessionID, c.GetHeader(openCodeSessionAffinityHeader))
    require.Equal(t, sessionID, c.GetHeader(openCodeSessionIDHeader))
}
```

- [ ] **Step 2: Run helper tests and verify RED**

Run: `cd backend && go test ./internal/service -run 'Test.*OpenCodeProtocol' -count=1`

Expected: compile failure because the protocol helpers do not exist.

- [ ] **Step 3: Implement the minimal protocol helper**

Define `openCodeSessionHeader = "Session-Id"`, reuse the existing OpenCode header constants, validate inbound values with the existing `sanitizeSessionID`, generate 24 random bytes with `crypto/rand`, encode with base64 raw URL encoding, and store the canonical value in Gin context. Extend explicit session-header resolution and usage correlation to include `Session-Id` without duplicating existing case-insensitive names.

- [ ] **Step 4: Run helper and existing session tests and verify GREEN**

Run: `cd backend && go test ./internal/service -run 'Test.*(OpenCodeProtocol|SessionID)' -count=1`

Expected: PASS.

### Task 3: OpenAI Responses HTTP Integration

**Files:**
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_gateway_passthrough.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Test: `backend/internal/service/openai_gateway_chat_completions_test.go`
- Test: `backend/internal/service/openai_oauth_passthrough_test.go`

**Interfaces:**
- Consumes: `GetOpenCodeProtocolSettings`, `prepareOpenCodeProtocolRequest`, and `applyOpenCodeProtocolHeaders` from Tasks 1-2.
- Guarantees final OpenCode identity runs after Codex normalization and account header overrides.

- [ ] **Step 1: Write failing normal and passthrough integration tests**

Enable the runtime setting in the test service, send a request without the three session headers, capture the upstream request, and assert the exact OpenCode UA, `originator`, and identical generated session headers. Add a test with account header overrides attempting to replace the identity and assert the final OpenCode values win. Add a disabled-mode regression assertion.

- [ ] **Step 2: Run integration tests and verify RED**

Run: `cd backend && go test ./internal/service -run 'Test.*OpenCodeProtocol.*(Forward|Passthrough)' -count=1`

Expected: FAIL because current Codex identity normalization wins and the session headers are absent.

- [ ] **Step 3: Prepare the session before scheduling and normalize both final outbound requests**

Expose `(*OpenAIGatewayService).PrepareOpenCodeProtocolRequest(context.Context, *gin.Context) error`, which loads the cached policy once and delegates to the pure helper. In the Responses handler, call it for `PlatformOpenAI` after `requestPlatform` is known and before `GenerateSessionHash`/account selection. In both HTTP request builders, call the final header helper after `account.ApplyHeaderOverrides`, Codex beta/routing helpers, and immediately before returning the request. Do not apply this mode to WebSocket forwarding.

- [ ] **Step 4: Run focused integration and OpenAI gateway tests**

Run: `cd backend && go test ./internal/service -run 'Test.*(OpenCodeProtocol|OpenAIGateway|Passthrough)' -count=1`

Expected: PASS.

### Task 4: Gateway Settings UI

**Files:**
- Modify: `frontend/src/api/admin/settings.ts`
- Modify: `frontend/src/views/admin/SettingsView.vue`
- Modify: `frontend/src/i18n/locales/en/admin/settings.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/settings.ts`
- Test: `frontend/src/views/admin/__tests__/SettingsView.spec.ts`

**Interfaces:**
- Consumes backend JSON fields `opencode_protocol_enabled` and `opencode_protocol_version`.
- Produces a Gateway Settings toggle plus conditional version input through the existing save payload.

- [ ] **Step 1: Write failing component assertions**

Extend the settings fixture with the two fields, assert the switch label renders, toggle it, assert the version input becomes enabled, change the version, save, and assert the update payload contains both exact JSON keys.

- [ ] **Step 2: Run the focused component test and verify RED**

Run: `cd frontend && pnpm vitest run src/views/admin/__tests__/SettingsView.spec.ts`

Expected: FAIL because the OpenCode controls and API fields do not exist.

- [ ] **Step 3: Add API types, form defaults, controls, payload, and translations**

Place a compact toggle row and version input beside the existing OpenAI Codex identity controls. Use the existing `Toggle` and `input` styles, bind `:disabled="!form.opencode_protocol_enabled"`, and add concise Chinese and English labels explaining HTTP-only scope.

- [ ] **Step 4: Run frontend tests and type checking**

Run: `cd frontend && pnpm vitest run src/views/admin/__tests__/SettingsView.spec.ts && pnpm type-check`

Expected: PASS.

### Task 5: Full Verification

**Files:**
- Verify only; no planned production edits.

**Interfaces:**
- Consumes all prior tasks.
- Produces release evidence for the requested feature.

- [ ] **Step 1: Format modified source files**

Run: `cd backend && gofmt -w internal/service/openai_opencode_protocol.go internal/service/openai_opencode_protocol_test.go internal/service/settings_view.go internal/service/domain_constants.go internal/service/setting_parse.go internal/service/setting_update.go internal/service/setting_service.go internal/service/setting_gateway_runtime.go internal/service/openai_gateway_scheduling.go internal/service/session_id.go internal/service/openai_gateway_forward.go internal/service/openai_gateway_passthrough.go internal/service/openai_gateway_service.go internal/handler/openai_gateway_handler.go`

- [ ] **Step 2: Run backend service tests**

Run: `cd backend && go test ./internal/service -count=1`

Expected: PASS.

- [ ] **Step 3: Run frontend verification**

Run: `cd frontend && pnpm vitest run src/views/admin/__tests__/SettingsView.spec.ts && pnpm type-check`

Expected: PASS.

- [ ] **Step 4: Check the final diff**

Run: `git diff --check && git status --short`

Expected: no whitespace errors; only the plan and requested implementation files are modified.
