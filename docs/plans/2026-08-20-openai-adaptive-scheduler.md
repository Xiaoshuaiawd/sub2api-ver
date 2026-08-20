# OpenAI Adaptive Scheduler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the OpenAI request-level full-pool Redis scheduler with a version-aware local snapshot, bounded local adaptive concurrency, sampled fair selection, and immediate 429 protection while keeping the legacy scheduler as a feature-flag rollback path.

**Architecture:** Static account eligibility remains owned by `SchedulerSnapshotService`, but decoded bucket snapshots are cached locally by active Redis version. When `adaptive_enabled` is on, OpenAI load balancing uses a process-local sharded runtime state and Power-of-Four sampling, reserves a local permit before the one final Redis slot acquire, and waits on release notifications for at most one second. Existing runtime account blocks remain authoritative and receive the first 429 synchronously; AIMD and route-storm state are updated in the same local fast path.

**Tech Stack:** Go 1.x, `sync`/`sync/atomic`, go-redis v9, Viper, Testify, miniredis-backed repository tests.

## Global Constraints

- One machine, approximately 3000 accounts, target 200 RPS and 250 RPS burst for 60 seconds.
- Scheduling wait timeout is 1 second; failover scheduling budget is 800 milliseconds.
- A request may use at most 2 distinct accounts, so the OpenAI account switch count is at most 1.
- No fixed ingress limit by user or API key; only physical scheduler wait capacity is bounded.
- `codex_7d_used_percent=100` is observation only and must not reject an account that can spend credits.
- Redis normal hot path target is 0-3 commands per business request; PostgreSQL must not participate in request-level scheduling.
- No new external dependency. Legacy scheduling remains available when `adaptive_enabled=false`.
- Default rollout is `adaptive_enabled=false`, `shadow_mode=true`.

---

### Task 1: Safe Configuration and OpenAI Failover Bound

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Test: `backend/internal/handler/openai_gateway_handler_test.go`
- Modify: `deploy/config.example.yaml`

**Interfaces:**
- Produces: `config.GatewayOpenAISchedulerConfig` fields for every adaptive scheduler constant.
- Produces: `OpenAIGatewayHandler.maxAccountSwitches <= MaxDistinctAccountAttempts-1` while adaptive mode is enabled.

- [x] **Step 1: Write failing default, validation, and handler construction tests**

Assert the exact defaults: disabled/shadow, windows `2/1/32`, sample `4x2`, sticky escape `0.8`, wait `1000ms`, failover `800ms`, distinct attempts `2`, storm `5s/20/0.20/0.05`, waiter cap `1000`, DB `64/24`, Redis `512/64`. Assert invalid window ordering, ratios, durations, sample sizes, attempt count, and waiter cap fail validation. Assert an adaptive handler constructed from a legacy switch value of 10 stores one switch.

- [x] **Step 2: Run tests and verify RED**

Run: `cd backend && go test ./internal/config ./internal/handler -run 'TestLoadDefaultOpenAIAdaptiveSchedulerConfig|TestValidateOpenAIAdaptiveSchedulerConfig|TestNewOpenAIGatewayHandlerAdaptiveFailoverCap' -count=1`

Expected: compile failures for missing fields.

- [x] **Step 3: Add typed config, defaults, validation, and deploy example**

Use millisecond integer fields at the config boundary and convert to `time.Duration` in service helpers. Cap handler switches only when adaptive mode is enabled; preserve existing behavior otherwise.

- [x] **Step 4: Run focused tests and commit**

Run the Step 2 command and `go test ./internal/config ./internal/handler -run 'TestLoadDefaultOpenAIWSConfig|TestNewOpenAIGatewayHandler' -count=1`.

Commit: `feat: configure adaptive openai scheduler`

### Task 2: Version-Aware Scheduler Snapshot L1

**Files:**
- Modify: `backend/internal/repository/scheduler_cache.go`
- Test: `backend/internal/repository/scheduler_cache_l1_test.go`

