# OpenAI Chat/Responses Prompt Cache 命中率优化设计

## 背景与根因

当前实现已经能在下游没有提供 `prompt_cache_key` 时，将 Redis 中的 UUIDv7 注入 OpenAI 出站请求，但仍存在四个影响命中率的结构性问题：

1. 自动 UUID 同时被用作本地账号调度的 `sessionHash` 和上游 `prompt_cache_key`。前者应该按会话粘住账号，后者应该把相同的可复用前缀路由到相同缓存分片，两者职责不同。
2. 自动 UUID 优先按 session/conversation ID 或“首条用户消息”建库。不同会话即使具有完全相同的 system/developer、tools 和 schema，也会得到不同 key，无法共享上游稳定前缀缓存。
3. Chat Completions 与 Responses 分别从 `messages` 和 `input` 派生内容，语义相同的请求不能形成同一个稳定前缀指纹。
4. 现有 UUID 固定窗口只有 5 分钟；同时缓存率统计包含大量未达到上游最低缓存长度的请求，导致观测值不能准确表示可缓存请求的实际命中情况。

OpenAI Prompt Caching 要求精确前缀匹配。`prompt_cache_key` 只帮助相同前缀路由到同一缓存位置，不能让不同前缀互相命中。GPT-5.6 及以后模型的最低可缓存前缀是 1,024 tokens，默认缓存 TTL 为 30 分钟，并支持显式缓存断点；更早模型的最低长度随模型不同为 1,024 到 2,048 tokens。官方还建议单个 cache key 的请求速率保持在约 15 RPM 以内。

官方参考：<https://developers.openai.com/api/docs/guides/prompt-caching>

## 目标

1. `/v1/chat/completions` 与 `/v1/responses` 共用同一套稳定前缀规范化和指纹算法。
2. 本地账号粘性与上游缓存路由彻底分离，自动 cache key 不再改变本地 `sessionHash`。
3. 相同下游租户、最终模型族和稳定前缀的独立会话可以复用少量 UUIDv7 缓存身份。
4. 通过稳定分片避免单一 `prompt_cache_key` 过热，默认 4 个分片。
5. Redis 身份窗口默认固定 30 分钟，从首次创建开始计时，命中不续期；TTL 和分片数可配置。
6. 对确认支持该协议的 GPT-5.6+ Responses 上游，在稳定前缀末尾注入显式断点。
7. 增加不记录明文 Prompt/会话 ID 的诊断字段，并单独统计“具备缓存资格的请求”命中率。
8. Redis、UUID 或可选断点注入失败时 fail-open，不阻塞模型请求。

## 非目标

- 不缓存模型响应或完整 Prompt。
- 不进行相似度、模糊或最长前缀检索。
- 不改变客户端显式 `prompt_cache_key` 的值和优先级。
- 不改变 Grok、Anthropic、Gemini 的缓存策略。
- 不把普通 `id`、request/message/item/tool-call ID 当作稳定会话 ID。
- 不在本次改动中新增前端设置页面或重写 Channel Monitor 现有 token 口径。
- 不承诺短 Prompt、动态前缀或上游未写入缓存时一定命中。

## 核心架构

请求同时维护两个相互独立的身份：

```text
session_identity
  来源：客户端会话信号；无信号时使用现有内容回退
  用途：本地 OAuth/API Key 账号粘性调度

cache_identity
  来源：canonical_prefix_hash + deterministic_shard
  用途：上游 prompt_cache_key
```

`session_identity` 继续由现有 `GenerateSessionHash` 生成，并原样传给选号、粘性绑定和 failover。删除“自动 UUID 覆盖 sessionHash”的逻辑。

`cache_identity` 只负责出站缓存路由。它在账号选定后，根据最终映射后的模型族、统一稳定前缀和分片号查询/创建 Redis UUIDv7。这样账号 failover 到不同最终模型时会重新解析正确的缓存身份，但不会改变本地会话粘性。

客户端显式提供非空 `prompt_cache_key` 时，继续原样用于出站，不访问自动身份库；本地调度仍可按现有兼容语义把显式 key 当作会话信号。

