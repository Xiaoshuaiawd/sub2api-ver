# Vcode 集成部署合同

Vcode 使用本实例的用户、分组权限、模型目录、余额和用量流水。Vcode 客户端及 Agent 不持有本实例 API Key；只有 sub2api 网关内部按 `(user_id, group_id)` 使用受管 Key，账单仍走原网关链路。

## 服务端设置

sub2api 后端设置 `VCODE_INTEGRATION_SECRET`，值为独立生成的高熵随机字符串，至少 32 字节。Vcode 控制服务设置相同的 `VCODE_INTEGRATION_SECRET`，并设置 `SUB2API_ORIGIN` 为本实例固定 HTTPS origin。两者必须使用单独的受保护环境文件，不得编入前端、CLI、镜像默认值或仓库。

sub2api 后端还必须单独设置 `VCODE_MANAGED_KEY_SECRET`（至少 32 字节，且与 `VCODE_INTEGRATION_SECRET` 不同）。它只保存在 sub2api，用于派生网关内部受管 Key，绝不能交给 Vcode 控制服务。若曾使用旧共享密钥派生受管 Key，升级前需在 sub2api 停用旧 `sk-vcode-` Key 并轮换两项密钥；单独轮换集成签名密钥不会撤销已创建的 Key。

sub2api 前端构建可设置 `VITE_VCODE_CONTROL_ORIGIN`；默认值为 `https://agent.albkw.cn`。该地址只用于授权后的固定回跳，登录用户不能自行修改。Vcode 控制服务的 Nginx 需将 `/api/v1/model/` 流式代理到控制服务，并关闭响应缓冲。

建议先升级 sub2api 后端与前端，再升级 Vcode 控制服务，最后发布桌面、Web 和服务器 CLI。没有服务间密钥时，sub2api 的 Vcode 授权端点不会注册；带 Vcode 委托头的模型请求会被拒绝，普通 API Key 网关不受影响。

## 接口与归属

| 接口 | 认证 | 用途 |
| --- | --- | --- |
| `POST /api/v1/vcode/authorize` | sub2api 用户 JWT | 为当前用户签发 5 分钟、一次性的 PKCE 授权码 |
| `POST /api/v1/vcode/exchange` | 服务间密钥 + 授权码 + verifier | 兑换稳定用户 ID 与展示名；用户状态重新检查 |
| `GET /api/v1/vcode/identity/:id` | 服务间密钥 | 检查用户仍然有效 |
| `GET /api/v1/vcode/catalog/:id` | 服务间密钥 | 返回当前可绑定活跃分组及白名单内模型 |
| `GET /api/v1/vcode/catalog` | sub2api 用户 JWT | 同一目录的用户本人入口 |
| `POST /v1/messages`、`/v1/responses`、`/v1/chat/completions` | Vcode 签名委托或普通 API Key | 模型调用；委托先解析内部受管 Key，再进入原鉴权与计费 |

委托签名绑定方法、路径、用户 ID、分组 ID、时间、随机 nonce 与请求体 SHA-256；Redis `SETNX` 保证 nonce 只能使用一次。用户/分组/模型、额度与订阅的最终准入由原网关在请求时检查。目录是选择提示，不代表未来的授权承诺。Vcode 委托请求禁用跨组 fallback，分组倍率、个人倍率和高峰倍率均以所选分组的原账单流水为准。

## 联调验收

1. 同一用户在两个不同倍率的分组选择同名模型，分别发送流式请求；确认用量记录的用户、分组、模型和实扣金额正确。
2. 撤销某分组、启用模型白名单或停用用户后，旧目录选择的新请求必须被拒绝；刷新目录后旧选项消失。
3. 错误签名、过期时间、重复 nonce、修改请求体、错误 PKCE verifier 和二次兑换授权码均应被拒绝。
4. 校验断流及取消请求只按 sub2api 实际执行用量结算；Vcode 不对已进入网关的模型请求自动重发。
5. 真实桌面、手机浏览器与服务器 CLI 完成登录回跳和设备归属验证；两台服务器的任务仍由各自 Host 管理。
