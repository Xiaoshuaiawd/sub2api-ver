# 分布式 OpenAI WARP 出口

本方案为每台 Sub2API 节点部署一个独立的 WARP SOCKS5 sidecar。它只影响
OpenAI、ChatGPT、Codex 和 OpenAI-compatible 账号的上游请求，不会修改宿主机、
Sub2API 容器、PostgreSQL、Redis 或其他模型流量的默认路由。

请求候选顺序如下：

```text
账号代理 -> 账号代理的备用链 -> 当前节点 WARP -> 可选直连
```

只有失败策略设为 `fallback_direct` 时才会出现最后的直连候选。默认的
`fail_closed` 会在所有代理候选发生连接、DNS、代理握手或 TLS 传输失败后返回错误；
上游已经返回 HTTP 状态码时不会切换候选。

## 为什么每个节点各自部署

三台 Sub2API 共用 PostgreSQL 和 Redis，因此管理后台保存的代理开关、地址和失败
策略会同步到所有实例。代理地址统一填写：

```text
socks5h://warp-proxy:1080
```

虽然配置值相同，但 `warp-proxy` 由每台机器自己的 Docker Compose DNS 解析，实际
连接的是当前节点本地 sidecar。这样没有跨机器单点，也不会把三台机器的 OpenAI
流量集中到一个 WARP 容器。

账号本身配置了代理时，账号代理仍然排在第一位。关闭“节点默认代理”后，系统恢复
为账号代理优先、无账号代理则直连的原有行为。

## 前置条件

- Linux 主机和 Docker Compose v2。
- 内核提供 TUN 设备 `/dev/net/tun`。
- 每台节点都保留 `docker-compose.standalone.yml` 和
  `docker-compose.warp.yml`。
- 三台节点使用相同的外部 PostgreSQL、Redis 和 Compose 项目配置，但分别执行部署
  命令。
- 可选的 WARP+ License 通过每台节点的 `.env` 设置 `WARP_LICENSE_KEY`；不要提交到
  Git。

Overlay 固定了镜像 tag 和 digest，只授予 `NET_ADMIN` 与 TUN 设备的 cgroup 访问权。
它不发布宿主机 `1080` 端口，也不读取 Sub2API、数据库或 Redis 的凭证。

## 三节点上线

先在每台节点的 `deploy` 目录验证合并后的 Compose：

```bash
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml config
```

然后逐台启动。Sub2API 不依赖 WARP health 才能启动，因此 WARP 异常不会阻止管理端
和非 OpenAI 流量提供服务。

```bash
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml up -d
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml ps
```

在每台节点验证本机 WARP 出口：

```bash
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml exec warp-proxy \
  curl --socks5-hostname 127.0.0.1:1080 https://www.cloudflare.com/cdn-cgi/trace
```

输出应包含 `warp=on` 或 `warp=plus`，并记录 `ip=` 供三台节点之间核对。

三台 sidecar 都正常后，只需在管理后台设置一次：

1. 打开“网关服务”中的“OpenAI 默认出口代理”。
2. 代理地址填写 `socks5h://warp-proxy:1080`。
3. 首次上线选择 `fail_closed`，保存后逐台观察当前节点状态、实例 ID、出口 IP 和检测
   时间。
4. 只有业务明确接受节点公网 IP 暴露风险时，才切换为 `fallback_direct`。

管理请求经负载均衡落到不同 Sub2API 实例时，状态行会显示本次响应实例的
`instance_id`。这是当前节点状态，不是三节点聚合视图。

## 排障

先检查 TUN 设备和内核模块：

```bash
ls -l /dev/net/tun
sudo modprobe tun
```

再检查容器状态和日志：

```bash
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml ps warp-proxy
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml logs --tail=200 warp-proxy
```

常见问题：

- `/dev/net/tun` 不存在：加载 `tun` 模块；若云厂商或虚拟化平台禁用 TUN，需要先在
  主机控制台开启。
- 出现 `operation not permitted`：确认 overlay 中仍有 `NET_ADMIN` 和
  `device_cgroup_rules: c 10:200 rwm`，并检查宿主机的容器安全策略。
- 容器 healthy 但后台异常：在 Sub2API 容器内解析 `warp-proxy`，确认两者由同一组
  Compose 文件启动并位于同一个项目网络。
- `warp=off`：查看 WARP 注册日志和主机时间，必要时保留 `warp-data` 卷后重建容器。
- 某个账号没有使用 WARP：检查账号是否配置了自己的代理；账号代理按设计优先。

FlareSolverr 和 Privoxy 不属于此版本。Sub2API 直接连接 SOCKS5H，DNS 解析也通过
代理完成。

## 镜像升级

不要改用 `latest`。升级前在测试节点确认新 tag 与 digest，然后一次只升级一台：

1. 修改 `docker-compose.warp.yml` 中的 tag 和 digest。
2. 执行 `docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml pull warp-proxy`。
3. 执行 `docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml up -d warp-proxy`。
4. 验证 Cloudflare trace 和管理后台当前节点状态，再升级下一台。

`warp-data` 命名卷保存 WARP 注册状态，普通镜像升级不应删除该卷。

## 回滚

先在管理后台关闭“OpenAI 默认出口代理”并保存，等待三个实例通过 Redis 收到设置
刷新。此时账号代理仍然生效，没有账号代理的请求恢复直连。

然后在每台节点移除 sidecar：

```bash
docker compose -f docker-compose.standalone.yml -f docker-compose.warp.yml stop warp-proxy
docker compose -f docker-compose.standalone.yml up -d --remove-orphans
```

保留 `warp-data` 卷可快速恢复；确认不再需要 WARP 前不要删除该卷。不要先移除
sidecar 再关闭 `fail_closed`，否则 OpenAI 请求会按策略失败。
