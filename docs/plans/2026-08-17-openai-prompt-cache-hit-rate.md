# OpenAI Prompt Cache Hit Rate Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve prompt-cache reuse for `/v1/chat/completions` and `/v1/responses` by separating account stickiness from cache routing, sharing one canonical stable-prefix fingerprint, and using configurable fixed-lifetime Redis UUIDv7 identities.

**Architecture:** Preserve the existing `GenerateSessionHash` exclusively for local account scheduling. Resolve the upstream `prompt_cache_key` only after an account is selected, from the final mapped model plus a protocol-neutral stable-prefix hash and deterministic shard. Reuse the current Redis Lua fixed-TTL pattern under v2 keys, and add narrowly gated GPT-5.6 explicit breakpoints plus request-eligibility metrics.

**Tech Stack:** Go 1.24, Gin, gjson/sjson, go-redis v9, miniredis, UUIDv7, testify, Viper.

**Design:** `docs/superpowers/specs/2026-08-17-openai-prompt-cache-hit-rate-design.md`

---

### Task 1: Add Safe Prompt-Cache Runtime Configuration

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `deploy/config.example.yaml`

**Step 1: Write failing config tests**

Add tests that load explicit values and verify zero/invalid values normalize to safe defaults:

```go
func TestOpenAIPromptCacheConfigDefaults(t *testing.T) {
    cfg := GatewayOpenAIPromptCacheConfig{}
    require.Equal(t, 1800, cfg.IdentityTTLSecondsValue())
    require.Equal(t, 4, cfg.ShardCountValue())
    require.True(t, cfg.ExplicitBreakpointsEnabledValue())
}

func TestLoadOpenAIPromptCacheConfig(t *testing.T) {
    resetViperWithJWTSecret(t)
    viper.Set("gateway.openai_prompt_cache.identity_ttl_seconds", 3600)
    viper.Set("gateway.openai_prompt_cache.shard_count", 8)
    viper.Set("gateway.openai_prompt_cache.explicit_breakpoints_enabled", false)
    cfg, err := Load()
    require.NoError(t, err)
    require.Equal(t, 3600, cfg.Gateway.OpenAIPromptCache.IdentityTTLSecondsValue())
    require.Equal(t, 8, cfg.Gateway.OpenAIPromptCache.ShardCountValue())
    require.False(t, cfg.Gateway.OpenAIPromptCache.ExplicitBreakpointsEnabledValue())
}
```

Use an explicit internal `explicitBreakpointsConfigured` marker, or equivalent `*bool`, so an omitted setting defaults to true while an explicitly configured false remains false.

**Step 2: Run tests and verify RED**

Run:

```bash
cd backend && go test ./internal/config -run 'Test(OpenAIPromptCacheConfigDefaults|LoadOpenAIPromptCacheConfig)' -count=1
```

Expected: compile failure because `GatewayOpenAIPromptCacheConfig` and accessors do not exist.

**Step 3: Implement config and defaults**

Add:

```go
const (
    DefaultOpenAIPromptCacheIdentityTTLSeconds = 1800
    MinOpenAIPromptCacheIdentityTTLSeconds = 300
    MaxOpenAIPromptCacheIdentityTTLSeconds = 86400
    DefaultOpenAIPromptCacheShardCount = 4
)

type GatewayOpenAIPromptCacheConfig struct {
    IdentityTTLSeconds         int   `mapstructure:"identity_ttl_seconds"`
    ShardCount                int   `mapstructure:"shard_count"`
    ExplicitBreakpointsEnabled *bool `mapstructure:"explicit_breakpoints_enabled"`
}
```

Add `OpenAIPromptCache GatewayOpenAIPromptCacheConfig` to `GatewayConfig`, accessors that return default values for zero/invalid direct-construction cases, Viper defaults, and example YAML. Accept shard counts only from `1,4,8,16`.

