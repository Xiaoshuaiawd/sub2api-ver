# OpenAI 高并发自适应调度与 429 风暴防护设计

## 背景

当前单机部署承载约 3000 个 OpenAI 账号，目标入口吞吐为 200 RPS，允许请求在调度层等待最多 1 秒。入口业务流量不能按用户做固定限流，但账号级派发必须受真实上游能力约束，否则无法阻止 429 风暴。

线上排查确认了以下问题：

- 账号 `load_factor` 为空时回退到 `concurrency=10000`。
- 负载率使用整数百分比，账号并发低于 100 时都显示为 `0%`。
- 默认每次只从 TopK=7 中选择；同分时账号 ID 成为稳定决胜条件。
- 每次选择会读取完整账号快照，并对大量候选账号查询 Redis 负载；429 换号会重复执行。
- Redis 连接约 3000，调度热路径产生约 270 条 Redis 命令/业务请求。
- 应用数据库连接上限为 256，PostgreSQL `max_connections=100`，429 状态持久化和配置读取会因连接耗尽失败。
- 部分并发槽位在 30 分钟后仍滞留，账号进入冷却后不再经过普通候选查询，清理延迟进一步放大可见并发。

`codex_7d_used_percent=100` 不能直接判定账号不可用。账号有积分时仍可继续调用，因此七天用量只能作为观测信息；实时 429、明确的积分不足响应、上游 reset 信息和主动积分探测才是运行时准入依据。

## 目标

- 单机持续承载 200 RPS，250 RPS 突发至少 60 秒。
- 调度等待 P99 不超过 1 秒，调度计算自身 P99 不超过 10 毫秒。
- 正常请求不再全量扫描账号池，不再全量读取 Redis 账号负载。
- 第一个可信 429 到达后，100 毫秒内停止向同一账号继续派发新请求。
- 单个请求最多尝试 2 个不同账号，换号总预算不超过 800 毫秒。
- 账号负载按真实在途数和动态窗口均衡，不使用整数百分比，不以账号 ID 作为同分固定决胜条件。
- Redis 正常热路径控制在每个业务请求 0-3 条命令，Redis CPU 在 200 RPS 压测下持续低于 60%。
- PostgreSQL 不参与请求级调度；数据库不可用时，本地 429 防护仍然生效。
- 兼容 HTTP、流式 Responses、Chat Completions 和现有粘性会话语义。

## 非目标

- 本方案不通过用户/API Key 固定 RPS 上限削减正常入口流量。
- 本方案不承诺在所有账号都被上游拒绝时仍返回成功。物理上游容量耗尽时，请求最多等待 1 秒后必须快速结束，不能无限占用内存和 goroutine。
- 本方案不把 Redis 改造成中央排序器，也不引入新的外部组件。
- 本方案不修改计费、积分扣减和账号导入流程；只消费它们提供的状态。

## 方案比较

### 方案 A：只修正负载率和 TopK

把整数百分比改为浮点数、提高 TopK、同分随机。改动小，可以缓解账号集中，但每次请求仍然读取全量快照和 Redis 负载，429 换号仍会放大 Redis，无法达到 200 RPS 的稳定目标。

### 方案 B：本机分片候选池 + 自适应窗口 + 本地 429 熔断

静态账号数据使用版本化 L1 快照，动态负载保存在本机分片状态表；选择使用 Power-of-Four 随机抽样，账号容量使用 AIMD 自动学习。Redis 只保留最终槽位租约、跨重启观测和异步状态同步。这是推荐方案，符合单机约束，热路径复杂度从 O(账号数) 降为近似 O(1)。

### 方案 C：Redis Lua 中央调度

由 Redis 统一维护候选、负载、冷却和选择。它适合多实例强一致场景，但当前 Redis 已经是瓶颈，且部署明确为单机。该方案会继续把业务吞吐绑定到单线程 Redis，不采用。

## 总体架构