**Interfaces:**
- Produces: an immutable `schedulerSnapshotL1Entry{version string, accounts []*service.Account}` per `SchedulerBucket`.
- Consumes: existing Redis `ready` and `active` keys; no SchedulerCache interface change.

- [x] **Step 1: Write failing L1 command-count and invalidation tests**

Publish version 1, read twice, and assert the second read performs only ready/active validation and no `ZRANGE`/account `MGET`. Publish version 2 and assert the next read reloads exactly version 2. Mutating a returned account must not mutate a later result. Retire/reopen must evict stale local data.

- [x] **Step 2: Run tests and verify RED**

Run: `cd backend && go test ./internal/repository -run 'TestSchedulerCacheSnapshotL1' -count=1`

Expected: command-count or isolation assertion fails because every read hydrates Redis and returns shared pointers only within that call.

- [x] **Step 3: Implement immutable, version-checked L1**

Store entries behind `sync.RWMutex`. Read `ready` and `active` in one pipeline; on matching version clone the pointer slice and account values. On a version miss hydrate Redis once, publish only after full decode, and use `singleflight.Group` keyed by bucket+version to coalesce concurrent hydration. Evict on retirement/reopen and after successful `SetAccount`/`DeleteAccount` because account metadata can change without bucket version rotation.

- [x] **Step 4: Verify and commit**

Run: `cd backend && go test ./internal/repository -run 'TestSchedulerCache|TestSchedulerSnapshot' -count=1`

Commit: `perf: cache scheduler snapshots by active version`

### Task 3: Local Runtime State, Precise Utilization, and AIMD

**Files:**
- Create: `backend/internal/service/openai_adaptive_scheduler.go`
- Test: `backend/internal/service/openai_adaptive_scheduler_test.go`

**Interfaces:**
- Produces: `openAIAdaptiveRuntime.tryReserve(accountID, hardLimit, now) (*openAIAdaptivePermit, bool)`.
- Produces: idempotent `openAIAdaptivePermit.Release()` and `ReportSuccess(now, latency)` / `Report429(now, resetAt)` state transitions.
- Produces: exact `utilization = inflight/window` as `float64`, never integer percent.

- [x] **Step 1: Write failing state-machine tests**

Cover initial window 2, third reservation rejection, idempotent release, concurrent reservation never exceeding window, reciprocal-credit increase no faster than two seconds and only after 30 seconds without 429, first 429 halving with 2-second cooldown, repeated 429 cooldowns `5s/15s/30s`, half-open allowing one probe, successful probe restoring window 2, and hard cap 32.

- [x] **Step 2: Run tests and verify RED**

Run: `cd backend && go test ./internal/service -run 'TestOpenAIAdaptiveRuntime' -count=1`

Expected: compile failure because the runtime does not exist.

- [x] **Step 3: Implement the smallest sharded runtime**

Use 64 shards, each with a mutex and account map. Keep permit state changes under the account shard lock. Release uses `sync.Once` and broadcasts a non-blocking notification channel. Keep clock input explicit in internal methods so tests are deterministic.

- [x] **Step 4: Run tests, race test, and commit**

Run: `cd backend && go test ./internal/service -run 'TestOpenAIAdaptiveRuntime' -race -count=1`

Commit: `feat: add adaptive openai account windows`

### Task 4: Power-of-Four Selection and Redis Hot-Path Removal

**Files:**
- Modify: `backend/internal/service/openai_adaptive_scheduler.go`
- Modify: `backend/internal/service/openai_account_scheduler.go`
- Test: `backend/internal/service/openai_adaptive_scheduler_test.go`
- Test: `backend/internal/service/openai_account_scheduler_adaptive_test.go`

**Interfaces:**
- Produces: `selectAdaptiveCandidate(req, candidates, now, seed) (*Account, *openAIAdaptivePermit)` using `sample_size` and `sample_rounds`.
- Consumes: static filtered candidates from the existing eligibility pipeline and `ExcludedIDs`.

