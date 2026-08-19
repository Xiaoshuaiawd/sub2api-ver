# OpenAI `response.created` TTFT Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record OpenAI Responses streaming TTFT at the first upstream `response.created` event and relay that event to downstream clients immediately.

**Architecture:** Add a narrow event-type predicate for the `response.created` TTFT boundary and use it in the existing native SSE, passthrough SSE, and WebSocket relay loops. Keep the visible-content classifiers unchanged as the fallback for streams without `response.created`; keep each protocol's existing normalization, usage parsing, and write machinery.

**Tech Stack:** Go, Gin response writers, SSE, coder/gorilla WebSocket relays, `testify/require`.

## Global Constraints

- Apply only to streaming OpenAI Responses paths; do not change Chat Completions, Anthropic, Gemini, Bedrock, image-only, or non-streaming behavior.
- `first_token_ms = floor(response.created observation time - existing forward start time)` and is assigned once.
- A complete `response.created` SSE event or WebSocket frame must reach downstream immediately; SSE output must flush at the event boundary.
- If `response.created` is absent, retain the current first-visible-content or token-event fallback.
- After `response.created` is written, transparent account or transport failover is no longer allowed.
- Add no dependencies, schemas, configuration, or dashboard changes.
- Follow TDD: each production edit requires a failing behavioral test first.

---

### Task 1: Native and passthrough SSE boundaries

**Files:**
- Modify: `backend/internal/service/openai_visible_ttft_test.go`
- Modify: `backend/internal/service/openai_gateway_passthrough_flush_test.go`
- Modify: `backend/internal/service/openai_first_output_timeout_test.go`
- Modify: `backend/internal/service/openai_gateway_passthrough.go`
- Modify: `backend/internal/service/openai_gateway_response_handling.go`

**Interfaces:**
- Produces: `openAIStreamEventStartsTTFT(eventType string) bool` in package `service`.
- Consumes: existing `openAIStreamDataStartsVisibleOutput`, `openAIStreamDataStartsClientOutput`, guarded first-output staging, and protocol-specific flushers.

- [ ] **Step 1: Write failing SSE timing and flush tests**

Add a Gin response writer spy whose `Flush` method publishes a copied body snapshot. Drive both `handleStreamingResponse` and `handleStreamingResponsePassthrough` with an `io.Pipe`: write a complete `response.created`, wait until the test observes its flush, delay the visible delta, then finish the stream. Assert the first flush contains the complete created event and no delta, and assert `firstTokenMs` matches the created flush rather than the delayed delta.

Keep `TestOpenAIVisibleOutputClassification` unchanged for `response.created == false`, proving this feature does not redefine visible model output. Add a separate classification assertion for:

```go
require.True(t, openAIStreamEventStartsTTFT(" response.created "))
require.False(t, openAIStreamEventStartsTTFT("response.in_progress"))
require.False(t, openAIStreamEventStartsTTFT("response.output_text.delta"))
```

- [ ] **Step 2: Write failing fallback and failure-boundary tests**

Drive streams without `response.created` and assert the existing first-visible delta still determines `firstTokenMs`. Update passthrough boundary coverage so `response.created` flushes before a following heartbeat/delta, and so retryable `response.failed` after a flushed created event returns a normal post-output stream error rather than `UpstreamFailoverError`.

Update the first-output timeout tests to assert a complete `response.created` disarms the guard and is emitted; retain timeout coverage with only `response.in_progress` or an incomplete created event, which must remain uncommitted.

- [ ] **Step 3: Run the focused tests and verify RED**

Run:

```bash
cd backend
go test ./internal/service -run 'TestOpenAI(ResponsesTTFT|StreamingPassthrough|NativeFirstOutput|StreamEventStartsTTFT)' -count=1
```

Expected: the new created timing/flush tests and the changed failure-boundary assertions fail because created is still classified as buffered preamble and TTFT still waits for visible output.

- [ ] **Step 4: Implement the minimum SSE change**

Add the exact event predicate next to the existing stream classifiers:

```go
func openAIStreamEventStartsTTFT(eventType string) bool {
	return strings.TrimSpace(eventType) == "response.created"
}
```

