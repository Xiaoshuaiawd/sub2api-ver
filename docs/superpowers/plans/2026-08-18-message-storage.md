# Message Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add high-concurrency admin access to the original client request body and final client response body for usage logs, using PostgreSQL-only storage with bounded capture, asynchronous persistence, and seven-day configurable retention.

**Architecture:** A gateway middleware creates a capture session that tees the original request stream and final `gin.ResponseWriter`. Small payloads stay within a global memory budget; larger payloads spill to per-request files. The terminal usage transaction creates a small pending capture row, then a dedicated bounded message worker compresses artifacts with Zstd and writes them to daily-partitioned PostgreSQL body tables. Admin list responses contain only status metadata; a detail endpoint loads bodies on demand.

**Tech Stack:** Go 1.26, Gin, Ent-backed `database/sql`, PostgreSQL migrations, Zstd (`klauspost/compress/zstd`), Vue 3 + TypeScript, existing admin audit log and dialog components.

## Global Constraints

- Do not modify or reset unrelated existing worktree changes.
- Do not add request/response bodies to `usage_logs` or the existing billing worker queue.
- Storage failure, queue overflow, spool exhaustion, compression failure, and body-size overflow must not change the user-facing API response or billing result.
- Capture the client request before model/protocol normalization and capture bytes finally written to the client.
- Default retention is 7 days; accepted values are 1-30 days.
- Default per-direction body limit is 16 MiB; global in-memory capture budget is 512 MiB.
- All list/detail endpoints must use parameterized SQL and must never select body payloads for list pages.

---

### Task 1: Add PostgreSQL schema, migration, and configuration

**Files:**
- Create: `backend/migrations/226_usage_message_storage.sql`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Test: `backend/migrations/usage_message_storage_migration_test.go`

**Interfaces:**
- Produces `config.GatewayMessageStorageConfig` with `Enabled`, `RetentionDays`, `MaxBodyBytes`, `MemoryBudgetBytes`, `SpoolDirectory`, `SpoolBudgetBytes`, `WorkerCount`, `QueueSize`, and `DBMaxOpenConns`.
- Produces PostgreSQL tables `usage_message_captures` and partitioned `usage_message_bodies` with statuses `pending`, `available`, `failed`, `partial`, `too_large`, `expired`, and `disabled`.

- [ ] **Step 1: Write migration contract tests**

Add tests that read migration `226_usage_message_storage.sql` and assert it contains:

```go
require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_message_captures")
require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_message_bodies")
require.Contains(t, sql, "PARTITION BY RANGE (created_at)")
require.Contains(t, sql, "payload_zstd BYTEA NOT NULL")
require.Contains(t, sql, "CHECK (body_type IN ('request', 'response'))")
require.Contains(t, sql, "expires_at")
```

- [ ] **Step 2: Run the migration test and verify it fails**

Run `cd backend && go test ./migrations -run TestUsageMessageStorageMigration -count=1`.
Expected: FAIL because the migration file does not exist.

- [ ] **Step 3: Add the migration**

Create the metadata table with a unique `usage_log_id`, `request_id`, status fields, byte counters, SHA-256 strings, `compression`, `error_code`, `error_message`, `expires_at`, and timestamps. Create `usage_message_bodies` partitioned by `created_at`, with a composite primary key `(created_at, usage_log_id, body_type)`, a foreign key to `usage_logs(id)`, and indexes on `(usage_log_id, created_at)`. Create a default partition for startup safety; the cleanup service creates the current and next-day partitions before writes and moves old data out of the default partition during maintenance. Set `payload_zstd` storage to `EXTERNAL`.

- [ ] **Step 4: Add config fields and validation**

Add `Gateway.MessageStorage` and defaults:

```go
Enabled:            true
RetentionDays:      7
MaxBodyBytes:       16 * 1024 * 1024
MemoryBudgetBytes:  512 * 1024 * 1024
SpoolBudgetBytes:   20 * 1024 * 1024 * 1024
WorkerCount:        32
QueueSize:          4096
DBMaxOpenConns:     16
```