```text
请求
  -> 路由键解析(group/platform/model/capability/transport)
  -> 不可变本机候选分片
  -> 本地运行时过滤(冷却/半开/动态窗口)
  -> Power-of-Four 抽样与精确评分
  -> 本地 CAS 预留 permit
  -> Redis 最终槽位租约(仅选中账号)
  -> 上游调用
       -> 成功: 释放 permit，更新 EWMA，缓慢增窗
       -> 429: 立即本地熔断，快速减窗，异步持久化
       -> 其他错误: 按错误分类处理，不统一触发 429 逻辑
  -> 释放 Redis 槽位和本地 permit，唤醒同分片等待者
```

核心原则是静态数据与动态数据分离：账号配置和路由能力走不可变快照；在途、窗口、429、延迟走本地原子状态。Redis 和 PostgreSQL 不再参与候选排序。

## 1. 版本化静态快照与候选分片

复用 `docs/plans/2026-08-20-scheduler-redis-bandwidth.md` 中的版本化 L1 快照设计：请求只读取 `ready` 和 `active version` 小键，版本未变化时复用本机解码后的账号快照，不重复执行全量 `ZRANGE + MGET`。

在快照之上构建不可变候选分片，路由键至少包含：

```text
group_id + platform + requested_model + transport + endpoint_capability + image_capability
```

分片值为账号 ID 数组和必要的静态评分字段。账号新增、删除、分组变化、模型映射变化或快照版本变化时重建受影响分片，并通过 `atomic.Pointer` 一次性替换。请求读取旧或新完整快照，不读取半更新状态。

运行时冷却、窗口和在途数不写入静态快照，避免每次 429 都触发 3000 账号快照重建。

## 2. 本机账号运行时状态

新增有上限的分片状态表，按账号 ID 分布到固定数量的 shard，避免单一全局锁。每个账号维护：

```text
inflight                 当前本机在途数
window                   当前允许派发窗口
increase_credit          AIMD 增窗累计 credit
consecutive_429          连续 429 数
cooldown_until           本地熔断截止时间
half_open_inflight       是否已有半开探测
latency_ewma_ms          完成延迟 EWMA
ttft_ewma_ms             首 token 延迟 EWMA
error_rate_ewma          可归责上游错误率 EWMA
last_selected_at         最近选择时间
last_429_at              最近 429 时间
generation               防止旧异步结果覆盖新状态
```

状态表只为当前快照存在或最近仍有在途/冷却的账号保留记录。后台清理无快照引用、无在途且超过空闲 TTL 的状态，防止导入和删除账号后内存无界增长。

服务重启时不从历史负载恢复窗口，所有账号从保守初值重新学习；已持久化且仍有效的明确 reset/cooldown 可以恢复。这样避免重启后用过期大窗口瞬间打出 429 风暴。

## 3. 动态并发窗口

账号的管理字段 `concurrency` 保留为硬安全上限，不再作为负载率分母。新增调度窗口：

```text
initial_window = 2
min_window = 1
max_window = min(account.concurrency, configured_max_window)
configured_max_window 默认 32
```

窗口使用 AIMD 调节。流式请求不能等待整条响应完成后才反馈容量，因此“成功准入”定义为收到成功响应头或首个有效 token；完整请求结束只负责更新最终延迟和错误 EWMA：

- 每次成功准入增加 `1/current_window` credit。
- credit 达到 1、最近 30 秒无 429且延迟 EWMA 未恶化时，窗口增加 1，并扣减 1 credit。这等价于每个窗口 RTT 约增加 1。
- 每个账号最多每 2 秒增加一次，避免低延迟账号瞬间冲高，同时保证约 90 秒长流量下能够从初始窗口 2 及时扩到稳定窗口。
- 收到账号级瞬时 429 时，窗口调整为 `max(1, ceil(window/2))`。
- 连续第二次 429 时窗口降为 1，并进入短冷却。
- 明确积分不足或上游提供 reset 时，按明确时间冷却，不进行短周期盲探测。
- 冷却结束进入 half-open，只允许 1 个探测请求；探测成功后窗口恢复为 2，再正常增窗。

负载比较不再计算整数百分比，而使用精确值：

```text
utilization = (inflight + reserved) / window
```

排序先比较是否可拿 permit，再比较 utilization、inflight、错误率、延迟和最近使用时间。`codex_7d_used_percent` 默认不参与准入和评分；只有在能确认“无积分兜底”且功能开关显式启用时，才允许作为弱权重。