**Step 4: Run config tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/config -run 'OpenAIPromptCache' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go deploy/config.example.yaml
git commit -m "feat: configure openai prompt cache routing"
```

---

### Task 2: Build One Canonical Stable-Prefix Fingerprint

**Files:**
- Create: `backend/internal/service/openai_prompt_cache_prefix.go`
- Create: `backend/internal/service/openai_prompt_cache_prefix_test.go`
- Modify: `backend/internal/service/openai_content_session_seed.go`
- Modify: `backend/internal/service/openai_content_session_seed_test.go`

**Step 1: Write failing equivalence tests**

Define protocol-equivalent Chat and Responses bodies and require the same hash:

```go
func TestOpenAIPromptCachePrefixChatAndResponsesEquivalent(t *testing.T) {
    chat := []byte(`{
      "model":"client-alias",
      "messages":[
        {"role":"system","content":"be concise"},
        {"role":"user","content":"first question"}
      ],
      "tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
      "response_format":{"type":"json_object"},
      "parallel_tool_calls":true
    }`)
    responses := []byte(`{
      "model":"ignored-inbound-model",
      "input":[
        {"type":"message","role":"system","content":[{"type":"input_text","text":"be concise"}]},
        {"type":"message","role":"user","content":"different question"}
      ],
      "tools":[{"type":"function","name":"lookup","parameters":{"type":"object"},"strict":false}],
      "text":{"format":{"type":"json_object"}},
      "parallel_tool_calls":true
    }`)
    a, okA := deriveOpenAIPromptCachePrefix(chat, "gpt-5.6-sol")
    b, okB := deriveOpenAIPromptCachePrefix(responses, "gpt-5.6-sol")
    require.True(t, okA)
    require.True(t, okB)
    require.Equal(t, a.Hash, b.Hash)
}
```

Add table tests proving:

- changing user/assistant/tool-output content, IDs, metadata, stream, or service tier does not change the hash;
- changing final model family, ordered system/developer content, tools, schema, reasoning/tool choice, or `parallel_tool_calls` changes the hash;
- object key order is ignored but array order is preserved;
- string text and a single text content block normalize identically;
- a request with only user content returns `ok=false`.

**Step 2: Run tests and verify RED**

Run:

```bash
cd backend && go test ./internal/service -run 'TestOpenAIPromptCachePrefix' -count=1
```

Expected: compile failure because `deriveOpenAIPromptCachePrefix` does not exist.

**Step 3: Implement the canonical AST**

Create small internal types with stable JSON field order:

```go
type openAIPromptCachePrefix struct {
    Hash      string
    Canonical []byte
}

type openAICanonicalPromptPrefix struct {
    Version           int               `json:"version"`
    ModelFamily       string            `json:"model_family"`
    Instructions      []canonicalPrompt `json:"instructions,omitempty"`
    Tools             []json.RawMessage `json:"tools,omitempty"`
    TextFormat        json.RawMessage   `json:"text_format,omitempty"`
    Reasoning         json.RawMessage   `json:"reasoning,omitempty"`
    ToolChoice        json.RawMessage   `json:"tool_choice,omitempty"`
    ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
}
```

Implementation rules:

1. Use the explicit final model argument; never trust inbound `model` for the canonical model.
2. Convert Chat system/developer messages and Responses system/developer input items to ordered `{role, text-blocks}` entries.
3. Convert legacy `functions` and Chat nested function tools into the same Responses-style tool object used by native Responses.
4. Reuse the existing Chat response-format mapping semantics so `response_format` and `text.format` converge.
5. Canonicalize JSON objects recursively with sorted keys; preserve arrays.
6. Return SHA-256 hex and never expose canonical bytes in logs.

Replace `deriveOpenAIStablePrefixSessionSeed` internals with this canonical helper where safe, keeping its public behavior for Grok tests by returning a namespaced hash rather than raw prompt material.

**Step 4: Run focused tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/service -run 'Test(OpenAIPromptCachePrefix|DeriveOpenAIStablePrefix)' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/service/openai_prompt_cache_prefix.go backend/internal/service/openai_prompt_cache_prefix_test.go backend/internal/service/openai_content_session_seed.go backend/internal/service/openai_content_session_seed_test.go
git commit -m "feat: canonicalize openai prompt cache prefixes"
```

---

### Task 3: Upgrade Redis Identities to Fixed-TTL v2 Sources

**Files:**
- Modify: `backend/internal/service/openai_prompt_cache_identity_store.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity_test.go`
- Modify: `backend/internal/repository/openai_prompt_cache_identity_cache.go`
- Modify: `backend/internal/repository/openai_prompt_cache_identity_cache_test.go`

**Step 1: Write failing source/shard tests**

Introduce a safe store argument that contains no raw prompt/session text:

```go
type OpenAIPromptCacheIdentitySource struct {
    Kind       string
    Hash       string
    ShardCount int
    ShardIndex int
}
```

Test:

