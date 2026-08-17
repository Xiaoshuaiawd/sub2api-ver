# Sub2API Ops Hermes Skill Design

**Status:** Draft for user review

## Goal

Add a repository-shipped Hermes Agent Skill named `sub2api-ops` for human-confirmed Sub2API operations. The skill turns the existing admin monitoring surface into a repeatable workflow:

1. collect a bounded diagnostic snapshot;
2. correlate dashboard, alert, error, account, and system-log evidence;
3. explain the likely cause and impact;
4. create a concrete mutation plan;
5. require an explicit human confirmation token;
6. execute only the allow-listed mutation;
7. read the affected resource again and report the result.

The first release is a Hermes Skill and CLI integration. It does not add a new backend agent API, autonomous remediation loop, or frontend chat page.

## Repository Layout

```text
skills/devops/sub2api-ops/
  SKILL.md
  references/ops-api.md
  scripts/sub2api-ops.js
  tests/sub2api-ops.test.js
```

`SKILL.md` uses Hermes frontmatter (`name`, `description`, `version`, `author`, `license`, and `metadata.hermes`) and contains the always-needed workflow and safety rules. Endpoint details and payload examples live in `references/ops-api.md`. The Node script is the deterministic transport and confirmation gate; it uses the same `SUB2API_BASE_URL`, `SUB2API_ADMIN_API_KEY`, and `SUB2API_JWT` environment variables as the existing admin CLI.

The existing `skills/sub2api-admin` Skill and CLI remain unchanged. The Ops Skill has its own command surface so account administration and production diagnosis do not share an implicit trigger or an unrestricted raw API fallback.

## User Workflow

### Read-only diagnosis

For an alert, elevated error rate, unavailable account pool, or suspected upstream failure, Hermes runs the smallest useful set of read-only commands:

- `snapshot` for the dashboard overview, throughput, and error trend;
- `availability` and `concurrency` for capacity symptoms;
- `alerts` and `alert-event <id>` for alert context;
- `errors`, `error <id>`, `upstream-errors`, and `requests` for request-level evidence;
- `system-logs` and `system-log-health` for runtime and ingestion evidence;
- `ingress-rejections` and `auth-cache-health` for admission and cache health.

The response is summarized without credentials, bearer tokens, request bodies, or full upstream error bodies. The diagnosis must identify the time window, filters, evidence IDs, confidence, and unresolved uncertainty.

### Human-confirmed mutation

Hermes must not call a write command immediately after diagnosis. It first runs `plan` for one allow-listed action. The command returns a JSON plan containing:

- action name and HTTP method/path;
- target IDs and a safe target label when available;
- before-state snapshot or expected precondition;
- exact change;
- risk and blast radius;
- verification command;
- one-time confirmation token and expiry.

Hermes presents this plan and waits for an explicit confirmation from the administrator. Only then may it run `execute --plan <plan-file> --confirm <token>`. A stale, altered, or already-used plan is rejected. The `execute` command performs the verification request itself and returns both the write response and the observed post-state for Hermes to report.

## Read-only Command Contract

The CLI accepts a default one-hour window and supports `--time-range`, `--start-time`, `--end-time`, `--platform`, and `--group-id` where the backend endpoint supports them. All output is JSON, with secrets and large opaque bodies redacted or omitted.

```text
snapshot
availability
concurrency
alerts [--status firing|resolved] [--severity P0|P1|P2|P3]
alert-event <id>
errors [filters]
error <id>
upstream-errors [filters]
requests [filters]
system-logs [filters]
system-log-health
ingress-rejections [filters]
auth-cache-health
```

The script bounds list sizes to the backend's safe limits and rejects invalid IDs, time ranges, and enum values before making a request.

## Allow-listed Mutations

Only these actions are available through `plan` and `execute`:

