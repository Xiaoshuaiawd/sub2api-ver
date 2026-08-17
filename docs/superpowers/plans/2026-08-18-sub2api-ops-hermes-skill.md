# Sub2API Ops Hermes Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a repository-local Hermes Skill that diagnoses Sub2API incidents and executes only allow-listed remediation after a signed, expiring, one-time human confirmation plan.

**Architecture:** A self-contained CommonJS Node CLI owns HTTP transport, redaction, read commands, mutation planning, plan signing, execution, and post-write verification. `SKILL.md` owns the Hermes workflow and safety policy, while `references/ops-api.md` provides command recipes and endpoint semantics without loading them into every turn.

**Tech Stack:** Node.js 18+ built-ins (`fetch`, `node:crypto`, `node:fs`, `node:os`, `node:path`, `node:test`), Hermes `SKILL.md`, existing Sub2API admin REST APIs.

## Global Constraints

- Do not modify backend or frontend application code.
- Do not modify `skills/sub2api-admin`; the new Skill has no raw API passthrough.
- Authenticate with `SUB2API_ADMIN_API_KEY` first, then `SUB2API_JWT`.
- Default read window is one hour; reject windows larger than 30 days.
- Mutation plans expire after 10 minutes, are HMAC-signed, bind method/path/body/precondition, and are single-use.
- Plans must never persist credentials, access tokens, cookies, authorization headers, or unredacted error bodies.
- Supported writes are exactly the eight actions in the approved design; all other action names fail before any request.
- Do not stage or commit unrelated dirty-worktree files.

## File Structure

- Create `skills/devops/sub2api-ops/scripts/sub2api-ops.js`: CLI entrypoint plus exported, dependency-injectable transport, redaction, command, plan, and execution functions.
- Create `skills/devops/sub2api-ops/tests/sub2api-ops.test.js`: Node built-in tests for command construction, API envelopes, redaction, plans, signatures, TTL, replay protection, action mappings, and verification.
- Create `skills/devops/sub2api-ops/SKILL.md`: Hermes trigger metadata, diagnostic workflow, confirmation gate, action allowlist, forbidden operations, and completion criteria.
- Create `skills/devops/sub2api-ops/references/ops-api.md`: environment setup, exact read/write recipes, filter reference, exit codes, and troubleshooting.

---

### Task 1: Read-only Ops CLI

**Files:**
- Create: `skills/devops/sub2api-ops/scripts/sub2api-ops.js`
- Create: `skills/devops/sub2api-ops/tests/sub2api-ops.test.js`

**Interfaces:**
- Produces: `parseArgs(argv)`, `buildReadRequest(command, positional, flags)`, `redactValue(value)`, `createApiClient(options)`, and `runReadCommand(args, api)`.
- Produces CLI commands: `snapshot`, `availability`, `concurrency`, `alerts`, `alert-event`, `errors`, `error`, `upstream-errors`, `requests`, `system-logs`, `system-log-health`, `ingress-rejections`, and `auth-cache-health`.

- [ ] **Step 1: Write failing tests for read requests, envelopes, and redaction**

Append this test foundation and focused cases to `skills/devops/sub2api-ops/tests/sub2api-ops.test.js`:

```js
"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  buildReadRequest,
  createApiClient,
  redactValue,
} = require("../scripts/sub2api-ops.js");

test("snapshot uses the bounded default time range", () => {
  assert.deepEqual(buildReadRequest("snapshot", [], {}), {
    method: "GET",
    path: "/api/v1/admin/ops/dashboard/snapshot-v2?time_range=1h",
  });
});

test("upstream-errors carries supported filters and a bounded page size", () => {
  assert.deepEqual(
    buildReadRequest("upstream-errors", [], {
      platform: "openai",
      "group-id": "7",
      resolved: "false",
      "page-size": "999",
    }),
    {
      method: "GET",
      path: "/api/v1/admin/ops/upstream-errors?time_range=1h&platform=openai&group_id=7&resolved=false&page_size=500",
    },
  );
});

test("detail commands reject non-positive IDs", () => {
  assert.throws(() => buildReadRequest("error", ["0"], {}), /positive integer/);
});

test("redaction removes sensitive keys and truncates opaque bodies", () => {
  assert.deepEqual(
    redactValue({
      id: 4,
      access_token: "secret-value",
      nested: { api_key: "key-value" },
      error_body: "x".repeat(700),
    }),
    {
      id: 4,
      access_token: "[REDACTED]",
      nested: { api_key: "[REDACTED]" },
      error_body: `${"x".repeat(512)}...[TRUNCATED]`,
    },
  );
});

test("API client unwraps the Sub2API success envelope", async () => {
  const calls = [];
  const api = createApiClient({
    baseUrl: "https://sub2api.test",
    adminApiKey: "admin-key",
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      return new Response(JSON.stringify({ code: 0, data: { health_score: 99 } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    },
  });
  assert.deepEqual(await api.request("GET", "/api/v1/admin/ops/dashboard/overview"), {
    health_score: 99,
  });
  assert.equal(calls[0].options.headers["x-api-key"], "admin-key");
});

test("API errors do not expose credentials", async () => {
  const api = createApiClient({
    baseUrl: "https://sub2api.test",
    jwt: "private-jwt",
    fetchImpl: async () => new Response(
      JSON.stringify({ code: "INVALID_ADMIN_KEY", message: "Authorization: Bearer private-jwt" }),
      { status: 401 },
    ),
  });
  await assert.rejects(
    () => api.request("GET", "/api/v1/admin/ops/concurrency"),
    (error) => error.exitCode === 1 && !error.message.includes("private-jwt"),
  );
});
```

- [ ] **Step 2: Run the focused tests and confirm the missing module failure**

Run:

```bash
node --test skills/devops/sub2api-ops/tests/sub2api-ops.test.js
```

Expected: FAIL with `Cannot find module '../scripts/sub2api-ops.js'`.

- [ ] **Step 3: Implement parsing, validation, read mappings, transport, and redaction**

Create `skills/devops/sub2api-ops/scripts/sub2api-ops.js` with these public constants and functions:

```js
#!/usr/bin/env node
"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const MAX_OPAQUE_LENGTH = 512;
const SENSITIVE_KEY = /(token|secret|password|credential|authorization|cookie|api[_-]?key|refresh[_-]?token)/i;
const OPAQUE_KEY = /(error_body|response_body|request_body|raw|stack)/i;
const ALLOWED_TIME_RANGE = /^(?:[1-9]|[12][0-9]|30)(?:m|h|d)$/;

class CliError extends Error {
  constructor(message, exitCode = 1) {
    super(message);
    this.exitCode = exitCode;
  }
}

function parseArgs(argv) {
  const positional = [];
  const flags = {};
  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (!token.startsWith("--")) {
      positional.push(token);
      continue;
    }
    const key = token.slice(2);
    const next = argv[index + 1];
    if (!next || next.startsWith("--")) flags[key] = true;
    else {
      flags[key] = next;
      index += 1;
    }
  }
  return { positional, flags };
}

function positiveInt(value, name) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) throw new CliError(`${name} must be a positive integer`);
  return parsed;
}

function boundedInt(value, fallback, maximum, name) {
  if (value === undefined) return fallback;
  return Math.min(positiveInt(value, name), maximum);
}

function queryString(entries) {
  const query = new URLSearchParams();
  for (const [key, value] of entries) {
    if (value !== undefined && value !== null && value !== "") query.set(key, String(value));
  }
  const rendered = query.toString();
  return rendered ? `?${rendered}` : "";
}

function commonWindow(flags) {
  const range = flags["time-range"] || "1h";
  if ((flags["start-time"] || flags["end-time"]) && flags["time-range"]) {
    throw new CliError("use either --time-range or --start-time/--end-time");
  }
  if (!flags["start-time"] && !flags["end-time"] && !ALLOWED_TIME_RANGE.test(range)) {
    throw new CliError("--time-range must be 1-30 followed by m, h, or d");
  }
  return flags["start-time"] || flags["end-time"]
    ? [["start_time", flags["start-time"]], ["end_time", flags["end-time"]]]
    : [["time_range", range]];
}

function requireId(positional, label = "id") {
  return positiveInt(positional[0], label);
}

function buildReadRequest(command, positional, flags) {
  const scoped = () => [
    ...commonWindow(flags),
    ["platform", flags.platform],
    ["group_id", flags["group-id"] && positiveInt(flags["group-id"], "group-id")],
  ];
  const routes = {
    snapshot: ["/api/v1/admin/ops/dashboard/snapshot-v2", scoped()],
    availability: ["/api/v1/admin/ops/account-availability", scoped().slice(1)],
    concurrency: ["/api/v1/admin/ops/concurrency", scoped().slice(1)],
    alerts: ["/api/v1/admin/ops/alert-events", [["limit", boundedInt(flags.limit, 20, 100, "limit")], ["status", flags.status], ["severity", flags.severity], ["platform", flags.platform], ["group_id", flags["group-id"]]]],
    errors: ["/api/v1/admin/ops/errors", [...scoped(), ["page_size", boundedInt(flags["page-size"], 100, 500, "page-size")], ["resolved", flags.resolved], ["q", flags.query]]],
    "upstream-errors": ["/api/v1/admin/ops/upstream-errors", [...scoped(), ["resolved", flags.resolved], ["page_size", boundedInt(flags["page-size"], 100, 500, "page-size")], ["q", flags.query]]],
    requests: ["/api/v1/admin/ops/requests", [...scoped(), ["page_size", boundedInt(flags["page-size"], 50, 100, "page-size")], ["request_id", flags["request-id"]], ["q", flags.query]]],
    "system-logs": ["/api/v1/admin/ops/system-logs", [...scoped(), ["page_size", boundedInt(flags["page-size"], 100, 200, "page-size")], ["level", flags.level], ["component", flags.component], ["request_id", flags["request-id"]], ["account_id", flags["account-id"]], ["q", flags.query]]],
    "ingress-rejections": ["/api/v1/admin/ops/ingress-rejections", [...scoped(), ["page_size", boundedInt(flags["page-size"], 100, 200, "page-size")]]],
  };
  if (command === "alert-event") return { method: "GET", path: `/api/v1/admin/ops/alert-events/${requireId(positional)}` };
  if (command === "error") return { method: "GET", path: `/api/v1/admin/ops/errors/${requireId(positional)}` };
  if (command === "system-log-health") return { method: "GET", path: "/api/v1/admin/ops/system-logs/health" };
  if (command === "auth-cache-health") return { method: "GET", path: "/api/v1/admin/ops/auth-cache-invalidation/health" };
  if (!routes[command]) throw new CliError(`unknown read command: ${command}`);
  const [basePath, entries] = routes[command];
  return { method: "GET", path: `${basePath}${queryString(entries)}` };
}

function redactValue(value, key = "") {
  if (SENSITIVE_KEY.test(key)) return "[REDACTED]";
  if (typeof value === "string" && OPAQUE_KEY.test(key) && value.length > MAX_OPAQUE_LENGTH) {
    return `${value.slice(0, MAX_OPAQUE_LENGTH)}...[TRUNCATED]`;
  }
  if (Array.isArray(value)) return value.map((item) => redactValue(item));
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).map(([childKey, child]) => [childKey, redactValue(child, childKey)]));
  }
  return value;
}

function createApiClient({ baseUrl, adminApiKey, jwt, fetchImpl = globalThis.fetch }) {
  const normalized = String(baseUrl || "").replace(/\/$/, "");
  if (!normalized) throw new CliError("Missing SUB2API_BASE_URL");
  if (!adminApiKey && !jwt) throw new CliError("Missing SUB2API_ADMIN_API_KEY or SUB2API_JWT");
  const auth = adminApiKey ? { "x-api-key": adminApiKey } : { authorization: `Bearer ${jwt}` };
  return {
    async request(method, requestPath, body) {
      const response = await fetchImpl(`${normalized}${requestPath}`, {
        method,
        headers: { accept: "application/json", ...auth, ...(body === undefined ? {} : { "content-type": "application/json" }) },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      const text = await response.text();
      let envelope;
      try { envelope = text ? JSON.parse(text) : {}; }
      catch { envelope = { message: text.slice(0, MAX_OPAQUE_LENGTH) }; }
      if (!response.ok || (envelope.code !== undefined && String(envelope.code) !== "0")) {
        const authFailure = response.status === 401 || response.status === 403 || envelope.code === "INVALID_ADMIN_KEY";
        throw new CliError(authFailure ? "Sub2API admin authentication failed" : `${method} ${requestPath} failed: ${String(envelope.message || response.statusText).slice(0, MAX_OPAQUE_LENGTH)}`);
      }
      return envelope.data;
    },
  };
}
```