Reject retention outside 1-30, non-positive body/memory/spool limits, worker count outside 1-128, queue size below worker count, and DB max connections outside 1-64. An empty spool directory must resolve under the OS temp directory with a `sub2api-message-storage` child.

- [ ] **Step 5: Run config and migration tests**

Run `cd backend && go test ./internal/config ./migrations -count=1`.
Expected: PASS.

- [ ] **Step 6: Commit**

Run `git add backend/migrations/226_usage_message_storage.sql backend/migrations/usage_message_storage_migration_test.go backend/internal/config/config.go backend/internal/config/config_test.go backend/internal/handler/dto/settings.go backend/internal/handler/admin/setting_handler.go` followed by `git commit -m "feat: add message storage schema and config"`.

### Task 2: Implement bounded request/response capture and artifact lifecycle

**Files:**
- Create: `backend/internal/service/message_capture.go`
- Create: `backend/internal/service/message_capture_test.go`
- Create: `backend/internal/server/middleware/message_capture.go`
- Create: `backend/internal/server/middleware/message_capture_test.go`

**Interfaces:**
- `service.MessageCaptureSession` owns request and response capture and exposes `Artifact() MessageCaptureArtifact`.
- `service.MessageCaptureArtifact` contains request/response `BodyArtifact`, status, byte counts, SHA-256, and cleanup function.
- `middleware.MessageCapture(cfg, factory)` returns a Gin middleware that stores the session in request context under a private key.
- `service.MessageCaptureFromContext(ctx)` returns the session for `UsageService.Create`.

- [ ] **Step 1: Write capture unit tests**

Cover: in-memory capture under the budget, spill after memory threshold, exact SHA-256 and byte count, per-direction limit producing `too_large`, writer errors producing `failed`, response writer preserving `http.Flusher`/`http.CloseNotifier` behavior where available, and cleanup deleting spill files.

- [ ] **Step 2: Run capture tests and verify failure**

Run `cd backend && go test ./internal/service ./internal/server/middleware -run 'MessageCapture|Message.*Capture' -count=1`.
Expected: FAIL because the capture types and middleware do not exist.

- [ ] **Step 3: Implement bounded artifact writer**

Use a mutex-protected writer with a global atomic byte budget. Keep bytes in memory until a configurable spill threshold, then create a file with `os.OpenFile` using `O_CREATE|O_EXCL`, stream future bytes to it, and release memory permits. Never allocate a second full copy of a body. Once the body limit is exceeded, consume no further capture bytes, record `too_large`, and allow the underlying request/response stream to continue.

- [ ] **Step 4: Implement request tee and response writer wrapper**

Wrap `c.Request.Body` with an `io.ReadCloser` that writes exactly bytes read by the handler into the request artifact. Wrap `gin.ResponseWriter.Write` and `WriteString` so capture happens after all transformations and before bytes reach the client. On handler return, finalize the session; if the request context was canceled after a response started, mark the response `partial`.

- [ ] **Step 5: Run capture tests**

Run `cd backend && go test ./internal/service ./internal/server/middleware -run 'MessageCapture|Message.*Capture' -count=1`.
Expected: PASS.

- [ ] **Step 6: Commit**

Run `git add backend/internal/service/message_capture.go backend/internal/service/message_capture_test.go backend/internal/server/middleware/message_capture.go backend/internal/server/middleware/message_capture_test.go` followed by `git commit -m "feat: add bounded client message capture"`.

### Task 3: Add repository and asynchronous PostgreSQL worker

**Files:**
- Create: `backend/internal/repository/usage_message_repo.go`
- Create: `backend/internal/repository/usage_message_repo_test.go`
- Create: `backend/internal/service/message_storage.go`
- Create: `backend/internal/service/message_storage_worker.go`
- Create: `backend/internal/service/message_storage_test.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/internal/service/wire.go`

