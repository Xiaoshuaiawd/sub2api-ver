# OpenAI `response.created` TTFT Design

## Goal

For streaming OpenAI Responses requests, treat the upstream `response.created`
event as the first-token boundary. Relay the complete SSE event to the client
immediately, flush it, and store its elapsed time in `first_token_ms`.

This restores the user-visible behavior from the older relay implementation
without restoring its overly broad rule that counted every non-empty SSE data
event as a first token.

## Scope

Apply the behavior to OpenAI Responses streaming paths that can receive a
`response.created` event:

- native HTTP/SSE response handling;
- HTTP/SSE passthrough response handling;
- OpenAI Responses WebSocket relays, including HTTP ingress bridge output.

Do not change Chat Completions, Anthropic Messages, Gemini, Bedrock, image-only
endpoints, non-streaming requests, database schemas, or dashboard aggregation.

## Event Semantics

The first-token timestamp uses the existing forwarding start time:

```text
first_token_ms = floor(response.created observation time - forward start time)
```

When the first valid `response.created` event arrives:

1. preserve all existing response normalization and model restoration;
2. write the complete SSE or WebSocket-derived SSE event to the downstream;
3. flush the downstream writer at the event boundary;
4. set `first_token_ms` once, using the existing forwarding start time;
5. mark downstream output as started.

If a stream never contains `response.created`, retain the current first-visible-
content detection as a fallback. Empty deltas, keepalives, usage-only terminal
events, and `[DONE]` do not independently start TTFT.

The timestamp remains server-observed. It represents when the gateway handles
and flushes `response.created`, not a client-side network acknowledgment.

## Failure Boundary

Before `response.created` is flushed, current safe failover behavior remains
available. After it is flushed, the HTTP response is committed and the request
must not be transparently replayed through another account. Later upstream
failures follow the existing post-output stream-error behavior.

First-output timeout and staging logic must treat `response.created` as progress
that commits the attempt. Pre-output buffers must be released at that event.

## Implementation Shape

Use a small shared event-type predicate for the `response.created` boundary and
reuse it in the native HTTP, passthrough, and WebSocket paths. Keep protocol-
specific writing and flushing in their existing handlers; do not introduce a
new relay abstraction.

The existing visible-output predicate remains unchanged and acts only as the
fallback when `first_token_ms` is still unset.

## Verification

Tests must prove:

- native HTTP/SSE records TTFT at `response.created`, before a delayed text
  delta, and flushes the created event immediately;
- passthrough HTTP/SSE has the same behavior;
- WebSocket-derived streaming records and relays `response.created` before a
  delayed token event;
- first-visible-content fallback still works when `response.created` is absent;
- `response.created` followed by an upstream failure is treated as a
  post-output failure and is not transparently failed over;
- non-streaming and non-Responses paths remain unchanged.

Focused service tests must pass before the broader backend service test suite is
run.