In each SSE handler, calculate `startsTTFT` as created or the existing visible-output fallback. Let created participate in the existing client-output/progress boundary, use `startsTTFT` for the one-time timestamp, and force the existing event-boundary flush. Do not alter `openAIStreamDataStartsVisibleOutput`.

- [ ] **Step 5: Run the focused SSE tests and verify GREEN**

Run the command from Step 3. Expected: PASS with no races, warnings, or leaked writer goroutines.

- [ ] **Step 6: Commit the SSE behavior**

```bash
git add backend/internal/service/openai_visible_ttft_test.go \
  backend/internal/service/openai_gateway_passthrough_flush_test.go \
  backend/internal/service/openai_first_output_timeout_test.go \
  backend/internal/service/openai_gateway_passthrough.go \
  backend/internal/service/openai_gateway_response_handling.go
git commit -m "fix(openai): start responses TTFT at response.created"
```

### Task 2: HTTP ingress over Responses WebSocket

**Files:**
- Modify: `backend/internal/service/openai_ws_forwarder_success_test.go`
- Modify: `backend/internal/service/openai_ws_protocol_forward_test.go`
- Modify: `backend/internal/service/openai_ws_forwarder_v2.go`

**Interfaces:**
- Consumes: `openAIStreamEventStartsTTFT(eventType string) bool` from Task 1.
- Preserves: `isOpenAIWSTokenEvent` as the no-created fallback and token-event counter.

- [ ] **Step 1: Write failing early-flush and no-failover tests**

Add an HTTP streaming integration test whose upstream WebSocket sends `response.created`, blocks the delta behind a channel, and completes only after the test sees the Gin writer flush. Assert the flushed SSE contains created but no delta and the final `FirstTokenMs` precedes the delayed delta.

Change the created-then-early-close regression to assert the created SSE is returned and HTTP fallback is not attempted after downstream has observed it.

- [ ] **Step 2: Run the focused WebSocket forwarder tests and verify RED**

Run:

```bash
cd backend
go test ./internal/service -run 'TestOpenAIGatewayService_Forward_WSv2(StreamEarlyClose|.*Created)' -count=1
```

Expected: early flush times out or sees no created frame, and created-only close still falls back to HTTP.

- [ ] **Step 3: Implement immediate created output**

In `forwardOpenAIWSV2`, use created or the existing token event to assign `firstTokenMs`. For streaming HTTP downstream, exclude created from the pre-token buffer, emit it through `emitStreamMessage`, and pass `forceFlush=true` for created so configured batching cannot delay it. Leave token counters and non-streaming handling unchanged.

- [ ] **Step 4: Run the focused WebSocket forwarder tests and verify GREEN**

Run the command from Step 2. Expected: PASS and `httpUpstream.lastReq` remains nil after created is flushed.

- [ ] **Step 5: Commit the HTTP ingress bridge behavior**

```bash
git add backend/internal/service/openai_ws_forwarder_success_test.go \
  backend/internal/service/openai_ws_protocol_forward_test.go \
  backend/internal/service/openai_ws_forwarder_v2.go
git commit -m "fix(openai): flush websocket response.created to HTTP clients"
```

### Task 3: Native WebSocket and HTTP fallback relay timing

**Files:**
- Modify: `backend/internal/service/openai_ws_forwarder_ingress.go`
- Modify: `backend/internal/service/openai_ws_http_bridge.go`
- Modify: `backend/internal/service/openai_ws_v2/passthrough_relay.go`
- Modify: `backend/internal/service/openai_ws_v2/passthrough_relay_test.go`
- Modify: `backend/internal/service/openai_ws_v2_passthrough_lifecycle_test.go`
- Modify: `backend/internal/service/openai_ws_http_bridge_test.go`

**Interfaces:**
- Consumes in package `service`: `openAIStreamEventStartsTTFT(eventType string) bool`.
- Produces in package `openai_ws_v2`: a private exact-created predicate used by relay-level timing.
- Preserves: existing per-turn `startAt`, relay-wide `startAt`, frame writes, token counters, usage parsing, and terminal detection.

