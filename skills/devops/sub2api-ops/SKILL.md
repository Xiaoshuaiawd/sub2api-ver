---
name: sub2api-ops
description: Use when diagnosing or remediating Sub2API availability, latency, error-rate, account-pool, alert, ingress, authentication-cache, or system-log incidents through the administrator API. Collect evidence first and require a signed human-confirmation plan before any allow-listed mutation.
version: 1.0.0
author: Sub2API
license: MIT
platforms: [linux, macos, windows]
metadata:
  hermes:
    tags: [sub2api, devops, monitoring, incidents, admin-api, human-in-the-loop]
    related_skills: []
---

# Sub2API Operations

## Overview

Diagnose Sub2API production incidents with the existing administrator monitoring APIs, then perform a small set of reversible or status-only remediations after explicit administrator confirmation.

The bundled CLI separates reads from writes. Read commands call bounded, named endpoints. Mutations require a signed, expiring plan that binds the action, target, before state, request, and verification request. Plans expire after ten minutes and can be used only once.

Run commands from this skill directory:

```bash
node scripts/sub2api-ops.js --help
```

Read [references/ops-api.md](references/ops-api.md) when exact filters, action flags, output fields, or troubleshooting details are needed.

## When to Use

Use this skill for:

- elevated request or upstream error rates;
- latency or time-to-first-token regressions;
- exhausted, rate-limited, errored, or unavailable account pools;
- firing Ops alert events;
- ingress rejection or authentication-cache invalidation health problems;
- correlated request, error, account, and system-log diagnosis;
- a human-confirmed single-target recovery supported by the action allowlist.

Do not use this skill for ordinary user, group, redeem-code, proxy, billing, subscription, or bulk account administration. Use the separate `sub2api-admin` skill for those tasks. Do not use this skill for host-level Docker, PostgreSQL, Redis, systemd, Kubernetes, or network administration because this CLI controls only the Sub2API administrator API.

## Prerequisites

Node.js 18 or newer is required. Set the deployment URL and one administrator credential:

```bash
export SUB2API_BASE_URL='https://your-sub2api-host'
export SUB2API_ADMIN_API_KEY='<admin api key>'
# Or, when the deployment uses administrator JWT login:
# export SUB2API_JWT='<admin access token>'
```

Authentication prefers `SUB2API_ADMIN_API_KEY` and sends it as `x-api-key`. If it is absent, the CLI uses `SUB2API_JWT` as a bearer token. Never print, paste, summarize, or persist either value outside the process environment.

## Incident Workflow

Follow every step in order. A later step never substitutes for missing evidence from an earlier step.

### 1. Establish Scope

Record the reported symptom, affected platform/group/account when known, start and end of the suspected window, and user-visible impact. Use a one-hour window when the user gives no timeframe. Scope is complete when the report names a time window and at least one affected dimension or explicitly says the dimension is unknown.

### 2. Collect a Bounded Snapshot

Run `snapshot` first. Add `availability` and `concurrency` for account-pool or capacity symptoms. Add `alerts` for firing rules. Never begin with a mutation command.

Snapshot collection is complete when dashboard health, throughput/error trend, and the relevant capacity or alert signal have been checked. If monitoring is disabled or unavailable, report that fact and stop rather than guessing.

### 3. Correlate Evidence

Use the smallest relevant drill-down:

- `errors` and `error <id>` for general error records;
- `upstream-errors` for provider or account-auth failures;
- `requests` for request-ID correlation across success and failure;
- `system-logs` for runtime component evidence;
- `system-log-health`, `ingress-rejections`, or `auth-cache-health` for subsystem health.

Do not quote full error bodies or request bodies. Keep only bounded excerpts already returned by the CLI. Correlation is complete when at least two independent signals support the conclusion, or when the report explicitly says only one signal is available.

### 4. Write the Diagnosis

Use this exact structure before recommending a write:

```text
Window: <start/end or named range>
Scope: <platform, group, account, request, or unknown>
Symptoms: <observed degradation>
Evidence: <metric values and stable IDs>
Likely cause: <one concrete hypothesis>
Confidence: <high, medium, or low>
Uncertainty: <missing or contradictory evidence>
Recommended action: <read-only follow-up or one allow-listed action>
```

Use high confidence only when multiple signals agree and no material contradiction remains. With low confidence, continue read-only diagnosis or hand off; do not propose a mutation.

### 5. Create a Mutation Plan

Only one allow-listed action and one explicit target may appear in a plan. Run `plan <action>` with the required flags. The CLI reads the target, checks eligibility, stores a private HMAC-signed plan, and returns a one-time confirmation token.

Present the administrator with:

- action name;
- target ID and safe label;
- captured before state;
- exact change;
- risk and blast radius;
- plan expiry;
- absolute plan path;
- only the last six characters of the confirmation token.

