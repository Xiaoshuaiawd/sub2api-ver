#!/usr/bin/env node
"use strict";

const crypto = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const MAX_OPAQUE_LENGTH = 512;
const SENSITIVE_KEY = /(token|secret|password|credential|authorization|cookie|api[_-]?key|refresh[_-]?token)/i;
const OPAQUE_KEY = /(error_body|response_body|request_body|raw|stack)/i;
const ALLOWED_TIME_RANGE = /^(?:5m|30m|1h|6h|24h|7d|30d)$/;
const PLAN_VERSION = 1;
const PLAN_TTL_MS = 10 * 60 * 1000;

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
    if (!next || next.startsWith("--")) {
      flags[key] = true;
    } else {
      flags[key] = next;
      index += 1;
    }
  }
  return { positional, flags };
}

function positiveInt(value, name) {
  const text = typeof value === "string"
    ? value.trim()
    : typeof value === "number" && Number.isSafeInteger(value)
      ? String(value)
      : "";
  const parsed = Number(text);
  if (!/^[1-9]\d*$/.test(text) || !Number.isSafeInteger(parsed)) {
    throw new CliError(`${name} must be a positive integer`);
  }
  return parsed;
}

function requiredString(value, name) {
  if (typeof value !== "string" || !value.trim()) {
    throw new CliError(`${name} must be provided`);
  }
  return value.trim();
}

function boundedInt(value, fallback, maximum, name) {
  if (value === undefined) return fallback;
  return Math.min(positiveInt(value, name), maximum);
}

function queryString(entries) {
  const query = new URLSearchParams();
  for (const [key, value] of entries) {
    if (value !== undefined && value !== null && value !== "") {
      query.set(key, String(value));
    }
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
    throw new CliError("--time-range supported values: 5m, 30m, 1h, 6h, 24h, 7d, 30d");
  }
  if (flags["start-time"] || flags["end-time"]) {
    return [
      ["start_time", flags["start-time"]],
      ["end_time", flags["end-time"]],
    ];
  }
  return [["time_range", range]];
}

function requireId(positional, label = "id") {
  return positiveInt(positional[0], label);
}

function optionalGroupId(flags) {
  return flags["group-id"] === undefined
    ? undefined
    : positiveInt(flags["group-id"], "group-id");
}

function optionalAccountId(flags) {
  return flags["account-id"] === undefined
    ? undefined
    : positiveInt(flags["account-id"], "account-id");
}

