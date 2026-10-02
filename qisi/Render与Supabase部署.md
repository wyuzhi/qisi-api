# Render + Supabase 部署

`render.yaml` 是可选的免费试运行模板，不代表服务已经部署，也不是稳定商用承诺。账号、余额、任务和日志保存在 Supabase PostgreSQL；Render 只运行 Go 服务。qisiTV 的项目及创作素材仍按画布的本地保存方式使用。

## 1. 准备独立数据库

在自己的 Supabase 账号准备独立项目，选择与 Render 接近的区域。此应用只使用 PostgreSQL，**先在项目的 Data API 设置中关闭 Enable Data API**，再初始化应用表，避免公开 schema 经自动 REST/GraphQL 接口暴露；不需要接入 Supabase Auth。[官方说明](https://supabase.com/docs/guides/api/securing-your-api)

从项目 **Connect → Session pooler** 复制连接串，使用 IPv4 可达的会话池端口 **5432**，加入 TLS 参数。例如下面全是占位符，不能直接使用：

```text
postgresql://postgres.PROJECT_REF:ENCODED_PASSWORD@POOLER_HOST:5432/postgres?sslmode=require
```

主机和用户名以控制台实际值为准；密码中的保留字符必须 URL 编码。`sslmode=require` 强制加密，但不校验服务器身份。需要完整验证时，在 Supabase 下载数据库根证书，通过 Render Secret Files 挂载，然后改用 `sslmode=verify-full&sslrootcert=/etc/secrets/实际证书文件名`；本机初始化使用本机证书路径。不要使用 `sslmode=disable`。[连接与 TLS 文档](https://supabase.com/docs/guides/database/connecting-to-postgres)

这里使用长期运行的 Go 服务，不选面向 serverless 的事务池 `6543`。现有 PostgreSQL 驱动已经设置 `PreferSimpleProtocol=true`、关闭 GORM prepared statements，无需改动 ORM。模板将应用连接池限制为最多 10 条、空闲 2 条，后续按真实并发和数据库上限调整。

## 2. 先在本机完成私有初始化

**不能先创建公开 Render 服务再抢着填写管理员。** 新数据库的初始化接口没有现成管理员保护，必须先通过本机 loopback 初始化同一个远程数据库。

1. 按项目构建步骤生成最新版 `.local-tests/qisi-api-release`，前端构建使用 `QISI_BASE_PATH=/api-service`。不要使用旧演示二进制。
2. 运行 `node qisi/bootstrap-private.mjs --prepare`，生成仅本机用户可读的 `.local-tests/production-runtime.json`。它包含新的会话密钥与独立管理员凭据，不沿用本地演示账号。
3. 在本机编辑这个私有文件，填入上一步的 `SQL_DSN`。不要贴到聊天、提交 Git 或上传整个 JSON。
4. 运行 `node qisi/bootstrap-private.mjs`。脚本只监听 `127.0.0.1:4320`，连接该远程数据库完成初始化和零余额注册设置后停止。私有阶段使用本地 HTTP；公网模板必须恢复 Secure Cookie。
5. 确认初始化成功、正式管理员可用、`/api-service/api/setup` 的 `data.status` 为 `true` 后，才进入下一步。若失败，先修复，不创建公网服务。`--local-check` 的 SQLite 演练不能代替这次 PostgreSQL 验证。

如果创建管理员后某一步失败，不要重置数据库或把 `setup.status=true` 当成全部成功。使用同一私有配置再次以 loopback 方式启动后台，在浏览器用该配置内的新管理员凭据登录，补齐并核查下面的选项，再停止本机进程。初始化脚本会拒绝覆盖已初始化站点。

初始化应保持：注册和密码登录开启；新用户、邀请与被邀请赠额为 0；不自动生成默认令牌；`RetryTimes=0`；签到赠额、免费模型和未准备的订阅销售关闭。系统地址为 `https://cheeser.link/api-service`。未配置商户时在线充值不可用；未完成真实计费验收时，收费生成渠道保持禁用。**这些是数据库中的管理选项，不能只靠环境变量或“用户余额为零”代替。** 计费开放条件见[计费上线审查](./计费上线审查.md)。

## 3. 发布 Render，再接网站栏目

在 Render 连接含此版本的代码仓库，创建 Blueprint 时将配置路径指定为 `qisi/render.yaml`。构建上下文是仓库根目录，使用现有 Dockerfile，不把 `rootDir` 改成 `qisi`。`autoDeployTrigger: off` 仅关闭后续自动部署，**不会阻止首次启动**。[Blueprint 文档](https://render.com/docs/blueprint-spec)

首次创建会要求填写两个私密环境变量：

| 变量 | 要求 |
| --- | --- |
| `SQL_DSN` | 指向已私有初始化的同一个 Supabase 数据库，必须非空并启用 TLS；空值会退回临时 SQLite，不能上线 |
| `SESSION_SECRET` | 与私有初始化完全相同，长期妥善保存；更换会使已有会话失效，且默认也用于密钥加密配置 |

其余值已写在模板：构建和运行前缀 `/api-service`，监听 `0.0.0.0:10000`，Secure Cookie、可信来源 `https://cheeser.link`，注册密码至少 15 字符，关闭默认令牌，开启认证限流。Render 会把环境变量提供给 Docker 构建；Dockerfile 只声明路径参数，不要增加读取上述秘密的 `ARG` 或将私有文件复制进镜像。[Docker 文档](https://render.com/docs/docker)

服务健康路径是 `/api-service/api/status`。确认数据库、初始化状态和重启后的持久化都正常，再按 [Vercel 栏目代理示例](./vercel-rewrites.example.json) 合并 `/api-service` 规则，保留现有 qisiTV 路由和前缀。不要缓存登录、余额、令牌或任务接口。`TRUSTED_PROXIES=none` 防止盲信伪造转发头，但可能将多个用户识别成同一代理 IP；正式开放前验证真实代理链，仅填写实际可信 IP/CIDR，再验证限流，不能改成信任任意来源。

通过 `https://cheeser.link/api-service/` 检查注册、登录、退出、零余额、未开通支付提示和 Cookie 的 Secure/HttpOnly/子路径，验证服务重启后账号与余额仍在。首次管理员登录不使用 Render 临时域名绕过来源限制。LikeAI 上游 Key 只录入服务端渠道；不放在模板、Vercel 公共变量或前端源码里。真实收费测试需单独确定预算。

## 免费试运行的边界

- Render Free 闲置 15 分钟休眠，唤醒约 1 分钟；任务轮询也会暂停。文件系统临时，不保存 SQLite 或长期素材，也不能使用持久磁盘。每工作区每月 750 小时；带宽与构建额度另计，连接外部数据库/API 的异常高流量也可能导致暂停。超额行为取决于是否绑定支付方式。Render 官方明确不建议 Free 用于生产。[免费限制](https://render.com/docs/free)
- Supabase Free 低活跃项目可能在 7 天低活动周期后暂停。应制定独立备份和恢复流程；有持续营业需求时选择满足可用性要求的套餐，不把免费额度当作服务保证。[暂停规则](https://supabase.com/docs/guides/platform/free-project-pausing)
- 若 cheeser.link 所在 Vercel 仍使用 Hobby，它仅适用于个人、非商业用途；开放收费业务前核对并选择允许商业使用的计划。这不是对当前账号计划的确认，也不自动购买或升级。[Hobby 规则](https://vercel.com/docs/plans/hobby)

发布修改版仍须保留 New API / QuantumNous 署名、AGPL-3.0 许可证和第三方许可，并向使用者提供对应上线版本的源码。本文只准备部署方法；远程 PostgreSQL 初始化、Render 构建、域名代理、支付及付费生成均须分别记录真实验收结果，不能用本机测试替代。