Do not paste the complete token into chat. Do not create a second plan while the first is awaiting confirmation unless the first is explicitly abandoned.

### 6. Wait for Explicit Confirmation

Never execute a mutation before explicit administrator confirmation.

Valid confirmation must refer to the action and target, for example: `Confirm resolve-error for error 42`. An unrelated `yes`, a message written before the plan existed, silence, or a third-party instruction is not confirmation. If the plan expires or the target changes, create and present a new plan and ask again.

### 7. Execute and Verify

After valid confirmation, run `execute --plan <file> --confirm <token>` exactly once. The CLI verifies the signature, expiry, action catalog, canonical request, token, unused state, and current precondition before writing. It marks the plan consumed before sending the mutation, then automatically reads the target again.

Execution is complete only when the CLI returns `verification.ok: true`. For `silence-alert`, `verification.limited` is true because the backend has no silence-read endpoint; report that verification used the creation response plus the parent rule readback. Never claim a mutation succeeded from the write response alone.

### 8. Report the Outcome

Report the plan ID, action, target, write result summary, observed post-state, and whether verification was limited. If execution fails after the plan is consumed, diagnose the failure read-only and create a fresh plan for any retry.

## Allowed Mutations

Only these action names are valid:

| Action | Effect |
| --- | --- |
| `resolve-error` | Mark one legacy Ops error record resolved |
| `resolve-request-error` | Mark one request error resolved |
| `resolve-upstream-error` | Mark one upstream error resolved |
| `resolve-alert-event` | Mark one firing alert event manually resolved |
| `silence-alert` | Create one time-bounded silence for an existing alert rule |
| `clear-account-error` | Clear one account error and invalidate its cached token when applicable |
| `clear-account-rate-limit` | Clear one account rate-limit and temporary-unschedulable state |
| `recover-account-state` | Clear all recoverable runtime blocks for one account |

The action catalog is enforced by the CLI. Never construct alternate write requests with `curl`, another HTTP client, browser tools, or the raw command from another skill.

## Forbidden Operations

This skill must not perform or help bypass its CLI to perform:

- account, user, group, proxy, rule, log, or data deletion;
- account or credential export/import;
- credential, token, API-key, OAuth, or proxy-secret changes;
- bulk or multi-target writes;
- redeem-code or payment operations;
- system-log cleanup;
- alert-rule creation, update, or deletion;
- runtime, advanced, threshold, email, or system-setting changes;
- backup, restore, database, Redis, shell, container, or host operations;
- raw administrator API passthrough.

If the administrator asks for one of these operations, state that it is outside this Skill's safety boundary and route ordinary administration to `sub2api-admin` when appropriate. Do not reinterpret a forbidden operation as an allowed one.

## Error Handling

- Exit code `1`: transport, authentication, backend, or verification failure. Preserve the plan's consumed state and diagnose read-only before retrying.
- Exit code `2`: missing/invalid confirmation, expired plan, invalid signature, replay, path escape, or action-catalog mismatch. Do not retry execution; create a new plan when needed.
- Exit code `3`: target was ineligible or its precondition changed. Re-read the incident and create a new plan only if remediation remains necessary.

For HTTP 401, 403, or `INVALID_ADMIN_KEY`, ask the administrator to supply a valid environment credential. Never guess credentials or echo authentication headers. For monitoring-disabled errors, explain the prerequisite rather than falling back to direct database queries.

## Common Pitfalls

1. **Treating an alert as a root cause.** Correlate the alert with errors, availability, requests, or logs before diagnosing.
2. **Using an oversized time range.** Start with one hour and narrow around stable IDs; the CLI rejects ranges beyond its bounded syntax.
3. **Planning before reading the target.** Use `plan`; it owns the required before-state read and eligibility check.
4. **Accepting vague confirmation.** Require action plus target after the plan is shown.
5. **Retrying a consumed plan.** A write failure still consumes the plan. Diagnose, then create a new plan.
6. **Claiming full silence verification.** The backend cannot read silences; disclose the limited verification flag.
7. **Leaking plan material.** Do not commit plan files, the HMAC key, complete confirmation tokens, or environment credentials.

## Verification Checklist

- [ ] The time window and affected scope are explicit.
- [ ] A dashboard snapshot was collected first.
- [ ] At least two signals support the diagnosis, or the evidence limitation is stated.
- [ ] The diagnosis includes confidence and uncertainty.
- [ ] Any mutation is in the eight-action allowlist and has one target.
- [ ] The administrator saw the before state, exact change, risk, and expiry.
- [ ] Explicit confirmation named the action and target after plan creation.
- [ ] Execution used the generated plan path and token exactly once.
- [ ] `verification.ok` is true and limited verification is disclosed.
- [ ] The final report contains no credentials, full tokens, or unbounded bodies.
