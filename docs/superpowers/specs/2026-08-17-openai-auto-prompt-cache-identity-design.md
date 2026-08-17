# OpenAI 自动 Prompt Cache 身份库设计

## 背景

原生 `/v1/responses` 请求在客户端未提供 `prompt_cache_key` 时，sub2api 已能从稳定请求内容生成 `sessionHash`，并通过现有 Redis `sticky_session` 记录把请求尽量粘到同一个上游账号。但是该内容身份没有被提升为上游 `prompt_cache_key`：OpenAI OAuth 出站只会在客户端显式提供 key 时设置隔离后的 `session_id` / `conversation_id`，OpenAI API Key 出站也不会自动补 key。

结果是本级账号粘性与下一级网关或 OpenAI Prompt Cache 路由脱节。尤其当 API Key 上游本身是另一个 OAuth 账号池网关时，缺少 `prompt_cache_key` 会使下一级网关无法稳定粘到同一 OAuth 账号。

## 目标

1. 客户端未提供 `prompt_cache_key` 时，为原生 Responses 请求解析稳定会话来源并生成 UUIDv7 格式的自动 key。
2. 使用 Redis 在首次创建后的固定 5 分钟内复用同一 UUID，命中不得续期。
3. OpenAI OAuth 与 OpenAI API Key 上游都在请求体中收到自动 `prompt_cache_key`。
4. OAuth 上游继续使用租户隔离后的 `session_id` / `conversation_id`。
5. 支持 `previous_response_id` 链：成功响应的 `response.id` 在剩余固定窗口内映射回同一 UUID。
6. 保留现有本地 `sessionHash -> accountID` 粘性调度，不保存原始 Prompt 或原始会话标识。
7. Redis 或 UUID 生成故障必须 fail-open，不得阻塞模型请求。

## 非目标

- 不缓存模型响应或完整 Prompt。
- 不进行相似度、模糊或最长前缀检索。
- 不改变 Grok 已有缓存身份逻辑。
- 不向 Anthropic、Gemini 或其他非 OpenAI 平台注入 OpenAI 字段。
- 不把 `request_id`、普通 `id`、message/item/tool-call ID 当作会话 ID。
- 不修改现有显式 `prompt_cache_key` 的优先级或 TTL 语义。

## 身份来源与优先级

自动解析仅在请求体缺少非空 `prompt_cache_key` 时执行。

1. 稳定显式会话标识：
   - 请求头 `session_id`、`conversation_id`；
   - 现有 OpenCode/CodeBuddy 会话头；
   - `X-Claude-Code-Session-Id`；
   - Responses `conversation` 字符串或 `conversation.id`；
   - 明确的 `metadata.session_id`、`metadata.conversation_id`；
   - 明确的 `client_metadata.session_id`、`client_metadata.thread_id`；
   - 兼容 metadata 中已经支持的嵌套 `session_id`。
2. 合法 `previous_response_id`（仅接受 `resp_*`）：先查 Redis response alias。
3. 稳定内容指纹：请求模型、instructions、system/developer、tools/functions 和第一条 user 消息。
4. 如果 alias 未命中、请求又只有增量内容，则允许合法 `resp_*` 自身作为新链的起点。
5. 仍无可靠来源时不生成自动 key，防止把租户内无关请求归到同一身份。

原始来源仅用于进程内计算 SHA-256；Redis key 中只包含散列值。

## Redis 数据模型

### 主身份记录

```text
openai:prompt_cache_identity:v1:{apiKeyID}:{modelHash}:{sourceHash}
  -> 019b2745-7252-73f1-93da-ede850329211
  EX 300
```

value 是标准 UUIDv7 字符串。`apiKeyID` 实现下游租户隔离，`modelHash` 防止不同模型共享缓存路由身份，`sourceHash` 不泄漏 Prompt 或会话标识。

### Response 别名记录

```text
openai:prompt_cache_response_alias:v1:{apiKeyID}:{modelHash}:{responseIDHash}
  -> 019b2745-7252-73f1-93da-ede850329211
  PX <主身份剩余毫秒数>
```

别名 TTL 只能等于主身份的剩余 TTL，禁止重新获得 300 秒。响应完成时间已经超过身份窗口时不写别名。

### 原子解析

使用 Lua 脚本在一个原子操作内：

