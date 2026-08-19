# Sub2API Ops CLI Reference

Run all examples from the `sub2api-ops` skill directory.

## Environment

```bash
export SUB2API_BASE_URL='https://your-sub2api-host'
export SUB2API_ADMIN_API_KEY='<admin api key>'
# Or use an administrator JWT instead of the API key:
# export SUB2API_JWT='<admin access token>'
```

`SUB2API_ADMIN_API_KEY` takes precedence when both credentials are set. The CLI never prints request headers. Node.js 18 or newer is required.

## Read Commands

### Dashboard and Capacity

```bash
node scripts/sub2api-ops.js snapshot --time-range 1h
node scripts/sub2api-ops.js snapshot --platform openai --group-id 7
node scripts/sub2api-ops.js availability --platform openai --group-id 7
node scripts/sub2api-ops.js concurrency --platform openai --group-id 7
```

`snapshot` calls `GET /api/v1/admin/ops/dashboard/snapshot-v2`. `availability` and `concurrency` call the matching real-time Ops endpoints and do not accept time-window filters.

### Alerts

```bash
node scripts/sub2api-ops.js alerts --status firing --severity P1 --limit 20
node scripts/sub2api-ops.js alert-event 42
```

`alerts` accepts the common time filters plus `--status`, `--severity`, `--platform`, `--group-id`, and `--limit`. The CLI defaults to `1h` and caps the limit at 100.

### Errors and Requests

```bash
node scripts/sub2api-ops.js errors --time-range 30m --resolved false
node scripts/sub2api-ops.js error 42
node scripts/sub2api-ops.js upstream-errors --platform openai --resolved false
node scripts/sub2api-ops.js requests --request-id req_123 --time-range 1h
```

List commands accept `--platform`, `--group-id`, `--resolved`, `--query`, and `--page-size` where supported. Error lists cap page size at 500; request lists cap it at 100.

### System and Admission Health

```bash
node scripts/sub2api-ops.js system-logs --level error --time-range 30m
node scripts/sub2api-ops.js system-logs --component gateway --account-id 12
node scripts/sub2api-ops.js system-log-health
node scripts/sub2api-ops.js ingress-rejections --time-range 1h
node scripts/sub2api-ops.js auth-cache-health
```

System-log filters include `--level`, `--component`, `--request-id`, `--account-id`, `--query`, `--platform`, and the common window filters. The backend does not support group filtering for system logs. The CLI caps system-log and ingress page sizes at 200.

## Time Filters

Use either a named range or explicit timestamps, never both:

```bash
node scripts/sub2api-ops.js snapshot --time-range 1h
node scripts/sub2api-ops.js snapshot \
  --start-time 2026-08-18T00:00:00Z \
  --end-time 2026-08-18T01:00:00Z
```

Named ranges are `5m`, `30m`, `1h`, `6h`, `24h`, `7d`, or `30d`. The default is `1h`. The backend additionally rejects explicit windows larger than 30 days.

## Mutation Plans

Every mutation is a two-command workflow. The `plan` command performs a before-state read and writes a private plan under the configured state directory.

### Resolve Records

```bash
node scripts/sub2api-ops.js plan resolve-error --id 42
node scripts/sub2api-ops.js plan resolve-request-error --id 42
node scripts/sub2api-ops.js plan resolve-upstream-error --id 42
node scripts/sub2api-ops.js plan resolve-alert-event --id 42
```

The target must exist and currently be unresolved or firing.

### Silence an Alert

```bash
node scripts/sub2api-ops.js plan silence-alert \
  --rule-id 7 \
  --until 2026-08-18T14:00:00Z \
  --reason 'provider maintenance' \
  --platform openai
```

Required flags are `--rule-id`, `--platform`, future RFC3339 `--until`, and non-empty `--reason`. Optional scope flags are `--group-id` and `--region`.

The backend currently has no read endpoint for alert silences. Successful execution therefore returns `verification.limited: true` after validating the creation response and re-reading the parent rule.

### Recover an Account

```bash
node scripts/sub2api-ops.js plan clear-account-error --id 12
node scripts/sub2api-ops.js plan clear-account-rate-limit --id 12
node scripts/sub2api-ops.js plan recover-account-state --id 12
```

The target account is read first. The plan contains only safe account status fields, never credentials or `extra` payloads.

### Execute After Confirmation

The plan output has this shape:

```json
{
  "plan_file": "/absolute/state/sub2api-ops/plans/plan-id.json",
  "confirmation_token": "one-time-token",
  "preview": {
    "plan_id": "plan-id",
    "action": "resolve-error",
    "target": { "id": 42 },
    "before": { "id": 42, "resolved": false },
    "change": {
      "method": "PUT",
      "path": "/api/v1/admin/ops/errors/42/resolve",
      "body": { "resolved": true }
    },
    "risk": "Marks one existing error record as resolved; no request data is deleted.",
    "expires_at": "2026-08-18T12:10:00.000Z"
  }
}
```

After the administrator confirms the displayed action and target:

```bash
node scripts/sub2api-ops.js execute \
  --plan '/absolute/state/sub2api-ops/plans/plan-id.json' \
  --confirm '<confirmation token>'
```

Do not edit a plan. It is HMAC-signed and binds the canonical action request, verification request, and captured precondition. Plans expire after ten minutes and are consumed before the write request so a failed write cannot be replayed.

## State Directory

Resolution order:

1. `SUB2API_OPS_STATE_DIR`;
2. `${XDG_STATE_HOME}/sub2api-ops`;
3. `~/.local/state/sub2api-ops`.

The root and `plans/` directories use mode `0700`. `hmac.key` and plan files use mode `0600`. Never commit, paste, upload, or share these files.

## JSON and Redaction

All successful commands print JSON. Fields whose names match tokens, secrets, passwords, credentials, authorization, cookies, API keys, or refresh tokens are replaced with `[REDACTED]`. Large error, response, request, raw, and stack fields are truncated to 512 characters.

Treat returned IDs, timestamps, statuses, metric values, and safe account labels as evidence. Do not quote full request or upstream payloads even if a future backend version returns them under an unfamiliar key.

## Exit Codes

| Code | Meaning | Operator response |
| --- | --- | --- |
| `0` | Command and required verification succeeded | Report the result |
| `1` | Authentication, transport, backend, or post-write verification failed | Diagnose read-only; create a new plan for any retry |
| `2` | Confirmation, signature, expiry, replay, path, or action-catalog failure | Do not retry the plan |
| `3` | Target ineligible or precondition changed | Re-read and create a new plan if still needed |

## Troubleshooting

### Missing configuration

- `Missing SUB2API_BASE_URL`: set the deployment origin, including `https://`.
- `Missing SUB2API_ADMIN_API_KEY or SUB2API_JWT`: set one administrator credential in the environment.
- `Sub2API admin authentication failed`: regenerate the administrator API key or obtain a fresh administrator JWT. Do not paste it into chat.

### Monitoring unavailable

Ops endpoints may return a feature-disabled or service-unavailable response when monitoring is disabled. Report the missing prerequisite. This Skill must not query PostgreSQL or Redis directly as a fallback.

### Plan rejected

- `plan has expired`: create a new plan and request confirmation again.
- `plan has already been consumed`: do not replay it; diagnose and create a new plan.
- `target state changed`: re-run the relevant read commands before creating a replacement plan.
- `plan request does not match action catalog`: treat the plan or local state as unsafe; do not execute it.

### Forbidden request

There is no raw API command. Account deletion, exports, credential changes, bulk writes, log cleanup, settings, and database operations are intentionally unavailable from this CLI.