**Interfaces:**
- `service.MessageStorageRepository` exposes `CreatePending`, `StoreBody`, `UpdateState`, `GetDetail`, `MarkStalePendingFailed`, and `ExpireBefore`.
- `service.MessageStorageService.Enqueue(ctx, usageLogID, requestID, artifact)` returns a non-blocking `MessageEnqueueResult`.
- `service.MessageStorageService.Start()` and `Stop(ctx)` own worker goroutines and cleanup.
- `repository.NewUsageMessageRepository(*sql.DB) service.MessageStorageRepository` uses the existing SQL pool and never loads body columns for list queries.

- [ ] **Step 1: Write repository tests**

Use `sqlmock` to assert parameterized `INSERT` for pending metadata, `INSERT ... ON CONFLICT` for each body, state updates, detail queries that return metadata plus body bytes only for a requested `usage_log_id`, stale-pending update, and expiry deletion. Assert that no query used by `GetSummary` contains `payload_zstd`.

- [ ] **Step 2: Run repository tests and verify failure**

Run `cd backend && go test ./internal/repository -run 'UsageMessage' -count=1`.
Expected: FAIL because the repository does not exist.

- [ ] **Step 3: Implement repository SQL**

Use a small set of constants and `database/sql`. Insert pending metadata in the usage transaction when called with an `ent.Tx`-compatible executor; use the regular `*sql.DB` for worker writes. For body writes, bind compressed bytes, raw/stored sizes, partition timestamp, and body type. Always update the metadata row only after both requested body writes succeed.

- [ ] **Step 4: Implement Zstd worker**

Build a separate `pond` pool with configured queue size and worker count. Queue items contain artifact file paths or small byte slices, never a copy of a large body. Each task uses a bounded context timeout, streams the artifact into a Zstd encoder, performs one body insert at a time, updates request/response state independently, and removes temporary files in a `defer`. Queue-full returns `failed/queue_full` to the caller without waiting.

- [ ] **Step 5: Add metrics and stale-pending cleanup**

Expose atomic counters for submitted, completed, failed, too-large, queue-full, pending age, bytes captured, bytes spooled, compression duration, and database duration. Add a periodic cleanup loop that marks stale pending rows and calls `ExpireBefore` according to configured retention.

- [ ] **Step 6: Run repository and service tests**

Run `cd backend && go test ./internal/repository ./internal/service -run 'UsageMessage|MessageStorage' -count=1`.
Expected: PASS.

- [ ] **Step 7: Commit**

Run `git add backend/internal/repository/usage_message_repo.go backend/internal/repository/usage_message_repo_test.go backend/internal/service/message_storage.go backend/internal/service/message_storage_worker.go backend/internal/service/message_storage_test.go backend/internal/repository/wire.go backend/internal/service/wire.go` followed by `git commit -m "feat: add asynchronous postgres message storage"`.

### Task 4: Connect capture to usage lifecycle and gateway routes

**Files:**
- Modify: `backend/internal/service/usage_service.go`
- Modify: `backend/internal/service/usage_service_test.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/gemini_v1beta_handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/server/routes/wire.go`
- Modify: `backend/cmd/server/wire.go`
- Regenerate: `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/server/routes/message_capture_route_test.go`

**Interfaces:**
- `UsageService.SetMessageStorage(*service.MessageStorageService)` adds an optional dependency without breaking existing unit-test constructors.
- `UsageService.Create` reads a capture session from context after the usage row is inserted and creates the pending metadata row plus enqueue artifact job; it never places body bytes in the usage record task closure.
- Gateway route groups install `middleware.MessageCapture` after body-size limiting and before handler dispatch for `/v1`, `/v1beta`, aliases, and `/antigravity` request paths that produce usage logs.

- [ ] **Step 1: Write lifecycle tests**

