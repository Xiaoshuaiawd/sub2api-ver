# OpenCode Protocol Global Gateway Mode

## Goal

Add a global gateway setting that makes OpenAI/Codex Responses requests use a consistent OpenCode HTTP identity. When enabled, the gateway supplies the three OpenCode session headers with one shared session value and normalizes the outbound OpenCode identity headers.

This mode improves HTTP-level protocol compatibility. It does not and cannot guarantee that an upstream service will identify the connection as a particular program because TLS, HTTP/2, IP, account, traffic-pattern, and server-side signals remain outside this feature's control.

## Scope

The feature applies only to OpenAI/Codex Responses-compatible outbound requests, including the normal HTTP forwarding path and the raw passthrough HTTP path. It applies to both OAuth and API-key OpenAI accounts when those paths target a Responses upstream.

The feature does not change Claude, Gemini, Antigravity, Grok-native, image, video, authentication, quota-probe, or administration requests. WebSocket transport is outside the initial scope because its handshake and connection-level state require separate compatibility rules.

## System Settings

Add two persisted system settings exposed in **System Settings > Gateway Settings**:

- `opencode_protocol_enabled`: boolean, default `false`.
- `opencode_protocol_version`: string, default `1.18.21`.

The version accepts a non-empty dotted numeric version such as `1.18.21`. Invalid values are rejected by the settings API. The settings page presents a toggle and a version input; the version input is disabled while the mode is off.

Settings use the existing system-settings repository, admin settings API, runtime cache, and invalidation behavior. Enabling or changing the feature takes effect without restarting the process.

## Request Behavior

### Session identity

The OpenCode session header set is:

- `session-id`
- `x-session-affinity`
- `x-session-id`

When the mode is enabled, the gateway resolves one canonical session value in this order:

1. A valid, non-empty inbound `session-id` value.
2. A valid, non-empty inbound `x-session-affinity` value.
3. A valid, non-empty inbound `x-session-id` value.
4. A newly generated value when all three are absent or invalid.

The generated form is `ses_<random>`. Randomness must come from a cryptographically secure generator. The resulting value must contain only HTTP-header-safe ASCII and stay below the existing 255-character session identifier limit.

After resolution, all three outbound headers are set to exactly the same canonical value. Conflicting inbound values are therefore normalized according to the precedence above. The resolved value is also made available to the existing sticky-session and usage-correlation logic so one request does not acquire a separate gateway session identity.

### OpenCode identity

Immediately before sending an eligible upstream request, the gateway overwrites the controllable HTTP identity fields:

- `User-Agent: opencode/<version> (darwin 24.6.0; arm64) ai-sdk/provider-utils/4.0.38 runtime/bun/1.3.14`
- `originator: opencode`
- `session-id: <canonical-session>`
- `x-session-affinity: <canonical-session>`
- `x-session-id: <canonical-session>`

The gateway preserves required authentication, account, content negotiation, content type, request body, and routing headers. It does not synthesize or alter access tokens, ChatGPT account IDs, source IPs, TLS fingerprints, or account ownership data.

This OpenCode identity step runs after the existing Codex identity normalization so the global mode has deterministic precedence. Account-level OpenAI header overrides must not overwrite these five identity fields while the mode is enabled.

When the mode is disabled, no OpenCode header generation or identity overwrite occurs and existing behavior remains unchanged.

## Components

### Settings model and API

Extend the existing `SystemSettings` read/update pipeline, default map, validation, persistence map, frontend API types, and Gateway Settings form. Reuse the current settings save operation rather than adding a dedicated endpoint.

### Runtime policy resolver

Expose the effective OpenCode policy to the OpenAI gateway through the existing runtime-settings cache pattern. The request hot path must not query the database directly. Cache invalidation after an admin save makes changes effective promptly.

### Session preparation and outbound normalization

Add one focused session helper that runs after request parsing and before account scheduling. It accepts the inbound request headers and effective policy, resolves or generates the canonical session ID, writes the three normalized headers to the inbound request, and stores the value in request context. Existing sticky-session and usage-correlation code can then consume the same normalized headers without a separate identity.

Add one outbound identity helper that reads the prepared session value, accepts the outbound `http.Header` and effective policy, and applies the complete OpenCode header set. Both normal and passthrough request builders call it at their final identity-normalization point. If a request reaches this point without prior preparation, the helper performs the same resolution once and returns an error if secure generation fails.

## Error Handling

- Failure to obtain secure random bytes fails the request before upstream dispatch; it must not fall back to a predictable value.
- Invalid or control-character-bearing inbound session values are ignored during resolution.
- An invalid configured version is rejected on settings update. A missing legacy value reads as the default `1.18.21`.
- Logs may record whether OpenCode mode was applied, but must not log authentication values or full request bodies.

## Tests

Backend tests must prove:

- Defaults are disabled with version `1.18.21`.
- Settings parse, validation, persistence, and cache refresh work.
- With the mode disabled, existing headers and identity behavior are unchanged.
- With all three headers absent, one `ses_` value is generated and copied to all three outbound headers.
- With one header present, it is preserved and copied to the other two.
- With conflicting headers, precedence is deterministic.
- Invalid inbound session values are ignored.
- Normal and passthrough HTTP request builders both apply the final OpenCode identity.
- Account header overrides cannot replace OpenCode identity fields while enabled.
- Unrelated protocol paths remain unchanged.

Frontend tests or type checks must prove that the Gateway Settings form loads and saves the toggle and version, disables the version input when appropriate, and renders both Chinese and English labels without layout regressions.

## Acceptance Criteria

1. An administrator can globally enable OpenCode protocol mode and select its version in Gateway Settings.
2. An eligible request missing all three OpenCode session headers reaches the upstream with all three present and identical.
3. Eligible outbound requests use the configured OpenCode `User-Agent` and `originator` regardless of inbound or account-level identity overrides.
4. Disabling the setting restores the existing behavior without a process restart.
5. Focused backend tests, the relevant frontend tests/type check, and formatting checks pass.