1. `GET` 已有值；
2. 命中时读取 `PTTL` 并原样返回，绝不调用 `EXPIRE`；
3. 未命中时写入调用方生成的 UUIDv7，`PX 300000`；
4. 返回最终 value、剩余 TTL 和 hit/miss 标志。

并发首次请求只能有一个最终 UUID；所有并发调用返回相同结果。

## 请求数据流

1. Handler 完成鉴权并获得下游 `apiKeyID`、请求模型和原始请求体。
2. 如果客户端有显式 `prompt_cache_key`，跳过自动身份库并沿用当前逻辑。
3. 否则按优先级解析来源，查询/创建自动 UUID，并把 UUID 与绝对过期时间放入请求上下文。
4. 本地选号继续使用现有稳定 `sessionHash`。当普通 sessionHash 无法稳定表示 `previous_response_id` 增量链时，可以使用 alias 找回的 UUID 哈希作为兜底。
5. 每次 account failover 都从同一请求上下文读取同一个 UUID，不重复创建 Redis 记录。
6. OpenAI OAuth：请求体写入 UUID；现有出站构造使用 UUID 设置隔离后的 `session_id` / `conversation_id`。
7. OpenAI API Key（含 passthrough 到下一级 OAuth 池的 API Key 网关）：请求体同样写入 UUID；不额外强制非标准 session 头。
8. 成功转发返回合法 `OpenAIForwardResult.ResponseID` 时，将该 `resp_*` 写为 UUID 的别名，TTL 使用上下文中剩余的固定窗口。

## 平台和端点范围

- 开启：OpenAI 平台原生 `/v1/responses` HTTP 请求。
- 开启：该请求最终选中 OpenAI OAuth 或 OpenAI API Key 账号。
- 不覆盖：客户端显式 key。
- 不覆盖：`/responses/compact`，其已有独立 compact session 语义。
- 不覆盖：Grok、Anthropic-native、Gemini 和其他平台分支。
- Chat Completions 与 Messages 兼容路径保留现有自动派生逻辑，后续可单独迁移到同一 store，但不纳入本次范围。

## 错误处理

- UUIDv7 生成失败：记录结构化 warning，跳过自动注入。
- Redis miss：正常创建。
- Redis 读取、脚本或写入错误：记录结构化 warning，降级到当前请求路径，不返回 5xx。
- Redis 返回非法 UUID：视为缓存损坏；记录 warning，不把非法值发送上游。
- ResponseID 为空或不是合法 `resp_*`：不写 alias。
- 失败请求、failover 中间失败和未完成响应不写 alias。

## 可观测性

增加不含敏感明文的字段：

- `auto_prompt_cache_identity_source`；
- `auto_prompt_cache_identity_hit`；
- `auto_prompt_cache_identity_ttl_ms`；
- `auto_prompt_cache_identity_error`；
- 自动 UUID 的 SHA-256 日志值；
- alias hit/miss/bind 计数或结构化日志。

现有 usage 中的 `cached_tokens` 用于验证实际缓存收益；本功能不能保证短 Prompt 或非精确前缀一定产生 cache hit。

## 测试策略

1. Repository/miniredis：首次创建得到合法 UUIDv7、重复命中不续期、过期后得到新 UUID、并发首次创建唯一、租户/模型/来源隔离、alias 继承剩余 TTL。
2. Resolver unit：各稳定 ID 优先级、conversation 字符串/对象、metadata、previous alias、内容回退、非法/旋转 ID 排除。
3. Forward unit：OAuth body + session headers、API Key body、显式 key 不覆盖、compact/Grok 不介入、failover 复用同一 UUID。
4. Handler/service：成功 ResponseID 绑定 alias，失败不绑定，Redis 故障 fail-open。
5. 回归测试：现有 sessionHash 粘性、Chat/Messages compat、Grok cache 和 OpenAI passthrough 行为不退化。

## 安全与兼容性

- 不在 Redis key 或日志中保存原始 Prompt、session、conversation 或 response ID。
- UUID 仅在同一下游 API Key、模型和来源指纹窗口内复用。
- 显式客户端 key 永远优先。
- API Key 注入使用标准 Responses body 字段 `prompt_cache_key`，对下一级 sub2api/OAuth 池可直接参与调度。
- OpenAI 官方 Prompt Caching 仍要求精确前缀和最低可缓存长度；本设计优化路由身份，不改变上游缓存资格。
