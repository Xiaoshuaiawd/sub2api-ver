"use strict";

const assert = require("node:assert/strict");
const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  ACTION_SPECS,
  buildReadRequest,
  createApiClient,
  createMutationPlan,
  executeMutationPlan,
  main,
  redactValue,
} = require("../scripts/sub2api-ops.js");

function canonicalJson(value) {
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${canonicalJson(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

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

test("read commands reject conflicting time filters", () => {
  assert.throws(
    () => buildReadRequest("snapshot", [], {
      "time-range": "1h",
      "start-time": "2026-08-18T00:00:00Z",
    }),
    /either --time-range or --start-time\/--end-time/,
  );
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

test("every approved action maps to its exact write request", async (t) => {
  const until = "2026-08-18T13:00:00Z";
  const cases = [
    {
      action: "resolve-error",
      flags: { id: "42" },
      before: { id: 42, resolved: false, status_code: 502 },
      expected: { method: "PUT", path: "/api/v1/admin/ops/errors/42/resolve", body: { resolved: true } },
    },
    {
      action: "resolve-request-error",
      flags: { id: "43" },
      before: { id: 43, resolved: false, status_code: 500 },
      expected: { method: "PUT", path: "/api/v1/admin/ops/request-errors/43/resolve", body: { resolved: true } },
    },
    {
      action: "resolve-upstream-error",
      flags: { id: "44" },
      before: { id: 44, resolved: false, status_code: 429 },
      expected: { method: "PUT", path: "/api/v1/admin/ops/upstream-errors/44/resolve", body: { resolved: true } },
    },
    {
      action: "resolve-alert-event",
      flags: { id: "45" },
      before: { id: 45, rule_id: 9, status: "firing", severity: "P1" },
      expected: { method: "PUT", path: "/api/v1/admin/ops/alert-events/45/status", body: { status: "manual_resolved" } },
    },
    {
      action: "silence-alert",
      flags: { "rule-id": "7", until, reason: "provider maintenance", platform: "openai" },
      before: [{ id: 7, name: "OpenAI errors", enabled: true, severity: "P1", metric_type: "error_rate" }],
      expected: {
        method: "POST",
        path: "/api/v1/admin/ops/alert-silences",
        body: { rule_id: 7, until, reason: "provider maintenance", platform: "openai" },
      },
    },
    {
      action: "clear-account-error",
      flags: { id: "46" },
      before: { id: 46, name: "account-a", status: "active", error_message: "expired token" },
      expected: { method: "POST", path: "/api/v1/admin/accounts/46/clear-error", body: null },
    },
    {
      action: "clear-account-rate-limit",
      flags: { id: "47" },
      before: { id: 47, name: "account-b", status: "active", rate_limited_at: "2026-08-18T11:00:00Z" },
      expected: { method: "POST", path: "/api/v1/admin/accounts/47/clear-rate-limit", body: null },
    },
    {
      action: "recover-account-state",
      flags: { id: "48" },
      before: { id: 48, name: "account-c", status: "active", temp_unschedulable_until: "2026-08-18T13:00:00Z" },
      expected: { method: "POST", path: "/api/v1/admin/accounts/48/recover-state", body: null },
    },
  ];

  for (const item of cases) {
    const stateDir = tempStateDir(t);
    const api = fakeApi([item.before]);
    const result = await createMutationPlan({
      action: item.action,
      flags: item.flags,
      api,
      stateDir,
      now: () => new Date("2026-08-18T12:00:00Z"),
    });
    const stored = JSON.parse(fs.readFileSync(result.plan_file, "utf8"));
    assert.deepEqual(stored.request, item.expected, item.action);
  }
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
  const plan = await createMutationPlan({
    action: "resolve-error",
    flags: { id: "42" },
    api,
    stateDir,
  });

  await assert.rejects(
    () => executeMutationPlan({
      planFile: plan.plan_file,
      confirmationToken: "wrong",
      api,
      stateDir,
    }),
    (error) => error.exitCode === 2,
  );
  assert.equal(api.calls.length, 1);
});

test("execute rejects expired plans", async (t) => {
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
    () => executeMutationPlan({
      planFile: plan.plan_file,
      confirmationToken: plan.confirmation_token,
      api,
      stateDir,
      now: () => new Date("2026-08-18T12:11:00Z"),
    }),
    (error) => error.exitCode === 2 && /expired/.test(error.message),
  );
});

test("execute rejects tampered plans", async (t) => {
  const stateDir = tempStateDir(t);
  const api = fakeApi([{ id: 42, resolved: false }]);
  const plan = await createMutationPlan({
    action: "resolve-error",
    flags: { id: "42" },
    api,
    stateDir,
  });
  const stored = JSON.parse(fs.readFileSync(plan.plan_file, "utf8"));
  stored.request.path = "/api/v1/admin/accounts/1/delete";
  fs.writeFileSync(plan.plan_file, JSON.stringify(stored));

  await assert.rejects(
    () => executeMutationPlan({
      planFile: plan.plan_file,
      confirmationToken: plan.confirmation_token,
      api,
      stateDir,
    }),
    (error) => error.exitCode === 2 && /signature/.test(error.message),
  );
});

test("execute rejects a validly signed request that does not match its action", async (t) => {
  const stateDir = tempStateDir(t);
  const before = { id: 42, resolved: false, status_code: 502 };
  const api = fakeApi([before, before]);
  const planResult = await createMutationPlan({
    action: "resolve-error",
    flags: { id: "42" },
    api,
    stateDir,
  });
  const stored = JSON.parse(fs.readFileSync(planResult.plan_file, "utf8"));
  stored.request.path = "/api/v1/admin/accounts/1/delete";
  const key = fs.readFileSync(path.join(stateDir, "hmac.key"));
  const { signature: _oldSignature, ...payload } = stored;
  stored.signature = crypto
    .createHmac("sha256", key)
    .update(canonicalJson(payload))
    .digest("hex");
  fs.writeFileSync(planResult.plan_file, JSON.stringify(stored));

  await assert.rejects(
    () => executeMutationPlan({
      planFile: planResult.plan_file,
      confirmationToken: planResult.confirmation_token,
      api,
      stateDir,
    }),
    (error) => error.exitCode === 2 && /does not match action/.test(error.message),
  );
  assert.equal(api.calls.length, 1);
});

test("execute blocks a stale precondition before the write", async (t) => {
  const stateDir = tempStateDir(t);
  const api = fakeApi([
    { id: 42, resolved: false, status_code: 502 },
    { id: 42, resolved: true, status_code: 502 },
  ]);
  const plan = await createMutationPlan({
    action: "resolve-error",
    flags: { id: "42" },
    api,
    stateDir,
  });

  await assert.rejects(
    () => executeMutationPlan({
      planFile: plan.plan_file,
      confirmationToken: plan.confirmation_token,
      api,
      stateDir,
    }),
    (error) => error.exitCode === 3 && /changed/.test(error.message),
  );
  assert.equal(api.calls.length, 2);
});

test("execute rechecks, writes once, verifies, and blocks replay", async (t) => {
  const stateDir = tempStateDir(t);
  const api = fakeApi([
    { id: 42, resolved: false, status_code: 502 },
    { id: 42, resolved: false, status_code: 502 },
    { ok: true },
    { id: 42, resolved: true, status_code: 502 },
  ]);
  const plan = await createMutationPlan({
    action: "resolve-error",
    flags: { id: "42" },
    api,
    stateDir,
  });
  const result = await executeMutationPlan({
    planFile: plan.plan_file,
    confirmationToken: plan.confirmation_token,
    api,
    stateDir,
  });

  assert.equal(result.verification.ok, true);
  assert.deepEqual(api.calls[2], {
    method: "PUT",
    path: "/api/v1/admin/ops/errors/42/resolve",
    body: { resolved: true },
  });
  await assert.rejects(
    () => executeMutationPlan({
      planFile: plan.plan_file,
      confirmationToken: plan.confirmation_token,
      api,
      stateDir,
    }),
    (error) => error.exitCode === 2 && /consumed/.test(error.message),
  );
});

test("unknown mutation actions fail before authentication setup", async () => {
  await assert.rejects(
    () => main(["plan", "delete-account", "--id", "1"], {}),
    (error) => error.exitCode === 1 && /unknown mutation action/.test(error.message),
  );
});