Add `runReadCommand`, JSON printing, usage text, `main`, `require.main === module` handling, and `module.exports` for every tested function. `main` constructs the client from environment variables and never prints request headers.

- [ ] **Step 4: Run the tests and make the read-only suite pass**

Run:

```bash
node --test skills/devops/sub2api-ops/tests/sub2api-ops.test.js
node skills/devops/sub2api-ops/scripts/sub2api-ops.js --help
```

Expected: all tests PASS; help exits `0` and lists only the documented read commands plus `plan` and `execute` placeholders.

- [ ] **Step 5: Commit the read-only CLI**

```bash
git add skills/devops/sub2api-ops/scripts/sub2api-ops.js skills/devops/sub2api-ops/tests/sub2api-ops.test.js
git commit -m "feat: add Sub2API ops diagnostic CLI"
```

---

### Task 2: Signed Mutation Plans and Verified Execution

**Files:**
- Modify: `skills/devops/sub2api-ops/scripts/sub2api-ops.js`
- Modify: `skills/devops/sub2api-ops/tests/sub2api-ops.test.js`

**Interfaces:**
- Consumes: `CliError`, `positiveInt`, `redactValue`, and `createApiClient` from Task 1.
- Produces: `ACTION_SPECS`, `createMutationPlan(options)`, `executeMutationPlan(options)`, and CLI commands `plan <action>` and `execute --plan <file> --confirm <token>`.
- Plan JSON fields: `version`, `id`, `action`, `request`, `precondition`, `verification`, `created_at`, `expires_at`, `consumed_at`, `token_digest`, and `signature`.

- [ ] **Step 1: Write failing tests for allowlisting, signing, TTL, replay, and verification**

Append tests using a per-test temporary directory and injected clock/API:

```js
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const {
  ACTION_SPECS,
  createMutationPlan,
  executeMutationPlan,
} = require("../scripts/sub2api-ops.js");

function tempStateDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "sub2api-ops-test-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function fakeApi(sequence) {
  const calls = [];
  return {
    calls,
    async request(method, requestPath, body) {
      calls.push({ method, path: requestPath, body });
      const next = sequence.shift();
      if (next instanceof Error) throw next;
      return next;
    },
  };
}

test("the action catalog contains exactly the approved actions", () => {
  assert.deepEqual(Object.keys(ACTION_SPECS).sort(), [
    "clear-account-error",
    "clear-account-rate-limit",
    "recover-account-state",
    "resolve-alert-event",
    "resolve-error",
    "resolve-request-error",
    "resolve-upstream-error",
    "silence-alert",
  ]);
});

test("resolve-error plan binds target and produces a private signed plan", async (t) => {
  const stateDir = tempStateDir(t);
  const api = fakeApi([{ id: 42, resolved: false, status_code: 502 }]);
  const result = await createMutationPlan({
    action: "resolve-error",
    flags: { id: "42" },
    api,
    stateDir,
    now: () => new Date("2026-08-18T12:00:00Z"),
  });
  assert.match(result.confirmation_token, /^[A-Za-z0-9_-]+$/);
  assert.equal(fs.statSync(result.plan_file).mode & 0o777, 0o600);
  const stored = JSON.parse(fs.readFileSync(result.plan_file, "utf8"));
  assert.equal(stored.request.path, "/api/v1/admin/ops/errors/42/resolve");
  assert.equal(stored.confirmation_token, undefined);
  assert.ok(stored.signature);
});

test("execute rejects a wrong token before the write", async (t) => {
  const stateDir = tempStateDir(t);
  const api = fakeApi([{ id: 42, resolved: false }]);
  const plan = await createMutationPlan({ action: "resolve-error", flags: { id: "42" }, api, stateDir });
  await assert.rejects(
    () => executeMutationPlan({ planFile: plan.plan_file, confirmationToken: "wrong", api, stateDir }),
    (error) => error.exitCode === 2,
  );
  assert.equal(api.calls.length, 1);
});

test("execute rejects expired and tampered plans", async (t) => {
  const stateDir = tempStateDir(t);
  const api = fakeApi([{ id: 42, resolved: false }]);
  const plan = await createMutationPlan({
    action: "resolve-error",
    flags: { id: "42" },
    api,
    stateDir,
    now: () => new Date("2026-08-18T12:00:00Z"),
  });
  await assert.rejects(
    () => executeMutationPlan({ planFile: plan.plan_file, confirmationToken: plan.confirmation_token, api, stateDir, now: () => new Date("2026-08-18T12:11:00Z") }),
    (error) => error.exitCode === 2 && /expired/.test(error.message),
  );
  const stored = JSON.parse(fs.readFileSync(plan.plan_file, "utf8"));
  stored.request.path = "/api/v1/admin/accounts/1/delete";
  fs.writeFileSync(plan.plan_file, JSON.stringify(stored));
  await assert.rejects(
    () => executeMutationPlan({ planFile: plan.plan_file, confirmationToken: plan.confirmation_token, api, stateDir }),
    (error) => error.exitCode === 2 && /signature/.test(error.message),
  );
});

test("execute rechecks precondition, writes once, verifies, and blocks replay", async (t) => {
  const stateDir = tempStateDir(t);
  const api = fakeApi([
    { id: 42, resolved: false, status_code: 502 },
    { id: 42, resolved: false, status_code: 502 },
    { ok: true },
    { id: 42, resolved: true, status_code: 502 },
  ]);
  const plan = await createMutationPlan({ action: "resolve-error", flags: { id: "42" }, api, stateDir });
  const result = await executeMutationPlan({ planFile: plan.plan_file, confirmationToken: plan.confirmation_token, api, stateDir });
  assert.equal(result.verification.ok, true);
  assert.deepEqual(api.calls[2], {
    method: "PUT",
    path: "/api/v1/admin/ops/errors/42/resolve",
    body: { resolved: true },
  });
  await assert.rejects(
    () => executeMutationPlan({ planFile: plan.plan_file, confirmationToken: plan.confirmation_token, api, stateDir }),
    (error) => error.exitCode === 2 && /consumed/.test(error.message),
  );
});
```

Add one table-driven test that creates every action plan and asserts the exact method/path/body and verification path from the approved design. Add a stale-precondition test that changes `resolved` or account runtime fields between planning and execution and expects exit code `3` before the write.

- [ ] **Step 2: Run the plan tests and confirm exported APIs are missing**

Run:

```bash
node --test --test-name-pattern='action|plan|execute' skills/devops/sub2api-ops/tests/sub2api-ops.test.js
```

Expected: FAIL because `ACTION_SPECS`, `createMutationPlan`, and `executeMutationPlan` are not implemented.

- [ ] **Step 3: Implement the exact action catalog**

Add an immutable `ACTION_SPECS` object. Each entry provides `prepare(flags)`, `read`, `write`, `capture(data)`, and `verify(data, writeResult)` behavior. Use these exact mappings:

```js
const ACTION_SPECS = Object.freeze({
  "resolve-error": resolutionSpec("/api/v1/admin/ops/errors"),
  "resolve-request-error": resolutionSpec("/api/v1/admin/ops/request-errors"),
  "resolve-upstream-error": resolutionSpec("/api/v1/admin/ops/upstream-errors"),
  "resolve-alert-event": {
    prepare: idFlags,
    read: ({ id }) => ({ method: "GET", path: `/api/v1/admin/ops/alert-events/${id}` }),
    write: ({ id }) => ({ method: "PUT", path: `/api/v1/admin/ops/alert-events/${id}/status`, body: { status: "manual_resolved" } }),
    capture: (data) => pick(data, ["id", "rule_id", "status", "severity", "fired_at"]),
    eligible: (data) => data.status === "firing",
    verify: (data) => ["resolved", "manual_resolved"].includes(data.status),
  },
  "silence-alert": {
    prepare: silenceFlags,
    read: () => ({ method: "GET", path: "/api/v1/admin/ops/alert-rules" }),
    write: (target) => ({ method: "POST", path: "/api/v1/admin/ops/alert-silences", body: target.body }),
    capture: (rules, target) => ({ rule: pick(requireRule(rules, target.ruleId), ["id", "name", "enabled", "severity", "metric_type"]), body: target.body }),
    eligible: (data) => data.rule.enabled !== false,
    verify: (rules, writeResult, target) => Boolean(writeResult && writeResult.id && requireRule(rules, target.ruleId)),
    limitedVerification: true,
  },
  "clear-account-error": accountRecoverySpec("clear-error", (data) => data.error_message === ""),
  "clear-account-rate-limit": accountRecoverySpec("clear-rate-limit", (data) => data.rate_limited_at == null && data.rate_limit_reset_at == null),
  "recover-account-state": accountRecoverySpec("recover-state", (data) => data.error_message === "" && data.rate_limited_at == null && data.temp_unschedulable_until == null),
});
```

`resolutionSpec`, `accountRecoverySpec`, `idFlags`, `silenceFlags`, `pick`, and `requireRule` must validate positive IDs, RFC3339 expiry in the future, non-empty silence reason, allowed optional platform/group/region fields, preconditions, and the safe account field set (`id`, `name`, `platform`, `status`, `schedulable`, `error_message`, `rate_limited_at`, `rate_limit_reset_at`, `overload_until`, `temp_unschedulable_until`, `temp_unschedulable_reason`).

- [ ] **Step 4: Implement signed plan persistence and one-time execution**

Add these concrete helpers and rules:

```js
const PLAN_VERSION = 1;
const PLAN_TTL_MS = 10 * 60 * 1000;

function stableStringify(value) {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

function sha256(value) {
  return crypto.createHash("sha256").update(value).digest("hex");
}

function planPayload(plan) {
  const { signature, ...payload } = plan;
  return payload;
}

function signPlan(plan, key) {
  return crypto.createHmac("sha256", key).update(stableStringify(planPayload(plan))).digest("hex");
}

function safeEqual(left, right) {
  const a = Buffer.from(String(left));
  const b = Buffer.from(String(right));
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}
```

Resolve state storage from the injected `stateDir`, then `SUB2API_OPS_STATE_DIR`, then `${XDG_STATE_HOME}/sub2api-ops`, then `~/.local/state/sub2api-ops`. Create directories as `0700`, the HMAC key and plans as `0600`, and write plans atomically through a same-directory temporary file plus `renameSync`.

`createMutationPlan` performs the action's read request, creates the safe captured precondition, rejects an ineligible target with exit code `3`, generates `crypto.randomBytes(18).toString("base64url")`, stores only its SHA-256 digest, signs the plan, and returns a redacted preview plus `plan_file` and the plaintext `confirmation_token`.

`executeMutationPlan` validates the plan path is inside the resolved plans directory, signature, version, action allowlist, expiry, unused state, and token digest. It re-reads and compares the captured precondition using `stableStringify`; on mismatch it throws `CliError(..., 3)`. It then writes `consumed_at` and a new signature atomically before the mutation request. Finally it performs the verification read and throws a normal transport error when verification fails. The returned object has this exact shape:

```js
{
  plan_id: plan.id,
  action: plan.action,
  write_result: redactValue(writeResult),
  verification: {
    ok: true,
    limited: Boolean(spec.limitedVerification),
    observed: redactValue(spec.capture(after, target)),
  },
}
```