- [x] **Step 1: Write failing algorithm and integration tests**

Assert four unique samples per round, precise preference of `1/10000` over `2/10000`, random tie distribution across more than 80% of a 3000-account pool over a deterministic sequence, sticky escape at utilization `>=0.8`, exclusion of failed accounts, and at most one Redis acquire per successful selection. Assert adaptive selection never calls `GetAccountsLoadBatch` or `GetAccountsLoadBatchFresh`.

- [x] **Step 2: Run tests and verify RED**

Run: `cd backend && go test ./internal/service -run 'TestOpenAIAdaptiveSelect|TestOpenAIGatewayService_AdaptiveScheduler' -count=1`

Expected: compile failure for missing selector and legacy integration performs broad load reads.

- [x] **Step 3: Implement sampling and adaptive branch**

Reuse the existing account compatibility filter, but branch before building `loadReq`. Sample two rounds of four unique indexes, compare permit availability, utilization, inflight, EWMA error/latency, oldest selection, then request-random tie value. Reserve locally before `tryAcquireAccountSlot`; on Redis rejection or fresh-account veto release the permit and try another sampled candidate. Wrap Redis release and local release in one idempotent release function.

- [x] **Step 4: Verify and commit**

Run focused tests plus `cd backend && go test ./internal/service -run 'Test(OpenAI|EffectiveLoadFactor|Concurrency|Scheduler)' -count=1`.

Commit: `feat: select openai accounts from local adaptive samples`

### Task 5: Event-Driven One-Second Wait

**Files:**
- Modify: `backend/internal/service/openai_adaptive_scheduler.go`
- Modify: `backend/internal/service/openai_account_scheduler.go`
- Test: `backend/internal/service/openai_adaptive_scheduler_test.go`

**Interfaces:**
- Produces: bounded `waitAndSelect(ctx, deadline, routeKey, candidates)` driven by release notifications.
- Produces: global waiter admission bounded by `max_waiters`.

- [x] **Step 1: Write failing wait tests**

Assert a waiter wakes immediately after a permit release, returns by a 1-second absolute deadline when no capacity appears, respects caller cancellation, and rejects the 1001st waiter without creating another goroutine.

- [x] **Step 2: Run RED, implement, and verify GREEN**

Run: `cd backend && go test ./internal/service -run 'TestOpenAIAdaptiveWait' -count=1`.

Use a buffered generation notification channel per route shard and an atomic global waiter count. Waiting always re-runs bounded sampling after notification and never polls Redis.

- [x] **Step 3: Commit**

Commit: `feat: add bounded scheduler wait notifications`

### Task 6: Immediate 429 Feedback, Route Storm Ratio, and Half-Open Recovery

**Files:**
- Modify: `backend/internal/service/openai_account_runtime_block_fastpath.go`
- Modify: `backend/internal/service/openai_account_scheduler.go`
- Modify: `backend/internal/service/openai_adaptive_scheduler.go`
- Test: `backend/internal/service/openai_account_runtime_block_fastpath_test.go`
- Test: `backend/internal/service/openai_adaptive_scheduler_test.go`

**Interfaces:**
- Produces: `OpenAIAccountScheduler.ReportRateLimit(accountID, routeKey string, resetAt *time.Time)`.
- Produces: five-second route ring with attempt and 429 counters, 20-attempt/20% entry, ten-second below-5% recovery, and 0.75 storm window contraction.

- [x] **Step 1: Write failing 429 and storm tests**

Assert local account blocking and window reduction happen before a deliberately blocked persistence path completes; `codex_7d_used_percent=100` does not create a runtime block; one 429 cannot trigger a storm; 4/20 enters storm; storm caps switching at one; two clean five-second windows recover; reset headers control half-open time.

- [x] **Step 2: Run RED and implement local-first feedback**

Run: `cd backend && go test ./internal/service -run 'Test(OpenAIAdaptiveStorm|MarkOpenAIOAuth429|Codex7d)' -count=1`.