## 4. Power-of-Four 公平选择

每次从目标候选分片随机抽取 4 个不同账号，并使用以下顺序选择：

1. 未冷却或合法 half-open。
2. `inflight < window`。
3. utilization 更低。
4. inflight 更低。
5. 错误率和延迟 EWMA 更低。
6. `last_selected_at` 更早。
7. 最终同分使用每请求随机种子，不使用账号 ID 固定决胜。

如果 4 个账号都无法获得 permit，再抽取一组，最多 2 轮。两轮仍失败时进入该路由分片的事件驱动等待队列，等待账号释放通知，而不是轮询 Redis。

粘性账号只作为弱偏好：未冷却且 utilization 低于 0.8 时可以直接尝试；达到 0.8、窗口已满、错误率超阈值或收到 429 时立即逃逸到普通抽样。

## 5. 429 分类、熔断和恢复

429 必须先影响本地调度，再异步写 Redis/PostgreSQL。数据库写失败不能让账号重新进入候选池。

429 分类如下：

| 类型 | 判定 | 动作 |
| --- | --- | --- |
| 明确 reset | header/body 含可信 reset | 冷却到 reset，窗口减半 |
| 明确积分不足 | 上游错误明确表示 credits/balance exhausted | 长冷却并触发积分状态刷新 |
| 瞬时账号限速 | 无长期额度语义的账号级 429 | 窗口减半，2s/5s/15s/30s 指数冷却 |
| 模型级限速 | 同账号仅特定模型 429 | 只阻断 account+model，不阻断其他模型 |
| 平台级风暴 | 同路由短窗大量账号同时 429 | 启动路由级保护，统一收缩窗口并限制换号 |

积分存在时，七天用量满不触发长期冷却。若响应无法区分周额度和积分限速，按瞬时 429 处理并半开探测，不使用固定 13 分 20 秒作为唯一默认值。

路由级风暴检测使用 5 秒环形窗口：至少 20 个上游尝试且 429 比例达到 20% 时进入 storm 状态。storm 状态下：

- 同路由所有账号窗口按 0.75 收缩，但不低于 1。
- 每个请求最多执行 1 次换号。
- 选择只从未在当前请求失败且未冷却的本地样本中进行，不重新全池扫描。
- 连续 10 秒低于 5% 429 后退出 storm，窗口通过 AIMD 缓慢恢复。

## 6. 请求级换号与等待预算

每个请求共享一个绝对 deadline：

```text
调度等待总预算      1000ms
账号选择计算预算      10ms
换号调度总预算       800ms
不同账号尝试上限        2
```

所有 Responses、Chat Completions 和兼容协议处理器复用同一个 `FailoverBudget`，不能各自维护不同的无限循环。800ms 约束的是收到可换号错误之后的再次调度、等待和退避，不包含前一次真实上游调用已经消耗的时间。一次请求失败过的账号记录在本地小集合中，后续抽样直接排除。

响应已经向客户端输出语义内容后，不再换号。客户请求错误，例如远程 URL 无法下载、无效参数或明确 4xx，不应触发账号换号。只有可归责账号/上游的错误才消耗换号预算。

入口不设置固定 RPS 拒绝规则，但等待队列必须有内存安全上限。建议全局等待者上限按 `目标RPS * 1秒 * 5倍突发系数` 初始化为 1000；达到上限只表示物理容量已耗尽，此时快速返回过载错误，不能无限创建 goroutine。

## 7. Redis 热路径收敛

Redis 不再提供候选排序和全池负载读取。正常请求只允许：

- 对最终选中账号执行一次原子槽位 acquire。
- 请求结束执行一次 release。
- 必要时写一个带 TTL 的 429 状态；该写入可以异步合并。

本地 permit 先获取，Redis acquire 失败时立即归还本地 permit，并在本地把该账号短暂标记为状态不一致，避免立即重复选择。

运行时指标采用 dirty-set 写回：只同步发生变化的账号，每 500ms-1s 批量写 Redis，用于管理页和重启观测，不全量同步 3000 个账号。Redis 不可用时本地调度继续运行，管理页标记数据降级。