function buildReadRequest(command, positional, flags) {
  const scoped = () => [
    ...commonWindow(flags),
    ["platform", flags.platform],
    ["group_id", optionalGroupId(flags)],
  ];
  if (command === "system-logs" && flags["group-id"] !== undefined) {
    throw new CliError("--group-id is not supported by system-logs");
  }
  const routes = {
    snapshot: [
      "/api/v1/admin/ops/dashboard/snapshot-v2",
      scoped(),
    ],
    availability: [
      "/api/v1/admin/ops/account-availability",
      [
        ["platform", flags.platform],
        ["group_id", optionalGroupId(flags)],
      ],
    ],
    concurrency: [
      "/api/v1/admin/ops/concurrency",
      [
        ["platform", flags.platform],
        ["group_id", optionalGroupId(flags)],
      ],
    ],
    alerts: [
      "/api/v1/admin/ops/alert-events",
      [
        ...commonWindow(flags),
        ["limit", boundedInt(flags.limit, 20, 100, "limit")],
        ["status", flags.status],
        ["severity", flags.severity],
        ["platform", flags.platform],
        ["group_id", optionalGroupId(flags)],
      ],
    ],
    errors: [
      "/api/v1/admin/ops/errors",
      [
        ...scoped(),
        ["page_size", boundedInt(flags["page-size"], 100, 500, "page-size")],
        ["resolved", flags.resolved],
        ["q", flags.query],
      ],
    ],
    "upstream-errors": [
      "/api/v1/admin/ops/upstream-errors",
      [
        ...scoped(),
        ["resolved", flags.resolved],
        ["page_size", boundedInt(flags["page-size"], 100, 500, "page-size")],
        ["q", flags.query],
      ],
    ],
    requests: [
      "/api/v1/admin/ops/requests",
      [
        ...scoped(),
        ["page_size", boundedInt(flags["page-size"], 50, 100, "page-size")],
        ["request_id", flags["request-id"]],
        ["q", flags.query],
      ],
    ],
    "system-logs": [
      "/api/v1/admin/ops/system-logs",
      [
        ...commonWindow(flags),
        ["platform", flags.platform],
        ["page_size", boundedInt(flags["page-size"], 100, 200, "page-size")],
        ["level", flags.level],
        ["component", flags.component],
        ["request_id", flags["request-id"]],
        ["account_id", optionalAccountId(flags)],
        ["q", flags.query],
      ],
    ],
    "ingress-rejections": [
      "/api/v1/admin/ops/ingress-rejections",
      [
        ...scoped(),
        ["page_size", boundedInt(flags["page-size"], 100, 200, "page-size")],
      ],
    ],
  };

  if (command === "alert-event") {
    return {
      method: "GET",
      path: `/api/v1/admin/ops/alert-events/${requireId(positional)}`,
    };
  }
  if (command === "error") {
    return {
      method: "GET",
      path: `/api/v1/admin/ops/errors/${requireId(positional)}`,
    };
  }
  if (command === "system-log-health") {
    return { method: "GET", path: "/api/v1/admin/ops/system-logs/health" };
  }
  if (command === "auth-cache-health") {
    return {
      method: "GET",
      path: "/api/v1/admin/ops/auth-cache-invalidation/health",
    };
  }
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
    return Object.fromEntries(
      Object.entries(value).map(([childKey, child]) => [
        childKey,
        redactValue(child, childKey),
      ]),
    );
  }
  return value;
}

function createApiClient({ baseUrl, adminApiKey, jwt, fetchImpl = globalThis.fetch }) {
  const normalized = String(baseUrl || "").replace(/\/$/, "");
  if (!normalized) throw new CliError("Missing SUB2API_BASE_URL");
  if (!adminApiKey && !jwt) {
    throw new CliError("Missing SUB2API_ADMIN_API_KEY or SUB2API_JWT");
  }
  if (typeof fetchImpl !== "function") throw new CliError("Node.js 18+ fetch is required");
  const auth = adminApiKey
    ? { "x-api-key": adminApiKey }
    : { authorization: `Bearer ${jwt}` };
  const knownSecrets = [adminApiKey, jwt].filter(Boolean);

  function safeMessage(value) {
    let message = String(value || "").slice(0, MAX_OPAQUE_LENGTH);
    for (const secret of knownSecrets) message = message.split(secret).join("[REDACTED]");
    return message;
  }

  return {
    async request(method, requestPath, body) {
      let response;
      try {
        response = await fetchImpl(`${normalized}${requestPath}`, {
          method,
          headers: {
            accept: "application/json",
            ...auth,
            ...(body === undefined ? {} : { "content-type": "application/json" }),
          },
          ...(body === undefined ? {} : { body: JSON.stringify(body) }),
        });
      } catch (error) {
        throw new CliError(`${method} ${requestPath} failed: ${safeMessage(error.message)}`);
      }

      const text = await response.text();
      let envelope;
      try {
        envelope = text ? JSON.parse(text) : {};
      } catch {
        envelope = { message: text.slice(0, MAX_OPAQUE_LENGTH) };
      }
      if (!response.ok || (envelope.code !== undefined && String(envelope.code) !== "0")) {
        const authFailure = response.status === 401
          || response.status === 403
          || envelope.code === "INVALID_ADMIN_KEY";
        const message = authFailure
          ? "Sub2API admin authentication failed"
          : `${method} ${requestPath} failed: ${safeMessage(envelope.message || response.statusText)}`;
        throw new CliError(message);
      }
      return envelope.data;
    },
  };
}

function pick(value, keys) {
  const result = {};
  for (const key of keys) {
    if (value && Object.prototype.hasOwnProperty.call(value, key)) {
      result[key] = redactValue(value[key], key);
    }
  }
  return result;
}