Call adaptive `ReportRateLimit` immediately after parsing the reset and before any Redis/PostgreSQL persistence. Record all completed attempts from `ReportResult` so the denominator is real attempts, not only errors.

- [x] **Step 3: Verify and commit**

Run focused tests with `-race` and commit: `feat: stop openai 429 storms locally`.

### Task 7: Request Failover Deadline and Distinct-Account Budget

**Files:**
- Modify: `backend/internal/handler/failover_loop.go`
- Test: `backend/internal/handler/failover_loop_test.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/handler/openai_images.go`
- Modify: `backend/internal/handler/openai_embeddings.go`
- Modify: `backend/internal/handler/openai_alpha_search.go`
- Test: `backend/internal/handler/openai_gateway_credential_failover_loop_test.go`

**Interfaces:**
- Produces: `FailoverBudget` with absolute `deadline`, distinct account set, and `CanTry(accountID, now)`.
- Consumes: adaptive scheduler config `800ms` and `2` accounts.

- [x] **Step 1: Write failing budget tests**

Assert repeated same-account retries do not add a distinct account, a third distinct account is rejected, and scheduling after 800 milliseconds is rejected even when legacy switch count is larger. Customer-caused non-retryable 4xx must consume no second account.

- [x] **Step 2: Run RED, implement shared budget, and wire OpenAI loops**

Run: `cd backend && go test ./internal/handler -run 'TestOpenAIFailoverBudget|TestOpenAIFailoverDistinctAccounts' -count=1`.

Create one budget at request start and reuse it across every OpenAI selection loop. Do not include elapsed upstream request time in the 800-millisecond failover scheduling deadline; arm it on the first eligible account-switch error.

- [x] **Step 3: Verify and commit**

Run OpenAI handler failover tests and commit: `feat: bound openai failover scheduling`.

### Task 8: Expired Slot Cleanup and End-to-End Verification

**Files:**
- Modify: `backend/internal/repository/concurrency_cache.go`
- Modify: `backend/internal/service/concurrency_slot_cleanup.go`
- Test: `backend/internal/service/concurrency_slot_cleanup_test.go`
- Test: `backend/internal/repository/concurrency_cache_integration_test.go`

**Interfaces:**
- Produces: active-account index score equal to the earliest lease expiry; cleanup removes every expired member in one cycle even when newer leases exist.

- [x] **Step 1: Write a failing mixed-age slot test**

Create one expired and one live slot for the same account, move the active index as a newer acquire would, run one cleanup cycle, and assert the expired slot is gone while the live slot and active index remain.

- [x] **Step 2: Run RED, fix index maintenance, and verify GREEN**

Run: `cd backend && go test ./internal/service ./internal/repository -run 'TestConcurrencySlotCleanup|Test.*ExpiredAccountSlot' -count=1`.

Make acquire/release/cleanup atomically maintain the earliest member expiry instead of the latest activity time.

- [x] **Step 3: Run final verification**

Run:

```bash
cd backend
gofmt -w internal/config/config.go internal/handler internal/repository/scheduler_cache.go internal/repository/concurrency_cache.go internal/service/openai_account_scheduler.go internal/service/openai_adaptive_scheduler.go internal/service/openai_account_runtime_block_fastpath.go
go test ./internal/config ./internal/repository ./internal/service ./internal/handler -count=1
go test -race ./internal/repository ./internal/service ./internal/handler -run 'Test(OpenAIAdaptive|OpenAIAccount|SchedulerCacheSnapshotL1|ConcurrencySlotCleanup|OpenAIFailover)' -count=1
go test ./... -count=1
go vet ./internal/config ./internal/repository ./internal/service ./internal/handler
```

- [x] **Step 4: Review requirements and commit**

Confirm with tests or code references: no broad Redis load read in adaptive hot path, one final Redis acquire/release, 1-second waiter deadline and cap, two distinct accounts, local-first 429, half-open single probe, 7-day usage observation only, and legacy flag rollback.

Commit: `fix: clean expired concurrency leases deterministically`