Add a fake `MessageStorageService` to `UsageService` tests. Assert `Create` creates a pending capture for a successful usage row, keeps the original request bytes after handler normalization, passes final response bytes, and still returns the usage log when enqueue returns `queue_full`.

- [ ] **Step 2: Run lifecycle tests and verify failure**

Run `cd backend && go test ./internal/service ./internal/server/routes -run 'MessageCapture|MessageStorage' -count=1`.
Expected: FAIL because `UsageService` has no message-storage hook and routes have no middleware.

- [ ] **Step 3: Add usage lifecycle hook**

After `usageRepo.Create` returns the inserted usage log, call the message service with the context capture and inserted ID. Do not call it before the billing transaction commits. If the message service is nil or capture disabled, preserve existing behavior exactly.

- [ ] **Step 4: Add middleware to every gateway entry point**

Install the middleware on the shared `/v1` group and the separate `/v1beta`, root aliases, `/antigravity/v1`, and `/antigravity/v1beta` groups. Keep `RequestBodyLimit` before capture so rejected oversized HTTP requests are not spooled. Apply it to POST endpoints that create usage logs, including image/audio endpoints, and exclude models, usage, status, task polling, and other GET endpoints that do not create a usage log.

- [ ] **Step 5: Wire constructors and shutdown**

Add repository/service providers, pass the message storage service to `UsageService.SetMessageStorage`, start it with the application, and stop it before closing Ent/Redis. Regenerate with `cd backend && go generate ./cmd/server`.

- [ ] **Step 6: Run gateway and lifecycle tests**

Run `cd backend && go test ./internal/service ./internal/server/routes ./internal/handler -run 'MessageCapture|MessageStorage|UsageRecord' -count=1`.
Expected: PASS.

- [ ] **Step 7: Commit**

Run `git add backend/internal/service/usage_service.go backend/internal/service/usage_service_test.go backend/internal/server/routes/gateway.go backend/internal/handler/gateway_handler.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/gemini_v1beta_handler.go backend/internal/handler/wire.go backend/internal/server/routes/wire.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go backend/internal/server/routes/message_capture_route_test.go` followed by `git commit -m "feat: connect message capture to gateway usage logs"`.

### Task 5: Add admin detail API and DTOs

**Files:**
- Modify: `backend/internal/handler/admin/usage_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/repository/usage_log_repo_query.go`
- Modify: `backend/internal/repository/usage_log_repo.go`
- Test: `backend/internal/handler/admin/usage_message_handler_test.go`
- Test: `backend/internal/repository/usage_message_detail_test.go`

**Interfaces:**
- `GET /api/v1/admin/usage/:id/message` returns metadata and, when `include_bodies=true`, request/response bodies.
- `AdminUsageLog` gains `message_storage_status`, `request_body_state`, `response_body_state`, `request_body_bytes`, and `response_body_bytes` without body payloads.
- `MessageStorageService.GetDetail(ctx, usageLogID, includeBodies)` returns a bounded response DTO.

- [ ] **Step 1: Write handler tests**

Cover admin authorization, not-found, `include_bodies=false`, one-side failure, expired state, and body payload retrieval. Assert responses never return a body for the list endpoint.

- [ ] **Step 2: Run handler tests and verify failure**

Run `cd backend && go test ./internal/handler/admin ./internal/repository -run 'UsageMessage|MessageDetail' -count=1`.
Expected: FAIL because the route and DTOs do not exist.

- [ ] **Step 3: Implement detail endpoint**

Add a route beside existing admin usage routes. Require the same admin middleware as `GET /admin/usage`; call the message service with a server-side decompression limit. Return status and per-body error fields even when one body is unavailable. Record an audit event through the existing audit service when an admin views bodies.

- [ ] **Step 4: Add list status projection**

Left join only `usage_message_captures` metadata in the admin usage list query. Do not select `usage_message_bodies.payload_zstd`. Preserve pagination and count query plans.