| Action | Backend operation | Preconditions | Verification |
| --- | --- | --- | --- |
| `resolve-error` | `PUT /admin/ops/errors/:id/resolve` with `{"resolved":true}` | Error exists and is unresolved | Read the error and require `resolved=true` |
| `resolve-request-error` | `PUT /admin/ops/request-errors/:id/resolve` with `{"resolved":true}` | Request error exists and is unresolved | Read the request error and require `resolved=true` |
| `resolve-upstream-error` | `PUT /admin/ops/upstream-errors/:id/resolve` with `{"resolved":true}` | Upstream error exists and is unresolved | Read the upstream error and require `resolved=true` |
| `resolve-alert-event` | `PUT /admin/ops/alert-events/:id/status` with `{"status":"manual_resolved"}` | Alert event is firing | Read the event and require a resolved status |
| `silence-alert` | `POST /admin/ops/alert-silences` | Rule ID, RFC3339 expiry, and non-empty reason are present | Require a created-silence response and re-read the parent alert rule; report that the backend has no silence-read endpoint |
| `clear-account-error` | `POST /admin/accounts/:id/clear-error` | Account is read first and target ID is explicit | Read account and require the error state to be cleared |
| `clear-account-rate-limit` | `POST /admin/accounts/:id/clear-rate-limit` | Account is read first and target ID is explicit | Read account and require rate-limit state to be cleared |
| `recover-account-state` | `POST /admin/accounts/:id/recover-state` | Account is read first and target ID is explicit | Read account and report the resulting state |

The first release does not expose account deletion, account export, credential import, bulk account update, redeem-code operations, system-log cleanup, alert-rule deletion, runtime-setting changes, database backup/restore, or raw API passthrough.

## Confirmation Gate

`plan` writes a short-lived plan file under `${XDG_STATE_HOME:-~/.local/state}/sub2api-ops/plans/` with mode `0600`. The file contains the canonical action, target, request body, precondition snapshot, verification request, a random token digest, creation time, and expiry (10 minutes by default). It never stores access tokens or credential payloads.

`execute` requires both the plan path and the exact token returned by `plan`. It verifies:

1. the plan is younger than its TTL and has not been consumed;
2. the supplied token matches the stored digest;
3. the action is still allow-listed;
4. the current target state still satisfies the precondition;
5. the request body matches the canonical plan;
6. the plan is marked consumed even when the write fails, preventing replay.

The CLI returns exit code `2` for missing or invalid confirmation, `3` for a stale or changed precondition, and `1` for transport/API failures. These distinct codes let Hermes explain whether the administrator must reconfirm, whether the incident changed, or whether the service failed.

## Error Handling and Redaction

- Authentication is resolved in this order: `SUB2API_ADMIN_API_KEY`, then `SUB2API_JWT`.
- `INVALID_ADMIN_KEY` and HTTP 401/403 are reported as an authentication prerequisite, never retried with guessed credentials.
- 4xx validation failures include the backend message but omit request bodies that could contain secrets.
- 5xx, timeout, and network errors include method/path, status, and a bounded message; they do not print headers or tokens.
- JSON output redacts keys matching `token`, `secret`, `password`, `credential`, `authorization`, `cookie`, `api_key`, and `refresh_token`, and truncates opaque log/error bodies to a small diagnostic excerpt.
- Plans and verification output include IDs and safe labels only. Credential-bearing export/import endpoints are unreachable from this CLI.

## Testing and Acceptance

The Node script is tested without a live Sub2API instance by injecting a mock `fetch` implementation. Required coverage:

- frontmatter and file-size validation for `SKILL.md`;
- read-only query construction and default time range;
- response envelope parsing and non-zero backend error handling;
- redaction of sensitive JSON fields and truncation of large bodies;
- plan token generation, expiry, one-time consumption, and tamper detection;
- every allow-listed action maps to the expected method/path/body;
- invalid IDs, time ranges, action names, and confirmation tokens fail before a write request;
- successful execution is followed by the expected verification request;
- stale preconditions block execution and require a new plan.

Manual acceptance uses a running Sub2API deployment with a non-production admin account:

1. run `snapshot` and confirm the result matches the admin Ops dashboard;
2. create a plan for resolving a known test error and inspect the preview;
3. confirm that `execute` without the exact token is rejected;
4. execute with the token and confirm the post-state readback;
5. verify the existing admin audit log contains the mutation;
6. confirm forbidden operations have no command path.

## Non-goals and Follow-up

This release does not infer remediation from free-form text without evidence, run on a cron schedule, send external notifications, or modify Sub2API backend code. A later phase may add a backend-resident action-plan API with server-side TTL, RBAC scopes, and richer audit linkage once the Skill workflow has real operational feedback.