## 会话信号兼容

在不接受普通 `id` 的前提下，扩充明确的会话字段：

- Headers：现有 `session_id`、`conversation_id`、OpenCode/CodeBuddy 会话头、`X-Claude-Code-Session-Id`。
- Body：`session_id`、`conversation_id`、`thread_id`、Responses `conversation`/`conversation.id`。
- Metadata：`metadata.session_id`、`metadata.conversation_id`、`metadata.thread_id`、`client_metadata.session_id`、`client_metadata.conversation_id`、`client_metadata.thread_id`。
- 嵌套 JSON 字符串：现有 `metadata.user_id` 中的 session/conversation/thread 字段。
- Responses 链：合法 `previous_response_id` 仅接受 `resp_*`，通过已有 response alias 找回原会话缓存身份。

这些字段只增强会话连续性和分片稳定性，不直接成为共享稳定前缀指纹。原始值不写日志或 Redis key。

正常的 Chat Completions → Responses 转换应保留合法的 `previous_response_id` 扩展字段，使支持该字段的兼容客户端不会在转换时丢失连续链。最终上游不支持时仍沿用现有能力清理逻辑。

## 统一稳定前缀

新增一个与入站协议无关的 canonical prefix 结构。Chat 的 `messages`/`functions`/`response_format` 与 Responses 的 `input`/`tools`/`text.format` 先归一化，再进行确定性 JSON 编码和 SHA-256。

纳入指纹的字段：

- 最终映射后的模型族；日期后缀和兼容别名按现有模型规范化规则收敛，但不同计费/能力模型不合并。
- 按原始顺序保留的 system/developer 指令，角色不可互换。
- `tools` 以及 legacy `functions`，统一为 Responses function tool 形状；数组顺序保留，对象键稳定排序。
- Chat `response_format` 与 Responses `text.format` 的统一 schema。
- 会改变稳定渲染前缀的 reasoning、tool choice、parallel tool 等稳定选项。

明确排除：

- user、assistant、tool output 和普通会话历史。
- session/conversation/response/request/message/item/tool-call ID。
- 时间戳、动态 metadata、stream、超时、service tier 和其他传输选项。
- 客户端显式 `prompt_cache_key`。

文本字符串和等价的单一 text content block 归一为同一表示；空数组、空对象和缺失字段按各自 API 的等价语义收敛。数组顺序保持不变，防止工具或指令顺序变化被错误合并。

如果请求没有 system/developer、tools/functions、response schema 等可复用稳定前缀，则退回会话级 cache source。该回退只在同一会话内复用，不把租户内所有无前缀请求合并到同一个 key。

## 分片与 Redis 数据模型

默认分片数 `N=4`，只允许 `1`、`4`、`8`、`16`：

```text
shard = hash(session_identity) % N
cache_source = canonical_prefix_hash + ":shard:" + shard
```

没有显式会话信号但存在现有内容回退时，使用该内容生成的 `sessionHash` 计算稳定分片；没有任何会话来源时使用 0 号分片。分片数变更会自然形成新的 Redis 命名空间，避免新旧分片布局混用。

主记录升级为新版本，旧 v1 数据等待自然过期，不做迁移：

```text
openai:prompt_cache_identity:v2:{apiKeyID}:{modelHash}:n{N}:s{shard}:{prefixHash}
  -> 019b2745-7252-73f1-93da-ede850329211
  PX 1800000
```

无稳定前缀时使用单独的会话回退命名空间：

```text
openai:prompt_cache_identity:v2:{apiKeyID}:{modelHash}:session:{sessionHash}
  -> <UUIDv7>
```

Response alias 同样升级为 v2，并继承主记录剩余 TTL。所有 key 组件使用 SHA-256 或已散列 sessionHash，不保存明文 Prompt、会话 ID 或 response ID。

现有 Lua 原子解析语义保持不变：命中只返回 value 和 `PTTL`，不调用 `EXPIRE`；未命中以调用方 UUIDv7 和完整 TTL 创建记录。并发首次请求只产生一个最终有效 value。