连接池从当前 4096/256 调整到保守初值：

```text
pool_size = 512
min_idle_conns = 64
```

最终值由 200/250 RPS 压测决定。连接池等待 P99 低于 5ms 时不再扩大连接数，因为单线程 Redis 不会因 3000 个连接获得更高命令吞吐。

## 8. PostgreSQL 与异步持久化

应用连接池必须小于 PostgreSQL `max_connections=100`，推荐初值：

```text
max_open_conns = 64
max_idle_conns = 24
```

保留连接给 PostgreSQL 管理、迁移和其他容器。429、调度统计和使用记录走现有有界 worker/batch 模式；批量写入失败只记录指标和重试，不阻塞账号选择。

运行时配置读取缓存 TTL 从 5 秒提高到至少 30 秒，并保留最后一次成功值。数据库不可用时不得回退到会扩大 429 的危险默认值；应使用最后已知配置和本地熔断状态。

## 9. 槽位生命周期修正

本地 permit 由一次性 release guard 管理，请求正常完成、客户端取消、上游超时和 panic 恢复路径都必须归还。

Redis 槽位清理不能只依赖账号活跃索引的未来时间。后台任务每轮按成员分数清理已过期槽位，并让活跃索引 score 指向“最早成员到期时间”，不能因为同账号存在新请求而把旧成员再延长 30 分钟。

增加以下观测：

- release 被调用次数、重复调用次数和失败次数。
- `slot_age > configured_ttl` 数量。
- 本地 inflight 与 Redis 槽位差值。
- 请求完成后 5 秒仍存在的槽位数量。

Redis 只作为租约兜底；即使 Redis 残留，单机本地 permit 也必须立即恢复，避免清理滞后阻塞真实流量。

## 10. 指标与告警

新增调度指标：

```text
scheduler_select_duration_ms
scheduler_wait_duration_ms
scheduler_candidate_pool_size
scheduler_sample_rounds
scheduler_account_inflight
scheduler_account_window
scheduler_account_utilization
scheduler_healthy_account_count
scheduler_half_open_account_count
scheduler_cooldown_account_count
scheduler_429_rate_by_route
scheduler_storm_state
scheduler_failover_attempts
scheduler_local_redis_slot_drift
scheduler_snapshot_l1_hit_ratio
redis_commands_per_business_request
```

管理页的“账号并发”需要分别展示：真实本地 inflight、Redis 租约数、等待数、动态窗口和冷却状态，不能继续把所有值合成一个容易误解的并发数字。

关键告警：

- 5 秒 429 比例超过 20%。
- 调度等待 P99 超过 1 秒。
- 可调度账号少于路由分片账号总数的 20%。
- Top10 账号流量占比超过对应健康分片的 10%。
- Redis 命令超过 5 条/业务请求。
- 本地/Redis 槽位差值持续 30 秒大于 1%。

## 11. 配置与回滚

新增独立功能开关，默认先关闭：

```yaml
gateway:
  openai_scheduler:
    adaptive_enabled: false
    shadow_mode: true
    initial_window: 2
    min_window: 1
    max_window: 32
    increase_credit_per_success: reciprocal_window
    increase_interval: 2s
    no_429_increase_window: 30s
    sample_size: 4
    sample_rounds: 2
    sticky_escape_utilization: 0.8
    scheduling_wait_timeout: 1s
    failover_total_budget: 800ms
    max_distinct_account_attempts: 2
    storm_window: 5s
    storm_min_attempts: 20
    storm_429_ratio: 0.20
    storm_recovery_ratio: 0.05
```

`shadow_mode` 只计算新旧选择结果和预计窗口，不改变真实派发。正式开启后保留旧调度器作为单开关回滚路径。回滚只切换选择算法，不清空 Redis、不修改账号配置；本地窗口状态被丢弃，旧调度器立即接管。

## 12. 发布顺序

### 阶段 0：数据库和 Redis 止血

- PostgreSQL 连接池调整为 64/24。
- Redis 连接池调整为 512/64。
- 运行时配置读取保留最后一次成功值。
- 完成现有版本化 scheduler L1 计划。