- [ ] **Step 5: Run the entire CLI test suite**

Run:

```bash
node --test skills/devops/sub2api-ops/tests/sub2api-ops.test.js
```

Expected: all read, redaction, action, plan, execution, expiry, tamper, precondition, replay, and verification tests PASS.

- [ ] **Step 6: Commit the confirmation gate**

```bash
git add skills/devops/sub2api-ops/scripts/sub2api-ops.js skills/devops/sub2api-ops/tests/sub2api-ops.test.js
git commit -m "feat: gate Sub2API ops writes with signed plans"
```

---

### Task 3: Hermes Skill and Operator Reference

**Files:**
- Create: `skills/devops/sub2api-ops/SKILL.md`
- Create: `skills/devops/sub2api-ops/references/ops-api.md`
- Modify: `skills/devops/sub2api-ops/tests/sub2api-ops.test.js`

**Interfaces:**
- Consumes CLI commands and exit codes from Tasks 1-2.
- Produces Hermes skill name `sub2api-ops` with trigger-focused metadata and a deterministic diagnosis/confirmation workflow.

- [ ] **Step 1: Write the failing Hermes metadata validation test**

Append this test:

```js
test("SKILL.md has Hermes-compatible frontmatter and required safety language", () => {
  const skillPath = path.resolve(__dirname, "../SKILL.md");
  const content = fs.readFileSync(skillPath, "utf8");
  assert.ok(content.startsWith("---\n"));
  const closing = content.indexOf("\n---\n", 4);
  assert.ok(closing > 4);
  const frontmatter = content.slice(4, closing);
  assert.match(frontmatter, /^name: sub2api-ops$/m);
  assert.match(frontmatter, /^description: Use when /m);
  assert.match(frontmatter, /^version: 1\.0\.0$/m);
  assert.match(frontmatter, /^author: Sub2API$/m);
  assert.match(frontmatter, /^license: MIT$/m);
  assert.match(frontmatter, /^platforms: \[linux, macos, windows\]$/m);
  assert.match(frontmatter, /metadata:\n  hermes:/);
  assert.ok(content.length <= 100_000);
  assert.match(content, /Never execute a mutation before explicit administrator confirmation/);
  assert.match(content, /Forbidden operations/);
});
```

- [ ] **Step 2: Run the metadata test and confirm the missing file failure**

Run:

```bash
node --test --test-name-pattern='SKILL.md' skills/devops/sub2api-ops/tests/sub2api-ops.test.js
```

Expected: FAIL with `ENOENT` for `skills/devops/sub2api-ops/SKILL.md`.

- [ ] **Step 3: Create the Hermes Skill workflow**

Create `SKILL.md` with this frontmatter and section order:

```markdown
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
```

The body must include:

1. `# Sub2API Operations` and a short overview.
2. `## When to Use` with incident triggers and counter-triggers for ordinary account administration.
3. `## Prerequisites` with the three environment variables and Node 18+.
4. `## Incident Workflow` with checkable completion criteria for scope, snapshot, correlation, diagnosis, plan, confirmation, execution, and verification.
5. A required diagnosis report format containing window, scope, symptoms, evidence IDs, likely cause, confidence, uncertainty, and recommended action.
6. `## Human Confirmation Gate` containing the exact sentence `Never execute a mutation before explicit administrator confirmation.` and requiring the displayed action, target, before state, change, risk, expiry, plan path, and token suffix.
7. `## Allowed Mutations` listing exactly the eight action names.
8. `## Forbidden Operations` listing deletion, export/import, credentials, bulk writes, log cleanup, rule deletion, settings, backup/restore, database access, and raw API passthrough.
9. `## Exit Codes` defining `1`, `2`, and `3` behavior.
10. `## Common Pitfalls` and `## Verification Checklist`.
11. A pointer to `references/ops-api.md` for exact commands rather than duplicating every recipe.

- [ ] **Step 4: Create the exact operator reference**

Create `references/ops-api.md` with executable commands from the Skill directory:

```bash
export SUB2API_BASE_URL='https://your-sub2api-host'
export SUB2API_ADMIN_API_KEY='<admin api key>'
node scripts/sub2api-ops.js snapshot --time-range 1h
node scripts/sub2api-ops.js upstream-errors --platform openai --resolved false
node scripts/sub2api-ops.js system-logs --level error --time-range 30m
node scripts/sub2api-ops.js plan resolve-error --id 42
node scripts/sub2api-ops.js execute --plan '/absolute/plan.json' --confirm '<confirmation token>'
```

Document every read command, supported filter, allowed plan action and its required flags, JSON output contract, limited verification for `silence-alert`, authentication failures, exit codes, and the rule that plan files and credentials must not be pasted into chat or committed.

- [ ] **Step 5: Run tests and validate help/reference agreement**

Run:

```bash
node --test skills/devops/sub2api-ops/tests/sub2api-ops.test.js
node skills/devops/sub2api-ops/scripts/sub2api-ops.js --help > /tmp/sub2api-ops-help.txt
rg 'snapshot|availability|concurrency|alerts|upstream-errors|system-logs|plan|execute' /tmp/sub2api-ops-help.txt
```

Expected: all tests PASS and `rg` prints every named command.

- [ ] **Step 6: Commit the Hermes Skill package**

```bash
git add skills/devops/sub2api-ops/SKILL.md skills/devops/sub2api-ops/references/ops-api.md skills/devops/sub2api-ops/tests/sub2api-ops.test.js
git commit -m "docs: add Sub2API ops Hermes skill"
```

---

### Task 4: End-to-End Verification and Installation Smoke Test

**Files:**
- Modify only if verification finds a defect: files under `skills/devops/sub2api-ops/`

**Interfaces:**
- Consumes the complete Skill package.
- Produces a verified repository artifact ready for Hermes installation or publication.

- [ ] **Step 1: Run syntax, unit, formatting, and secret scans**

Run:

```bash
node --check skills/devops/sub2api-ops/scripts/sub2api-ops.js
node --test skills/devops/sub2api-ops/tests/sub2api-ops.test.js
git diff --check HEAD~2 -- skills/devops/sub2api-ops
rg -n 'Bearer [A-Za-z0-9]|sk-[A-Za-z0-9]|BEGIN .*PRIVATE KEY|SUB2API_ADMIN_API_KEY=.+' skills/devops/sub2api-ops || true
```

Expected: syntax check exits `0`, all tests PASS, diff check emits nothing, and the secret scan finds only placeholder documentation values if any.

- [ ] **Step 2: Validate the Hermes package shape**

Run:

```bash
test -f skills/devops/sub2api-ops/SKILL.md
test -f skills/devops/sub2api-ops/references/ops-api.md
test -x skills/devops/sub2api-ops/scripts/sub2api-ops.js
wc -c skills/devops/sub2api-ops/SKILL.md
```

Expected: all `test` commands exit `0`; `SKILL.md` is below 100,000 bytes.

- [ ] **Step 3: Run failure-path CLI smoke tests without credentials**

Run:

```bash
env -u SUB2API_BASE_URL -u SUB2API_ADMIN_API_KEY -u SUB2API_JWT node skills/devops/sub2api-ops/scripts/sub2api-ops.js snapshot
node skills/devops/sub2api-ops/scripts/sub2api-ops.js plan delete-account --id 1
```

Expected: the first command exits `1` with `Missing SUB2API_BASE_URL`; the second exits before any network request with `unknown mutation action: delete-account`.

- [ ] **Step 4: Inspect the final scoped diff**

Run:

```bash
git status --short
git diff --stat 506189084..HEAD -- skills/devops/sub2api-ops
git log --oneline 506189084..HEAD -- skills/devops/sub2api-ops
```

Expected: only planned Skill files appear in the scoped diff; unrelated pre-existing worktree changes remain untouched.

- [ ] **Step 5: Commit verification-only fixes if Step 1-4 required changes**

```bash
git add skills/devops/sub2api-ops
git commit -m "test: harden Sub2API ops skill verification"
```

Skip this commit only when verification required no file changes.