function idFlags(flags) {
  return { id: positiveInt(flags.id, "id") };
}

function requireRule(rules, ruleId) {
  if (!Array.isArray(rules)) throw new CliError("alert rules response is not a list", 3);
  const rule = rules.find((item) => Number(item && item.id) === ruleId);
  if (!rule) throw new CliError(`alert rule ${ruleId} no longer exists`, 3);
  return rule;
}

function silenceFlags(flags, now) {
  const ruleId = positiveInt(flags["rule-id"], "rule-id");
  const platform = requiredString(flags.platform, "platform");
  const untilRaw = typeof flags.until === "string" ? flags.until.trim() : "";
  const until = new Date(untilRaw);
  if (!untilRaw || Number.isNaN(until.getTime())) {
    throw new CliError("until must be an RFC3339 timestamp");
  }
  if (until.getTime() <= now.getTime()) throw new CliError("until must be in the future");
  const reason = requiredString(flags.reason, "reason");
  const body = {
    rule_id: ruleId,
    until: until.toISOString().replace(".000Z", "Z"),
    reason,
    platform,
  };
  if (flags["group-id"] !== undefined) {
    body.group_id = positiveInt(flags["group-id"], "group-id");
  }
  if (flags.region !== undefined) body.region = requiredString(flags.region, "region");
  return { ruleId, body };
}

const ERROR_FIELDS = [
  "id",
  "resolved",
  "status_code",
  "error_type",
  "error_phase",
  "account_id",
  "request_id",
  "client_request_id",
];

const ACCOUNT_FIELDS = [
  "id",
  "name",
  "platform",
  "status",
  "schedulable",
  "error_message",
  "rate_limited_at",
  "rate_limit_reset_at",
  "overload_until",
  "temp_unschedulable_until",
  "temp_unschedulable_reason",
];

function resolutionSpec(basePath) {
  return {
    risk: "Marks one existing error record as resolved; no request data is deleted.",
    prepare: idFlags,
    read: ({ id }) => ({ method: "GET", path: `${basePath}/${id}` }),
    write: ({ id }) => ({
      method: "PUT",
      path: `${basePath}/${id}/resolve`,
      body: { resolved: true },
    }),
    capture: (data) => pick(data, ERROR_FIELDS),
    eligible: (data) => data && data.resolved === false,
    verify: (data) => data && data.resolved === true,
  };
}

function accountRecoverySpec(pathSuffix, eligible, verify, risk) {
  return {
    risk,
    prepare: idFlags,
    read: ({ id }) => ({ method: "GET", path: `/api/v1/admin/accounts/${id}` }),
    write: ({ id }) => ({
      method: "POST",
      path: `/api/v1/admin/accounts/${id}/${pathSuffix}`,
      body: null,
    }),
    capture: (data) => pick(data, ACCOUNT_FIELDS),
    eligible,
    verify,
  };
}

