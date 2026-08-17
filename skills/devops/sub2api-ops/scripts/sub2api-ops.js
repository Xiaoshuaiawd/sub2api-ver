#!/usr/bin/env node
"use strict";

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
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new CliError(`${name} must be a positive integer`);
  }
  return parsed;
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
    throw new CliError("--time-range must be 1-30 followed by m, h, or d");
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
        ...scoped(),
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
  if (args.positional[0] === "plan" || args.positional[0] === "execute") {
    throw new CliError("mutation planning is not available in this build");
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
  buildReadRequest,
  createApiClient,
  main,
  parseArgs,
  redactValue,
  runReadCommand,
  usage,
};