- [ ] **Step 5: Run admin API tests**

Run `cd backend && go test ./internal/handler/admin ./internal/repository -run 'UsageMessage|MessageDetail' -count=1`.
Expected: PASS.

- [ ] **Step 6: Commit**

Run `git add backend/internal/handler/admin/usage_handler.go backend/internal/handler/dto/types.go backend/internal/handler/dto/mappers.go backend/internal/server/routes/admin.go backend/internal/repository/usage_log_repo_query.go backend/internal/repository/usage_log_repo.go backend/internal/handler/admin/usage_message_handler_test.go backend/internal/repository/usage_message_detail_test.go` followed by `git commit -m "feat: expose admin usage message details"`.

### Task 6: Build the admin usage detail UI

**Files:**
- Modify: `frontend/src/api/admin/usage.ts`
- Modify: `frontend/src/types/index.ts`
- Create: `frontend/src/components/admin/usage/UsageMessageDetailDialog.vue`
- Modify: `frontend/src/components/admin/usage/UsageTable.vue`
- Modify: `frontend/src/views/admin/UsageView.vue`
- Modify: `frontend/src/locales/zh-CN.ts`
- Modify: `frontend/src/locales/en-US.ts`
- Test: `frontend/src/components/admin/usage/__tests__/UsageMessageDetailDialog.spec.ts`
- Test: `frontend/src/views/admin/__tests__/UsageView.message-storage.spec.ts`

**Interfaces:**
- `adminUsageAPI.getMessageDetail(id, includeBodies)` calls `/admin/usage/:id/message`.
- `UsageMessageDetailDialog` accepts `show` and `usageLogId`, fetches on open, and emits `update:show`.

- [ ] **Step 1: Write component tests**

Mock the API and cover loading, request/response tabs, JSON formatting fallback to raw text, copy action, failed/partial/too-large/expired states, and close/reset behavior. Cover clicking a usage table row opens the dialog without changing existing user-click balance behavior.

- [ ] **Step 2: Run frontend tests and verify failure**

Run `cd frontend && pnpm test:run -- src/components/admin/usage/__tests__/UsageMessageDetailDialog.spec.ts src/views/admin/__tests__/UsageView.message-storage.spec.ts`.
Expected: FAIL because the API method and component do not exist.

- [ ] **Step 3: Implement API types and dialog**

Use existing `BaseDialog`, `Icon`, `LoadingSpinner`, `Toast`, and i18n patterns. Keep the body area in a fixed-height scroll container, show byte sizes with tabular numerals, use accessible labels on icon-only copy/download controls, and avoid loading body content in the list.

- [ ] **Step 4: Add table affordance and view state**

Add a message status column and a row action with a familiar document/search icon. Keep the existing user email click separate. Open the dialog from an explicit action so accidental row clicks do not expose sensitive content.

- [ ] **Step 5: Run frontend tests and typecheck**

Run `cd frontend && pnpm test:run -- src/components/admin/usage/__tests__/UsageMessageDetailDialog.spec.ts src/views/admin/__tests__/UsageView.message-storage.spec.ts` and `cd frontend && pnpm typecheck`.
Expected: PASS.

- [ ] **Step 6: Commit**

Run `git add frontend/src/api/admin/usage.ts frontend/src/types/index.ts frontend/src/components/admin/usage/UsageMessageDetailDialog.vue frontend/src/components/admin/usage/UsageTable.vue frontend/src/views/admin/UsageView.vue frontend/src/locales/zh-CN.ts frontend/src/locales/en-US.ts frontend/src/components/admin/usage/__tests__/UsageMessageDetailDialog.spec.ts frontend/src/views/admin/__tests__/UsageView.message-storage.spec.ts` followed by `git commit -m "feat: add admin usage message viewer"`.

### Task 7: Add retention settings, cleanup tests, and operational safeguards