const ACTION_SPECS = Object.freeze({
  "resolve-error": resolutionSpec("/api/v1/admin/ops/errors"),
  "resolve-request-error": resolutionSpec("/api/v1/admin/ops/request-errors"),
  "resolve-upstream-error": resolutionSpec("/api/v1/admin/ops/upstream-errors"),
  "resolve-alert-event": {
    risk: "Marks one firing alert event as manually resolved.",
    prepare: idFlags,
    read: ({ id }) => ({
      method: "GET",
      path: `/api/v1/admin/ops/alert-events/${id}`,
    }),
    write: ({ id }) => ({
      method: "PUT",
      path: `/api/v1/admin/ops/alert-events/${id}/status`,
      body: { status: "manual_resolved" },
    }),
    capture: (data) => pick(data, ["id", "rule_id", "status", "severity", "fired_at"]),
    eligible: (data) => data && data.status === "firing",
    verify: (data) => data && ["resolved", "manual_resolved"].includes(data.status),
  },
  "silence-alert": {
    risk: "Suppresses notifications for one alert rule until the specified expiry.",
    prepare: silenceFlags,
    read: () => ({ method: "GET", path: "/api/v1/admin/ops/alert-rules" }),
    write: (target) => ({
      method: "POST",
      path: "/api/v1/admin/ops/alert-silences",
      body: target.body,
    }),
    capture: (rules, target) => ({
      rule: pick(requireRule(rules, target.ruleId), [
        "id",
        "name",
        "enabled",
        "severity",
        "metric_type",
      ]),
      body: target.body,
    }),
    eligible: (data) => data && data.rule && data.rule.enabled !== false,
    verify: (rules, writeResult, target) => Boolean(
      writeResult
      && writeResult.id
      && requireRule(rules, target.ruleId),
    ),
    limitedVerification: true,
  },
  "clear-account-error": accountRecoverySpec(
    "clear-error",
    (data) => Boolean(data && data.error_message),
    (data) => data && data.error_message === "",
    "Clears one account error and invalidates its cached token when applicable.",
  ),
  "clear-account-rate-limit": accountRecoverySpec(
    "clear-rate-limit",
    (data) => Boolean(
      data
      && (data.rate_limited_at || data.rate_limit_reset_at || data.temp_unschedulable_until),
    ),
    (data) => Boolean(
      data
      && data.rate_limited_at == null
      && data.rate_limit_reset_at == null
      && data.temp_unschedulable_until == null
    ),
    "Clears one account's rate-limit and temporary-unschedulable runtime state.",
  ),
  "recover-account-state": accountRecoverySpec(
    "recover-state",
    (data) => Boolean(
      data
      && (
        data.error_message
        || data.rate_limited_at
        || data.rate_limit_reset_at
        || data.overload_until
        || data.temp_unschedulable_until
      )
    ),
    (data) => Boolean(
      data
      && data.error_message === ""
      && data.rate_limited_at == null
      && data.rate_limit_reset_at == null
      && data.overload_until == null
      && data.temp_unschedulable_until == null
    ),
    "Clears all recoverable runtime blocks for one account and invalidates its token cache.",
  ),
});