- same prefix hash and same shard reuse one UUID;
- different shard, shard-count generation, tenant, or final model produce different Redis keys;
- session fallback uses `Kind=session` and a pre-hashed value;
- key strings contain `v2`, shard generation/index, and hashes only;
- 30-minute default TTL does not renew on hit;
- expiry creates a new UUIDv7;
- response aliases use v2 and inherit the main record's remaining TTL.

Example assertion:

```go
source := OpenAIPromptCacheIdentitySource{Kind: "prefix", Hash: strings.Repeat("a", 64), ShardCount: 4, ShardIndex: 2}
key, err := openAIPromptCacheIdentityKey(7, "gpt-5.6-sol", source)
require.NoError(t, err)
require.Contains(t, key, "openai:prompt_cache_identity:v2:7:")
require.Contains(t, key, ":n4:s2:")
require.NotContains(t, key, "be concise")
```

**Step 2: Run tests and verify RED**

Run:

```bash
cd backend && go test ./internal/repository ./internal/service -run 'OpenAIPromptCacheIdentity' -count=1
```

Expected: compile failures from the new source type/signature and v2 expectations.

**Step 3: Implement v2 store keys and configurable TTL**

Change `ResolveOpenAIPromptCacheIdentity` to accept the safe source struct. Validate SHA-256 hex length and shard bounds before constructing a key. Use:

```text
openai:prompt_cache_identity:v2:{apiKeyID}:{modelHash}:n{N}:s{S}:{prefixHash}
openai:prompt_cache_identity:v2:{apiKeyID}:{modelHash}:session:{sessionHash}
openai:prompt_cache_response_alias:v2:{apiKeyID}:{modelHash}:{responseIDHash}
```

Keep the Lua GET/PTTL/SET PX behavior unchanged. Resolve TTL through `s.cfg.Gateway.OpenAIPromptCache.IdentityTTL()` with a 30-minute zero-config fallback. Expand the staged identity cache key to include model plus source so failover can safely resolve a different final model/source.

**Step 4: Run focused tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/repository ./internal/service -run 'OpenAIPromptCacheIdentity' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/service/openai_prompt_cache_identity_store.go backend/internal/service/openai_prompt_cache_identity.go backend/internal/service/openai_prompt_cache_identity_test.go backend/internal/repository/openai_prompt_cache_identity_cache.go backend/internal/repository/openai_prompt_cache_identity_cache_test.go
git commit -m "feat: shard redis prompt cache identities"
```

---

### Task 4: Separate Account Stickiness from Cache Routing

**Files:**
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_prompt_cache_identity_test.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity_test.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity_forward_test.go`

**Step 1: Replace coupling tests with failing independence tests**

Delete expectations that automatic UUIDs replace `sessionHash` or force account reselection. Add:

```go
func TestResolveOpenAIChatPromptCacheRoutingNeverChangesSessionHash(t *testing.T) {
    identity := "019b2745-7252-73f1-93da-ede850329211"
    routing := resolveOpenAIChatPromptCacheRouting(
        []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
        "conversation-session-hash",
        "header-session",
        identity,
    )
    require.Equal(t, identity, routing.PromptCacheKey)
    require.Equal(t, "conversation-session-hash", routing.SessionHash)
}
```

Add handler/service tests proving the resolver is invoked after account selection with `account.GetMappedModel(reqModel)`/the normalized final family, and that failover to the same final model/source reuses the staged UUID while a different final model resolves a separate UUID without reselecting the account because of UUID rotation.

Add deterministic shard tests:

```go
require.Equal(t, shardForSessionHash("stable-session", 4), shardForSessionHash("stable-session", 4))
require.Less(t, shardForSessionHash("stable-session", 4), 4)
```

**Step 2: Run tests and verify RED**

Run:

```bash
cd backend && go test ./internal/handler ./internal/service -run 'PromptCacheIdentity|PromptCacheRouting' -count=1
```

Expected: old behavior fails because it derives `SessionHash` from the automatic UUID and resolves before account selection.

**Step 3: Implement post-selection cache resolution**

Change the resolver API to accept `sessionHash`, final model, and body:

```go
func (s *OpenAIGatewayService) ResolveAndStageOpenAIAutoPromptCacheIdentity(
    ctx context.Context,
    c *gin.Context,
    apiKeyID int64,
    finalModel string,
    sessionHash string,
    body []byte,
) string
```

Resolution order:

1. explicit body `prompt_cache_key` -> skip automatic store;
2. canonical stable prefix -> source `{Kind:prefix, Hash:prefix.Hash, ShardCount:N, ShardIndex:hash(sessionHash)%N}`;
3. no prefix but non-empty `sessionHash` -> `{Kind:session, Hash:sessionHash}`;
4. otherwise `no_source`.

In both handlers:

- generate `sessionHash` before selection and never replace it with automatic identity;
- select/acquire the account first;
- resolve the cache identity immediately before deriving the attempt body, using the selected account's final mapped/normalized model;
- inject the staged identity into the attempt body;
- remove UUID-rotation-driven account reselection and the old `SessionHashUsesAutoIdentity` fields/helpers.

Keep explicit client key priority and Redis fail-open behavior.

**Step 4: Run focused tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/handler ./internal/service -run 'PromptCacheIdentity|PromptCacheRouting' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/handler/openai_chat_completions.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/openai_prompt_cache_identity_test.go backend/internal/service/openai_prompt_cache_identity.go backend/internal/service/openai_prompt_cache_identity_test.go backend/internal/service/openai_prompt_cache_identity_forward_test.go
git commit -m "fix: separate openai cache routing from account stickiness"
```

---

### Task 5: Preserve Stable Conversation Signals and Previous Response Chains

**Files:**
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Modify: `backend/internal/service/openai_gateway_scheduling_test.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity_test.go`
- Modify: `backend/internal/pkg/apicompat/types.go`
- Modify: `backend/internal/pkg/apicompat/chatcompletions_to_responses.go`
- Modify: `backend/internal/pkg/apicompat/chatcompletions_responses_test.go`

**Step 1: Write failing signal and conversion tests**

Add table tests for body fields:

```go
tests := []struct{name, body string}{
    {"thread_id", `{"thread_id":"thread-1"}`},
    {"metadata_thread", `{"metadata":{"thread_id":"thread-1"}}`},
    {"client_metadata_conversation", `{"client_metadata":{"conversation_id":"conv-1"}}`},
}
```

Require each to produce a stable `GenerateSessionHash`, while ordinary `id`, request IDs, message IDs, and tool-call IDs remain ignored.

Add a bridge test:

```go
func TestChatCompletionsToResponsesPreservesPreviousResponseID(t *testing.T) {
    req := &ChatCompletionsRequest{
        Model: "gpt-5.6-sol",
        PreviousResponseID: "resp_chain_1",
        Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`"next"`)}},
    }
    got, err := ChatCompletionsToResponses(req)
    require.NoError(t, err)
    require.Equal(t, "resp_chain_1", got.PreviousResponseID)
}
```

**Step 2: Run tests and verify RED**

Run:

```bash
cd backend && go test ./internal/service ./internal/pkg/apicompat -run 'SessionHash.*(Thread|Conversation)|PreviousResponseID' -count=1
```

Expected: failures because those body variants and the Chat request field are not supported.

**Step 3: Implement one shared stable-signal resolver**

Extract a helper used by scheduling and prompt-cache alias handling. Preserve the priority documented in the design. Sanitize values through existing `sanitizeSessionID`; return only the value to hashing code and only the source kind to logs.

Add:

```go
PreviousResponseID string `json:"previous_response_id,omitempty"`
```

to `ChatCompletionsRequest`, copy it in `ChatCompletionsToResponses`, and keep the existing final-upstream capability cleanup. Continue accepting only `resp_*` for response aliases.

**Step 4: Run focused tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/service ./internal/pkg/apicompat -run 'SessionHash|PreviousResponseID|PromptCacheIdentity' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/service/openai_gateway_scheduling.go backend/internal/service/openai_gateway_scheduling_test.go backend/internal/service/openai_prompt_cache_identity.go backend/internal/service/openai_prompt_cache_identity_test.go backend/internal/pkg/apicompat/types.go backend/internal/pkg/apicompat/chatcompletions_to_responses.go backend/internal/pkg/apicompat/chatcompletions_responses_test.go
git commit -m "feat: preserve openai conversation cache signals"
```

---

### Task 6: Add Capability-Gated GPT-5.6 Explicit Breakpoints

**Files:**
- Create: `backend/internal/service/openai_prompt_cache_breakpoint.go`
- Create: `backend/internal/service/openai_prompt_cache_breakpoint_test.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions_test.go`
- Modify: `backend/internal/service/openai_responses_rejected_field_retry.go`
- Modify: `backend/internal/service/openai_responses_rejected_field_retry_test.go`