## 配置

在 `gateway.openai_prompt_cache` 下新增后端配置：

```yaml
gateway:
  openai_prompt_cache:
    identity_ttl_seconds: 1800
    shard_count: 4
    explicit_breakpoints_enabled: true
```

约束：

- `identity_ttl_seconds` 默认 1800，允许 300 到 86400；非法值回退默认值。
- `shard_count` 默认 4，只接受 1/4/8/16；非法值回退 4。
- `explicit_breakpoints_enabled` 默认开启，但还必须通过下述模型和账号能力门控。
- 直接构造零值 Config 的测试也使用安全默认值，不依赖配置加载器是否执行。

## GPT-5.6+ 显式断点

显式断点只在同时满足以下条件时注入：

1. 最终模型属于 GPT-5.6 或以后且已知支持显式断点的模型族。
2. 最终出站是 OpenAI Responses 形状。
3. 选中 OpenAI API Key 账号，走直接支持标准 Responses 字段的路径。
4. 配置开关开启，且请求存在可标记的 system/developer text content block。
5. 客户端没有自行提供 `prompt_cache_options` 或 `prompt_cache_breakpoint`。

注入位置是稳定 system/developer 前缀最后一个受支持的 text block：

```json
{
  "prompt_cache_options": {"mode": "explicit"},
  "input": [{
    "type": "message",
    "role": "developer",
    "content": [{
      "type": "input_text",
      "text": "stable instructions",
      "prompt_cache_breakpoint": {"mode": "explicit"}
    }]
  }]
}
```

使用 `explicit` 模式可避免每个动态用户后缀都产生额外 cache write。若请求只有顶层字符串 `instructions`，不为断点而改变其语义形状；官方明确顶层 `instructions` 不能直接包含断点，此时仅注入 cache key，不注入断点。

OAuth/Codex internal 上游当前会清理或拒绝 `prompt_cache_options`，因此绝不注入断点字段。非 GPT-5.6、Chat Completions 直连上游、兼容供应商和能力未知账号同样保持原请求，只使用安全的 `prompt_cache_key`。

## 请求数据流

1. Handler 鉴权并解析请求，生成独立的 `sessionHash`。
2. 使用 `sessionHash` 选号；自动缓存 UUID 不参与选号。
3. 账号选定后计算最终映射模型和 canonical prefix。
4. 客户端有显式 key 时直接沿用；否则按 prefix+shard 或 session fallback 查询/创建 UUIDv7。
5. 将一次 failover 尝试对应的身份暂存在请求上下文；相同最终模型和 source 重试时复用，模型/source 变化时重新解析。
6. OpenAI OAuth 和 API Key 出站都注入 `prompt_cache_key`。OAuth 继续让现有隔离逻辑派生 `session_id`/`conversation_id`，但不注入显式断点。
7. 能力满足时，在最终 Responses body 注入 GPT-5.6+ 显式断点。
8. 成功响应返回合法 `resp_*` 时，绑定 response alias，TTL 只使用主身份剩余时间。
9. 记录无敏感信息的缓存决策和实际 usage；本地粘性绑定仍只绑定原 `sessionHash -> accountID`。

## 可观测性与缓存率口径

每次成功请求增加结构化诊断字段：

- `ingress_endpoint`、`upstream_endpoint`、最终模型族。
- `cache_identity_reason`、`source_kind`、`redis_hit`、`ttl_ms`。
- `prefix_sha256`、`identity_sha256`、`shard_count`、`shard_index`。
- `explicit_breakpoint_injected` 和未注入原因。
- `input_tokens`、`cached_tokens`、`cache_write_tokens`。

不记录原始 Prompt、schema、tool、session/conversation/response ID 或自动 UUID 明文。

保留 Channel Monitor 当前 `cached_tokens / all_prompt_tokens` 指标，避免静默改变既有业务语义；新增可缓存请求口径：