function stableStringify(value) {
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
      .join(",")}}`;
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
  return crypto
    .createHmac("sha256", key)
    .update(stableStringify(planPayload(plan)))
    .digest("hex");
}

function safeEqual(left, right) {
  const a = Buffer.from(String(left));
  const b = Buffer.from(String(right));
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}

function resolveStateRoot(stateDir, env = process.env) {
  if (stateDir) return path.resolve(stateDir);
  if (env.SUB2API_OPS_STATE_DIR) return path.resolve(env.SUB2API_OPS_STATE_DIR);
  if (env.XDG_STATE_HOME) return path.resolve(env.XDG_STATE_HOME, "sub2api-ops");
  return path.resolve(os.homedir(), ".local", "state", "sub2api-ops");
}

function ensurePrivateDir(directory) {
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  fs.chmodSync(directory, 0o700);
}

function readOrCreateSigningKey(root) {
  ensurePrivateDir(root);
  const keyPath = path.join(root, "hmac.key");
  try {
    fs.writeFileSync(keyPath, crypto.randomBytes(32), { flag: "wx", mode: 0o600 });
  } catch (error) {
    if (error.code !== "EEXIST") throw error;
  }
  fs.chmodSync(keyPath, 0o600);
  return fs.readFileSync(keyPath);
}

function atomicWritePlan(planFile, plan) {
  const temporary = `${planFile}.${crypto.randomBytes(6).toString("hex")}.tmp`;
  fs.writeFileSync(temporary, `${JSON.stringify(plan, null, 2)}\n`, {
    flag: "wx",
    mode: 0o600,
  });
  fs.renameSync(temporary, planFile);
  fs.chmodSync(planFile, 0o600);
}

async function apiRequest(api, request) {
  return request.body === null
    ? api.request(request.method, request.path)
    : api.request(request.method, request.path, request.body);
}

async function createMutationPlan({
  action,
  flags,
  api,
  stateDir,
  now = () => new Date(),
  env = process.env,
}) {
  const spec = ACTION_SPECS[action];
  if (!spec) throw new CliError(`unknown mutation action: ${action}`);
  const createdAt = now();
  const target = spec.prepare(flags, createdAt);
  const readRequest = spec.read(target);
  const before = await apiRequest(api, readRequest);
  const observed = spec.capture(before, target);
  if (!spec.eligible(observed, target)) {
    throw new CliError(`target is not eligible for ${action}`, 3);
  }

  const request = spec.write(target);
  const token = crypto.randomBytes(18).toString("base64url");
  const root = resolveStateRoot(stateDir, env);
  const plansDir = path.join(root, "plans");
  ensurePrivateDir(plansDir);
  const key = readOrCreateSigningKey(root);
  const plan = {
    version: PLAN_VERSION,
    id: crypto.randomUUID(),
    action,
    request,
    precondition: { target, observed },
    verification: readRequest,
    risk: spec.risk,
    created_at: createdAt.toISOString(),
    expires_at: new Date(createdAt.getTime() + PLAN_TTL_MS).toISOString(),
    consumed_at: null,
    token_digest: sha256(token),
  };
  plan.signature = signPlan(plan, key);
  const planFile = path.join(plansDir, `${plan.id}.json`);
  atomicWritePlan(planFile, plan);
  return {
    plan_file: planFile,
    confirmation_token: token,
    preview: {
      plan_id: plan.id,
      action,
      target: redactValue(target),
      before: observed,
      change: request,
      risk: spec.risk,
      expires_at: plan.expires_at,
      verification: readRequest,
    },
  };
}

function resolvePlanLocation(planFile, stateDir, env) {
  const root = resolveStateRoot(stateDir, env);
  const plansDir = path.resolve(root, "plans");
  const resolvedPlan = path.resolve(String(planFile || ""));
  if (!resolvedPlan.startsWith(`${plansDir}${path.sep}`)) {
    throw new CliError("plan file must be inside the Sub2API Ops plans directory", 2);
  }
  return { root, resolvedPlan };
}

function readVerifiedPlan(planFile, stateDir, env) {
  const { root, resolvedPlan } = resolvePlanLocation(planFile, stateDir, env);
  let plan;
  try {
    plan = JSON.parse(fs.readFileSync(resolvedPlan, "utf8"));
  } catch (error) {
    throw new CliError(`cannot read plan file: ${error.message}`, 2);
  }
  const key = readOrCreateSigningKey(root);
  const expected = signPlan(plan, key);
  if (!safeEqual(plan.signature, expected)) throw new CliError("plan signature is invalid", 2);
  return { key, plan, planFile: resolvedPlan };
}

async function executeMutationPlan({
  planFile,
  confirmationToken,
  api,
  stateDir,
  now = () => new Date(),
  env = process.env,
}) {
  const { resolvedPlan } = resolvePlanLocation(planFile, stateDir, env);
  const lockFile = `${resolvedPlan}.lock`;
  let lock;
  try {
    lock = fs.openSync(lockFile, "wx", 0o600);
  } catch (error) {
    if (error.code === "EEXIST") {
      throw new CliError("plan execution is already in progress", 2);
    }
    throw error;
  }

  try {
    const loaded = readVerifiedPlan(resolvedPlan, stateDir, env);
    const { key, plan } = loaded;
    if (plan.version !== PLAN_VERSION) throw new CliError("plan version is unsupported", 2);
    const spec = ACTION_SPECS[plan.action];
    if (!spec) throw new CliError("plan action is not allow-listed", 2);
    if (plan.consumed_at) throw new CliError("plan has already been consumed", 2);
    const currentTime = now();
    if (currentTime.getTime() >= new Date(plan.expires_at).getTime()) {
      throw new CliError("plan has expired", 2);
    }
    if (!confirmationToken || !safeEqual(sha256(confirmationToken), plan.token_digest)) {
      throw new CliError("confirmation token is invalid", 2);
    }

    const target = plan.precondition.target;
    const canonicalRequest = spec.write(target);
    const canonicalVerification = spec.read(target);
    if (
      stableStringify(plan.request) !== stableStringify(canonicalRequest)
      || stableStringify(plan.verification) !== stableStringify(canonicalVerification)
    ) {
      throw new CliError("plan request does not match action catalog", 2);
    }
    const current = await apiRequest(api, plan.verification);
    const currentObserved = spec.capture(current, target);
    if (stableStringify(currentObserved) !== stableStringify(plan.precondition.observed)) {
      throw new CliError("target state changed after the plan was created", 3);
    }

    plan.consumed_at = currentTime.toISOString();
    plan.signature = signPlan(plan, key);
    atomicWritePlan(loaded.planFile, plan);

    const writeResult = await apiRequest(api, plan.request);
    const after = await apiRequest(api, plan.verification);
    if (!spec.verify(after, writeResult, target)) {
      throw new CliError(`verification failed for ${plan.action}`);
    }
    return {
      plan_id: plan.id,
      action: plan.action,
      write_result: redactValue(writeResult),
      verification: {
        ok: true,
        limited: Boolean(spec.limitedVerification),
        observed: redactValue(spec.capture(after, target)),
      },
    };
  } finally {
    fs.closeSync(lock);
    fs.unlinkSync(lockFile);
  }
}

async function runReadCommand(args, api) {
  const [command, ...positional] = args.positional;
  const request = buildReadRequest(command, positional, args.flags);
  return redactValue(await api.request(request.method, request.path));
}

function usage() {
  return `Usage:
  sub2api-ops.js snapshot [--time-range 1h] [--platform NAME] [--group-id ID]
  sub2api-ops.js availability [--platform NAME] [--group-id ID]
  sub2api-ops.js concurrency [--platform NAME] [--group-id ID]
  sub2api-ops.js alerts [--status STATUS] [--severity P0|P1|P2|P3]
  sub2api-ops.js alert-event <id>
  sub2api-ops.js errors [filters]
  sub2api-ops.js error <id>
  sub2api-ops.js upstream-errors [filters]
  sub2api-ops.js requests [filters]
  sub2api-ops.js system-logs [filters]
  sub2api-ops.js system-log-health
  sub2api-ops.js ingress-rejections [filters]
  sub2api-ops.js auth-cache-health
  sub2api-ops.js plan <approved-action> [flags]
  sub2api-ops.js execute --plan <file> --confirm <token>