**Step 1: Write failing breakpoint matrix tests**

Test the helper against:

- GPT-5.6 direct OpenAI API Key Responses body with developer/system input text -> inject `prompt_cache_options.mode=explicit` and one final stable `prompt_cache_breakpoint`;
- OAuth/Codex, pre-5.6, non-OpenAI platform, raw Chat upstream, unknown capability, disabled config -> unchanged;
- top-level string `instructions` without a developer/system input block -> unchanged;
- client-owned `prompt_cache_options` or any existing breakpoint -> unchanged;
- multiple stable messages -> mark only the last supported text block;
- rejected automatic field -> strip only fields tagged as automatically injected and retry once.

Example:

```go
patched, decision, err := injectOpenAIPromptCacheBreakpoint(cfg, apiKeyAccount, "gpt-5.6-sol", body)
require.NoError(t, err)
require.True(t, decision.Injected)
require.Equal(t, "explicit", gjson.GetBytes(patched, "prompt_cache_options.mode").String())
require.Equal(t, "explicit", gjson.GetBytes(patched, "input.0.content.0.prompt_cache_breakpoint.mode").String())
```

**Step 2: Run tests and verify RED**

Run:

```bash
cd backend && go test ./internal/service -run 'PromptCacheBreakpoint|RejectedField.*PromptCache' -count=1
```

Expected: compile failure because the breakpoint helper does not exist.

**Step 3: Implement conservative capability gating**

Implement:

```go
type openAIPromptCacheBreakpointDecision struct {
    Injected bool
    Reason   string
}
```

Gate on all design conditions: enabled config, OpenAI platform, API Key account, Responses-shaped outbound path, known GPT-5.6+ family, no client-owned cache policy, and a supported `input[*].content[*]` text block. Never reshape top-level `instructions` merely to attach a breakpoint.

Apply the helper after Chat->Responses conversion for Chat ingress and after final body normalization for native Responses. Mark auto-injected fields in Gin context so the rejected-field retry may remove only those fields. Extend rejected-parameter classification for `prompt_cache_options` and `prompt_cache_breakpoint`; limit to one bounded retry.

**Step 4: Run focused tests and verify GREEN**

Run:

```bash
cd backend && go test ./internal/service -run 'PromptCacheBreakpoint|RejectedField|ForwardAsChatCompletions.*PromptCache' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/service/openai_prompt_cache_breakpoint.go backend/internal/service/openai_prompt_cache_breakpoint_test.go backend/internal/service/openai_gateway_forward.go backend/internal/service/openai_gateway_chat_completions.go backend/internal/service/openai_gateway_chat_completions_test.go backend/internal/service/openai_responses_rejected_field_retry.go backend/internal/service/openai_responses_rejected_field_retry_test.go
git commit -m "feat: inject gpt-5.6 prompt cache breakpoints"
```

---

### Task 7: Add Eligible-Request Cache Metrics and Safe Diagnostics

**Files:**
- Create: `backend/internal/service/openai_prompt_cache_metrics.go`
- Create: `backend/internal/service/openai_prompt_cache_metrics_test.go`
- Create: `backend/internal/handler/openai_prompt_cache_metrics.go`
- Create: `backend/internal/handler/openai_prompt_cache_metrics_test.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/service/openai_prompt_cache_identity.go`

**Step 1: Write failing metric tests**

Define a snapshot with atomic counters:

```go
type OpenAIPromptCacheMetricsSnapshot struct {
    EligibleRequests  int64
    EligibleHits      int64
    EligibleInput     int64
    EligibleCached    int64
    EligibleHitRate   float64
    EligibleTokenRate float64
}
```

Test:

- GPT-5.6 input 1023 is excluded and 1024 included;
- earlier/unknown input 2047 is excluded and 2048 included;
- eligible request with `cached_tokens > 0` increments hits;
- eligible token rate is cached/total eligible input;
- short requests never enter the eligible denominator;
- snapshots are race-safe under concurrent records;
- diagnostic logging contains only SHA-256 values, source kind, shard, endpoints, model, TTL, breakpoint decision, and token counts.