- GPT-5.6+：`input_tokens >= 1024`。
- 更早或能力未知模型：保守使用 `input_tokens >= 2048`。
- `eligible_request_count`：满足阈值且到达支持 Prompt Caching 的 OpenAI 上游的成功请求数。
- `eligible_hit_count`：eligible 且 `cached_tokens > 0` 的请求数。
- `eligible_cache_hit_rate = eligible_hit_count / eligible_request_count`。
- 同时记录 eligible 请求的 cached token ratio，区分“请求命中率”和“token 命中率”。

由于服务端不能在不引入模型 tokenizer 的情况下精确计算断点前 token 数，eligible 是保守的请求级近似，日志和指标命名不得宣称它是上游精确资格判断。

## 错误与降级

- canonical prefix 失败：退回会话级 cache source；仍失败则不注入自动 key。
- UUIDv7 生成失败、Redis 超时/错误、Redis value 非法：记录 warning，跳过自动 key，正常转发。
- 断点找不到合法位置或注入失败：保留 `prompt_cache_key`，不注入断点。
- 上游明确因自动断点字段返回字段不支持错误：沿用现有受限重试机制，仅删除本次自动注入的断点字段后重试一次，并记录能力降级原因。
- response ID 为空、不是 `resp_*`、请求失败或身份已过期：不写 alias。
- 配置非法：启动/归一化时回退安全默认值并记录一次 warning，不让热路径失败。

## 测试策略

严格使用失败测试先行：

1. Canonicalizer unit：Chat/Responses 等价前缀得到同一 hash；用户消息变化不改变 hash；system/tool/schema/稳定选项变化会改变 hash；数组顺序保留；动态 metadata/ID 被排除。
2. Routing unit：自动 UUID 不再覆盖 sessionHash；相同 prefix 同 shard 复用；不同 shard/租户/最终模型隔离；无 prefix 时会话回退；显式 key 永远优先。
3. Redis/miniredis：v2 key、固定 TTL 不续期、过期换新 UUIDv7、并发首次创建唯一、alias 继承剩余 TTL。
4. Chat bridge：`messages` 转 Responses 后得到与原生 Responses 相同身份；合法 `previous_response_id` 不被转换层误丢弃。
5. Breakpoint unit：仅 GPT-5.6+ 直接 API Key Responses 注入；OAuth、旧模型、未知能力、顶层 instructions、客户端自管断点均不注入；字段拒绝时只移除自动字段重试。
6. Handler/service：Chat 和 Responses 的选号只使用 sessionHash；failover 同模型复用、不同最终模型重新解析；成功绑定 alias，失败不绑定。
7. Metrics unit：1024/2048 阈值边界、eligible hit/miss、短请求不进入 eligible 分母、现有 Channel Monitor 口径不变。
8. 回归：现有 OpenAI/Grok cache、Chat/Responses SSE、billing、OAuth/API Key 转发和 sticky session 测试全部通过。

## 发布与回滚

- v2 Redis key 与 v1 并存，发布无需数据迁移；v1 在原 5 分钟窗口后自然消失。
- 默认 TTL 30 分钟、4 分片。若观察到单 key RPM 过高，可调到 8/16；低流量环境可调到 1。
- 显式断点可独立关闭；关闭后仍保留双身份、统一前缀和自动 UUID。
- 完整回滚代码不会污染 v1 数据；v2 key 最长按配置 TTL 自动过期。

## 验收标准

1. 相同租户、最终模型和稳定前缀的 Chat/Responses 请求，在相同 shard 上得到同一 UUIDv7。
2. 不同 user 消息不会改变 canonical prefix；不同 system/tools/schema 会改变它。
3. 自动 UUID 不再影响本地账号选择；同一会话继续使用原有 sticky session。
4. 默认身份 TTL 从首次创建固定 30 分钟，命中不续期。
5. OpenAI OAuth 与 API Key 出站均携带自动 key；只有能力确认的 GPT-5.6+ API Key Responses 出站携带自动断点。
6. Redis/UUID/断点失败不使模型请求失败。
7. 可分别看到全部 token 缓存率与 eligible 请求缓存命中率，短请求不再稀释后者。
8. 相关 focused tests、`go test -tags unit ./internal/...`、race tests 和 `git diff --check` 通过。