**Files:**
- Create: `backend/internal/service/message_storage_cleanup.go`
- Create: `backend/internal/service/message_storage_cleanup_test.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire.go`
- Modify: `backend/internal/server/api_contract_test.go`
- Modify: `docs/DEV_GUIDE.md`
- Modify: `README_CN.md`

**Interfaces:**
- `MessageStorageCleanupService.RunOnce(ctx, now)` removes expired partitions/rows and stale pending rows.
- `GET/PUT /api/v1/admin/usage/message-storage/settings` reads/writes retention and capture limits through the existing setting repository, validating retention 1-30.

- [ ] **Step 1: Write cleanup and settings tests**

Cover retention validation, setting persistence, stale pending conversion, partition cleanup SQL, and cleanup failure isolation. Assert cleanup never issues `DELETE FROM usage_logs`.

- [ ] **Step 2: Run tests and verify failure**

Run `cd backend && go test ./internal/service ./internal/server -run 'MessageStorage.*Cleanup|MessageStorage.*Setting' -count=1`.
Expected: FAIL because cleanup/settings do not exist.

- [ ] **Step 3: Implement cleanup and settings**

Use the existing periodic service lifecycle and setting repository. The cleanup job takes an advisory lock so multiple application instances do not concurrently drop the same partition. The settings endpoints are the canonical admin configuration surface; the frontend can add a compact retention/limit panel in the existing usage filters toolbar without a new navigation section.

- [ ] **Step 4: Add operational documentation**

Document PostgreSQL disk sizing, WAL impact, spool directory sizing, queue metrics, default limits, failure semantics, and a runbook for lowering retention or disabling capture during database pressure.

- [ ] **Step 5: Run cleanup tests**

Run `cd backend && go test ./internal/service ./internal/server -run 'MessageStorage.*Cleanup|MessageStorage.*Setting' -count=1`.
Expected: PASS.

- [ ] **Step 6: Commit**

Run `git add backend/internal/service/message_storage_cleanup.go backend/internal/service/message_storage_cleanup_test.go backend/internal/service/wire.go backend/cmd/server/wire.go backend/internal/server/api_contract_test.go docs/DEV_GUIDE.md README_CN.md` followed by `git commit -m "feat: add message storage retention cleanup"`.

### Task 8: Full verification and high-concurrency validation

**Files:**
- Create: `backend/internal/integration/message_storage_gateway_integration_test.go`
- Create: `backend/internal/service/message_storage_benchmark_test.go`
- Modify: `backend/internal/handler/admin/usage_handler_request_type_test.go` only if shared fixtures require new status fields

- [ ] **Step 1: Add integration coverage**

Use the existing test database helpers to send a sync request and a streamed request. Assert original request bytes are stored despite model normalization, final client bytes are stored after response transformation, and the admin detail endpoint returns both bodies.

- [ ] **Step 2: Add failure-path coverage**

Use a fake repository that returns queue-full, spool-full, compression, and database errors. Assert the gateway response and usage/billing result are unchanged while the capture state is `failed` or `too_large`.

- [ ] **Step 3: Add benchmark**

Benchmark capture with 1 KB, 8 KB, and 64 KB request/response pairs, recording allocations and P99 enqueue latency. Add a bounded worker saturation benchmark that proves queue-full returns without waiting for a worker.

- [ ] **Step 4: Run targeted verification**

Run:

```bash
cd backend && go test ./internal/service ./internal/repository ./internal/handler/admin ./internal/server/routes ./internal/integration -count=1
cd frontend && pnpm test:run
cd frontend && pnpm typecheck
```

Expected: PASS.

- [ ] **Step 5: Run broader checks**

Run `cd backend && go test ./... -count=1` and `cd frontend && pnpm build`.
Expected: PASS; report any pre-existing unrelated failure separately.

- [ ] **Step 6: Review worktree and summarize**

Run `git status --short`, `git log --oneline -8`, and inspect the final migration order. Confirm only message-storage commits are new and existing user modifications remain untouched.