**Step 2: Run tests and verify RED**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'PromptCacheMetrics' -count=1
```

Expected: compile failure because the metrics types and recorder do not exist.

**Step 3: Implement metrics and periodic snapshots**

Add atomic counters and:

```go
func RecordOpenAIPromptCacheOutcome(model string, inputTokens, cachedTokens int)
func SnapshotOpenAIPromptCacheMetrics() OpenAIPromptCacheMetricsSnapshot
```

Record exactly once after a successful terminal result in each handler. Use final upstream model and existing `OpenAIForwardResult.Usage`. Add a handler helper modeled on `maybeLogCompatibilityFallbackMetrics` that logs a cumulative snapshot every fixed number of successful OpenAI requests; keep per-request decision logs at debug level.

Expand `OpenAIPromptCacheIdentityDecision` with safe fields: prefix SHA-256, identity SHA-256, shard count/index, and breakpoint reason. Do not store canonical bytes or raw identifiers in the decision.

Do not change Channel Monitor's existing `cached_tokens / all_prompt_tokens` calculation.

**Step 4: Run focused tests and race test**

Run:

```bash
cd backend && go test ./internal/service ./internal/handler -run 'PromptCacheMetrics|PromptCacheIdentityDecision' -count=1
cd backend && go test -race ./internal/service -run 'PromptCacheMetrics' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/internal/service/openai_prompt_cache_metrics.go backend/internal/service/openai_prompt_cache_metrics_test.go backend/internal/handler/openai_prompt_cache_metrics.go backend/internal/handler/openai_prompt_cache_metrics_test.go backend/internal/handler/openai_chat_completions.go backend/internal/handler/openai_gateway_handler.go backend/internal/service/openai_prompt_cache_identity.go
git commit -m "feat: observe eligible openai prompt cache hits"
```

---

### Task 8: Complete Regression Verification and Documentation

**Files:**
- Modify if test gaps require it: focused `*_test.go` files from Tasks 1-7
- Modify: `docs/superpowers/specs/2026-08-17-openai-prompt-cache-hit-rate-design.md` only if implementation exposes a verified discrepancy

**Step 1: Run formatting and static diff checks**

Run:

```bash
cd backend && gofmt -w internal/config/config.go internal/config/config_test.go internal/service/openai_prompt_cache_*.go internal/handler/openai_prompt_cache_*.go internal/handler/openai_chat_completions.go internal/handler/openai_gateway_handler.go internal/repository/openai_prompt_cache_identity_cache*.go internal/pkg/apicompat/types.go internal/pkg/apicompat/chatcompletions_to_responses.go internal/pkg/apicompat/chatcompletions_responses_test.go
git diff --check
```

Expected: no output from `git diff --check`.

**Step 2: Run focused regression suites**

Run:

```bash
cd backend && go test ./internal/config ./internal/repository ./internal/pkg/apicompat ./internal/service ./internal/handler -run 'PromptCache|SessionHash|PreviousResponseID|ChatCompletionsToResponses' -count=1
```

Expected: PASS.

**Step 3: Run the full unit suite**

Run:

```bash
cd backend && go test -tags unit ./internal/... -count=1
```

Expected: PASS.

**Step 4: Run targeted race suites**

Run:

```bash
cd backend && go test -race ./internal/repository -run 'OpenAIPromptCacheIdentity' -count=1
cd backend && go test -race ./internal/service -run 'OpenAIPromptCache(Identity|Metrics|Prefix)' -count=1
```

Expected: PASS.

**Step 5: Review behavior against acceptance criteria**

Verify from tests/diffs:

- Chat and Responses equivalent prefixes share a UUID within the same shard;
- user content does not affect shared prefix identity;
- automatic UUID never changes account scheduling hash;
- fixed 30-minute TTL never slides;
- OAuth and API Key Responses receive auto keys;
- only gated GPT-5.6 API Key Responses receive breakpoints;
- Redis and breakpoint failures remain fail-open;
- short requests do not enter eligible metrics.

**Step 6: Commit final test/doc adjustments**

```bash
git add backend docs/superpowers/specs/2026-08-17-openai-prompt-cache-hit-rate-design.md
git commit -m "test: verify openai prompt cache routing"
```

Skip this commit when there are no final adjustments; do not create an empty commit.

---

## Final Review Checklist

- Inspect `git status --short` and ensure no unrelated user files are staged.
- Inspect every commit with `git log --stat --oneline <base>..HEAD`.
- Inspect the complete diff for raw prompt/session logging and accidental billing-model changes.
- Confirm Redis keys contain only tenant ID plus hashes/shard metadata.
- Confirm no SSE buffering or first-token path was introduced.
- Confirm no automatic cache identity participates in account selection or sticky binding.
- Run `git diff --check` once more after any review fix.