### 阶段 1：精确负载和本地运行时状态

- 引入分片运行时状态表和本地 permit。
- 删除整数 LoadRate 对选择结果的决定作用。
- 同分随机化，粘性增加 0.8 utilization 逃逸。
- 保持原选择接口和 Redis 最终槽位 acquire/release。

### 阶段 2：自适应窗口和 429 熔断

- 接入 AIMD、429 分类、半开探测和本地优先熔断。
- 统一请求级 FailoverBudget。
- 防止客户 4xx 和 URL 下载错误触发账号风暴。

### 阶段 3：分片索引和 O(1) 抽样

- 构建不可变路由分片。
- Power-of-Four 替代全量评分和固定 TopK。
- 等待机制改为 release 通知，不轮询 Redis。

### 阶段 4：异步观测与清理修正

- dirty-set 批量同步运行时指标。
- 修正槽位过期索引。
- 管理页区分本地 inflight、Redis 租约、窗口和冷却。

## 13. 验收标准

在与生产账号规模相同的 3000 账号数据集上执行：

1. 200 RPS 持续 30 分钟，250 RPS 突发 60 秒。
2. 请求体按生产分布回放，包含 P99 约 14MiB 和少量 50MiB 请求。
3. 注入单账号 429、10% 账号 429、同模型 429 风暴、Redis 延迟和 PostgreSQL 不可用。

必须同时满足：

```text
调度计算 P99                 <= 10ms
调度等待 P99                 <= 1s
首个 429 后同账号新增派发停止  <= 100ms
每请求不同账号尝试 P99         <= 2
Redis 命令/业务请求            <= 5
Redis CPU 持续值               < 60%
Redis 连接数                   < 700
PostgreSQL 连接数              < 80
本地/Redis 槽位漂移             < 1%
过期槽位                       1 个清理周期内归零
goroutine 和 RSS               稳态不单调增长
```

公平性按每个路由分片单独计算，排除明确冷却和不兼容账号。10 分钟窗口内，健康账号覆盖率应达到 80% 以上，Top10 健康账号请求占比不超过 10%，P95/P50 账号在途比不超过 2。

## 14. 预计代码边界

主要修改范围：

- `backend/internal/service/openai_account_scheduler.go`：选择接口、精确负载、FailoverBudget 接入。
- `backend/internal/service/openai_adaptive_scheduler.go`：新建本地状态、AIMD、Power-of-Four 和 storm detector。
- `backend/internal/service/openai_account_runtime_block_fastpath.go`：429 分类与本地优先熔断。
- `backend/internal/handler/openai_gateway_handler.go`：统一请求级换号和等待预算。
- `backend/internal/repository/scheduler_cache.go`：复用已有版本化 L1 计划。
- `backend/internal/repository/concurrency_cache.go`：最终槽位租约、批量观测和过期索引修正。
- `backend/internal/config/config.go` 与 `deploy/config.example.yaml`：新增调度配置和安全默认值。
- `backend/internal/service/ops_metrics_collector.go`、Ops DTO/handler：新增调度指标。

测试应优先放在独立的新调度测试文件，避免继续扩大已有大型测试文件。所有算法使用可注入时钟和随机源，确保 429 冷却、半开、AIMD 和公平性测试可重复。

## 风险与缓解

- **本地状态与 Redis 不一致**：本地负责单机派发，Redis负责租约兜底和观测；持续监控 drift，并保留最终 Redis acquire。
- **重启后窗口归零导致短时保守**：初始窗口 2，成功后快速但有界恢复；安全优先于重启瞬间冲高。
- **随机选择破坏 previous_response 粘性**：合法粘性优先，达到利用率阈值或冷却时才逃逸。
- **积分状态不及时**：七天用量不做硬门；明确积分不足才长冷却，普通 429 允许短冷却半开。
- **所有账号真实不可用**：1 秒预算后快速失败并暴露健康账号数，禁止无限排队和无限换号。
- **一次改造范围过大**：按四阶段分别上线，每阶段都有独立开关和旧调度回滚路径。