- [ ] **Step 1: Write failing deterministic relay timing tests**

Use `RelayOptions.Now` to give created, delayed delta, and terminal events distinct timestamps. Assert both `RelayResult.FirstTokenMs` and `RelayTurnResult.FirstTokenMs` equal the created offset. Add a no-created case that still records the first delta offset. Update the existing no-semantic-output sequence: a valid created event now yields non-nil TTFT even when output token usage is zero.

For service-level native WS and HTTP-bridge tests, assert the returned `OpenAIForwardResult.FirstTokenMs` is set by created before a deliberately delayed delta while the created frame is already observable downstream.

- [ ] **Step 2: Run the focused relay tests and verify RED**

Run:

```bash
cd backend
go test ./internal/service/openai_ws_v2 -run 'TestRelay_.*(Created|FirstToken|NoSemantic|Fallback)' -count=1
go test ./internal/service -run 'TestOpenAI.*(PassthroughLifecycle|HTTPBridge).*FirstToken' -count=1
```

Expected: relay timing equals the first delta or remains nil for created-only streams.

- [ ] **Step 3: Implement created timing in all WS relay observers**

In the service-package WS ingress and HTTP bridge loops, assign TTFT when the event is created or when the existing token fallback fires. In `openai_ws_v2.observeUpstreamMessage`, apply the exact same rule to relay-wide, active-turn, and response-ID turn timing. Do not change which events count toward token statistics or terminal status.

- [ ] **Step 4: Run the focused relay tests and verify GREEN**

Run both commands from Step 2. Expected: PASS with created offsets captured once and no-created fallback preserved.

- [ ] **Step 5: Commit WS relay timing**

```bash
git add backend/internal/service/openai_ws_forwarder_ingress.go \
  backend/internal/service/openai_ws_http_bridge.go \
  backend/internal/service/openai_ws_v2/passthrough_relay.go \
  backend/internal/service/openai_ws_v2/passthrough_relay_test.go \
  backend/internal/service/openai_ws_v2_passthrough_lifecycle_test.go \
  backend/internal/service/openai_ws_http_bridge_test.go
git commit -m "fix(openai): align websocket TTFT with response.created"
```

### Task 4: Regression verification

**Files:**
- Verify only; no planned production edits.

**Interfaces:**
- Consumes: all behavior from Tasks 1-3.
- Produces: verification evidence for completion.

- [ ] **Step 1: Run all focused OpenAI streaming tests**

```bash
cd backend
go test ./internal/service/openai_ws_v2 -count=1
go test ./internal/service -run 'TestOpenAI.*(TTFT|FirstOutput|StreamingPassthrough|WSv2|HTTPBridge|PassthroughLifecycle)' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run the broader backend service suite**

```bash
cd backend
go test ./internal/service/... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run formatting and repository checks**

```bash
gofmt -w backend/internal/service/openai_visible_ttft_test.go \
  backend/internal/service/openai_gateway_passthrough_flush_test.go \
  backend/internal/service/openai_first_output_timeout_test.go \
  backend/internal/service/openai_gateway_passthrough.go \
  backend/internal/service/openai_gateway_response_handling.go \
  backend/internal/service/openai_ws_forwarder_success_test.go \
  backend/internal/service/openai_ws_protocol_forward_test.go \
  backend/internal/service/openai_ws_forwarder_v2.go \
  backend/internal/service/openai_ws_forwarder_ingress.go \
  backend/internal/service/openai_ws_http_bridge.go \
  backend/internal/service/openai_ws_v2/passthrough_relay.go \
  backend/internal/service/openai_ws_v2/passthrough_relay_test.go
git diff --check
git status --short
```

Expected: formatting produces no additional semantic changes, `git diff --check` is silent, and status lists only this feature's files.

- [ ] **Step 4: Review the final diff against the design**

Confirm every production change traces to one of four requirements: created TTFT, created immediate relay, visible/token fallback, or post-created no-failover. Confirm `openAIStreamDataStartsVisibleOutput` and `isOpenAIWSTokenEvent` still exclude created and no unrelated protocols changed.
