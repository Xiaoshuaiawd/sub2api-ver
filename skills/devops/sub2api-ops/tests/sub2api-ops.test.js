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