`;
}

async function main(argv = process.argv.slice(2), env = process.env) {
  const args = parseArgs(argv);
  if (args.flags.help || args.positional.length === 0) {
    process.stdout.write(usage());
    return;
  }
  if (args.positional[0] === "plan") {
    const action = args.positional[1];
    if (!ACTION_SPECS[action]) throw new CliError(`unknown mutation action: ${action}`);
    const api = createApiClient({
      baseUrl: env.SUB2API_BASE_URL,
      adminApiKey: env.SUB2API_ADMIN_API_KEY,
      jwt: env.SUB2API_JWT,
    });
    const result = await createMutationPlan({
      action,
      flags: args.flags,
      api,
      env,
    });
    process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
    return;
  }
  if (args.positional[0] === "execute") {
    if (!args.flags.plan || !args.flags.confirm) {
      throw new CliError("execute requires --plan <file> and --confirm <token>", 2);
    }
    const api = createApiClient({
      baseUrl: env.SUB2API_BASE_URL,
      adminApiKey: env.SUB2API_ADMIN_API_KEY,
      jwt: env.SUB2API_JWT,
    });
    const result = await executeMutationPlan({
      planFile: args.flags.plan,
      confirmationToken: args.flags.confirm,
      api,
      env,
    });
    process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
    return;
  }
  const api = createApiClient({
    baseUrl: env.SUB2API_BASE_URL,
    adminApiKey: env.SUB2API_ADMIN_API_KEY,
    jwt: env.SUB2API_JWT,
  });
  const result = await runReadCommand(args, api);
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

if (require.main === module) {
  main().catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = error.exitCode || 1;
  });
}

module.exports = {
  CliError,
  ACTION_SPECS,
  buildReadRequest,
  createApiClient,
  createMutationPlan,
  executeMutationPlan,
  main,
  parseArgs,
  redactValue,
  runReadCommand,
  usage,
};
