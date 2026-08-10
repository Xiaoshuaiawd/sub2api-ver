# Proxy Group Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let accounts choose a proxy group and randomly route each new upstream request through an eligible group member.

**Architecture:** Persist proxy groups and proxy membership in Ent-managed tables, add nullable account selection metadata, and resolve one eligible proxy before the gateway retry loop. Extend the existing Vue account editor with a three-mode proxy control while preserving fixed proxy/direct behavior.

**Tech Stack:** Go, Ent, PostgreSQL migrations, Gin service layer, Vue 3 + TypeScript, Vitest, Go test.

## Global Constraints

- Existing accounts without `proxy_group_id` must behave exactly as before.
- A request selects one proxy at most once; retries reuse that proxy.
- Only active and non-expired proxies are eligible.
- No new runtime dependencies.
- Tests must be written before production code for each behavior.

### Task 1: Add proxy-group domain and persistence

**Files:**
- Create: `backend/ent/schema/proxygroup.go`
- Create: `backend/ent/schema/proxygroup_proxy.go`
- Modify: `backend/ent/schema/account.go`
- Modify: `backend/internal/service/proxy_group.go`
- Modify: `backend/internal/service/account.go`
- Modify: `backend/internal/repository/account_repo.go`
- Modify: `backend/internal/repository/proxy_repo.go`
- Create: `backend/migrations/<next>_proxy_groups.sql`
- Test: `backend/internal/service/proxy_group_test.go`

- [ ] Write failing tests for eligible member filtering and group CRUD/update semantics.
- [ ] Run `go test ./internal/service ./internal/repository` and verify the new tests fail for missing APIs.
- [ ] Add Ent schemas, nullable account field, migration, service interfaces, repository queries, and mappers.
- [ ] Run focused Go tests and `go generate ./ent` if required by repository conventions.

### Task 2: Resolve group proxy in gateway

**Files:**
- Modify: `backend/internal/service/gateway_forward.go`
- Create: `backend/internal/service/proxy_group_selector.go`
- Test: `backend/internal/service/proxy_group_selector_test.go`
- Test: `backend/internal/service/gateway_forward_proxy_group_test.go`

- [ ] Write failing selector and gateway retry-reuse tests.
- [ ] Verify RED with focused `go test` commands.
- [ ] Implement injected random selection and one-time resolution before retries.
- [ ] Preserve fixed proxy and direct paths, then run all gateway service tests.

### Task 3: Expose admin APIs and account editor controls

**Files:**
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Create/modify: `backend/internal/handler/admin/proxy_group_handler.go`
- Modify: `frontend/src/api/admin/accounts.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/components/admin/account/EditAccountModal.vue`
- Modify: `frontend/src/views/admin/AccountsView.vue`
- Modify: `frontend/src/i18n/locales/en.json`
- Modify: `frontend/src/i18n/locales/zh-CN.json`
- Test: `frontend/src/components/admin/account/__tests__/EditAccountModal.proxy-group.spec.ts`

- [ ] Add failing tests for three-mode payloads and group display.
- [ ] Verify RED with the focused Vitest test.
- [ ] Implement list/create/update group API and account form wiring.
- [ ] Run focused frontend tests and type-check.

### Task 4: Regression verification

- [ ] Run `go test ./internal/service ./internal/repository ./internal/handler/...`.
- [ ] Run frontend lint, type-check, and relevant Vitest suites.
- [ ] Review migration and API compatibility, then report any environment-limited checks.
