# 消息存储设计

## 目标

为管理员使用日志提供客户端原始请求体和最终客户端响应体的查看能力。正文不进入现有 `usage_logs` 主表，正文存储失败不得阻断 API 请求、计费或现有使用日志写入。

## 约束与默认值

- 存储后端仅使用 PostgreSQL，不依赖 S3。
- 正文默认保留 7 天，管理员可配置 1-30 天。
- 默认每个方向（请求/响应）最大捕获 16 MiB；超过上限标记 `too_large` 并停止捕获。该限制必须可配置，不能以无上限内存换取“完整”。
- 客户端请求体在任何模型规范化、协议转换或上游改写之前捕获。
- 客户端响应体捕获网关最终写出的字节，包含流式 SSE 和错误响应；客户端提前断开时标记 `partial`，不伪装为完整响应。
- 采集、压缩、正文写入使用独立有界队列和并发上限，不复用计费关键队列。
- 队列满、临时盘满、压缩失败或 PostgreSQL 正文写入失败只产生失败状态和指标，继续放行 API 请求。

## 方案选择

### 采用：PostgreSQL 分区表 + 本机临时文件 + 异步正文 worker

使用小型 `usage_message_captures` 元数据表记录每个使用日志的请求/响应状态、大小、哈希、错误和过期时间；使用按天分区的 `usage_message_bodies` 表保存 Zstd 压缩后的 `BYTEA` 正文。请求路径只保留受全局字节预算约束的内存缓冲，超出内存阈值后溢写本机临时文件，终态事件只携带文件路径和元数据。

`usage_message_captures` 在终态使用日志事务中以 `pending` 状态创建，保证管理员能区分“尚未完成”“存储失败”和“已过期”。正文 worker 成功写入后更新为 `available`，失败时更新为 `failed`；超过 30 分钟的残留 `pending` 由后台巡检改成 `failed/stale_pending`。

### 不采用：直接给 `usage_logs` 增加大字段

这会让列表、统计、备份、VACUUM 和复制都受到正文 TOAST 数据影响，并把正文写入失败耦合到计费事务。

### 不采用：PostgreSQL Large Object

Large Object 的生命周期、孤儿对象清理和备份恢复复杂度高于分区 `BYTEA`，不适合当前单体网关的第一版。

### 不采用：本机文件作为最终存储

本机文件可以作为短期 spill buffer，但多实例部署、节点迁移、故障恢复和管理员读取都会失去一致性，因此不能作为最终数据源。

## 数据模型

### `usage_message_captures`

一条使用日志一行，保存小字段，不保存正文：

- `usage_log_id`、`request_id`
- `request_state`、`response_state`
- `request_raw_bytes`、`response_raw_bytes`
- `request_stored_bytes`、`response_stored_bytes`
- `request_sha256`、`response_sha256`
- `compression`、`error_code`、`error_message`
- `expires_at`、`created_at`、`updated_at`

状态集合：`pending`、`available`、`failed`、`partial`、`too_large`、`expired`、`disabled`。

### `usage_message_bodies`

按 `created_at` 日分区，每个使用日志最多两行：

- `usage_log_id`
- `body_type`（`request` 或 `response`）
- `payload_zstd BYTEA`
- `raw_bytes`、`stored_bytes`
- `created_at`

仅建立按 `usage_log_id` 的查询索引；不做正文全文索引。预压缩数据设置为 `EXTERNAL`，避免 PostgreSQL 对 Zstd 结果重复 TOAST 压缩。

## 请求生命周期

1. 网关认证后创建 capture session，读取原始请求体时写入 capture；业务解析继续复用原有字节，不额外反序列化。
2. 用统一的客户端响应 writer 记录所有写出字节；同步响应和流式响应走同一 capture 接口。
3. 请求完成时生成 capture artifact：内存 bytes 或临时文件路径、原始大小、SHA-256、完成状态。
4. 现有使用日志终态写入事务同时创建 `usage_message_captures(pending)`。
5. 正文 worker 从 artifact 读取并 Zstd 压缩，按请求和响应分别写入正文分区表，再更新 capture 状态。
6. worker 成功或失败后删除临时文件；文件删除失败只记录运维指标。

## 并发与容量保护

- 全局内存预算默认 512 MiB，按请求申请字节令牌；无法申请时直接 spill 到临时文件。
- 临时目录设置独立磁盘配额和单文件上限；磁盘剩余空间低于阈值时新 capture 标记 `failed/spool_full`。
- 正文 worker 使用独立并发上限，默认 32；单次写入不做超大批量事务，避免大事务和长 WAL 峰值。
- 正文数据库连接池独立于业务连接池，默认最大 16 个连接。
- 详情接口默认单次只取一个使用日志的两个正文，限制解压后的返回大小，使用流式响应或分页下载，防止管理员浏览器和 API 网关内存暴涨。
- 指标至少包括队列深度、capture 内存字节、spill 文件字节、pending 年龄、压缩耗时、写入耗时、失败数、too-large 数、过期数和 PostgreSQL 磁盘/WAL 水位。

## 保留与清理

管理员修改保留天数只影响后续清理，不改变已写入正文的 `expires_at`。后台定时任务每小时：

1. 根据 `expires_at` 删除或直接移除已过期日分区。
2. 对无法整分区删除的历史数据执行带 `LIMIT` 的批量删除。
3. 删除残留临时文件和超过 30 分钟未更新的 `pending` 元数据。

## 管理端交互

使用日志列表增加消息状态列，点击行打开详情抽屉，详情接口默认按需加载正文：

- 请求体 / 响应体两个 tab
- JSON 格式化与原始文本模式
- 复制、下载、大小和状态信息
- `failed`、`partial`、`too_large`、`expired` 明确显示原因
- 正文不进入列表 API，避免分页查询读取 TOAST
- 查看正文写入现有管理员审计日志，并要求现有管理员权限/二次验证策略

## 验证标准

- 同步与流式请求都能看到客户端原始请求和最终客户端响应。
- 上游转换后的请求不覆盖客户端原始请求。
- 队列满、正文数据库失败、临时盘满和超限均不改变 API 状态码或计费结果。
- 两个正文可以独立成功或失败，详情页不会因为一侧失败而隐藏另一侧。
- 7 天清理不会扫描或锁住整张 `usage_logs` 表。
- 在压测中，正文 worker 饱和时网关 P99 延迟和错误率不出现级联增长。
