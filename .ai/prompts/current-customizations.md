# 当前后端定制保护 prompt

适用于日常开发、故障修复和上游移植。可以直接把本文交给开发 agent；`<任务>` 替换为本次需求。本文件只指导工作，不授权提交、部署、运行生产迁移或处理真实凭据。

## 开始前读取

1. [当前定制台账](../sync-diffs/v1.21.7-local-2026-11-19.md)与[Markdown 逐文件改动索引](../sync-diffs/v1.21.7-local-2026-11-19.files.md)。后续差异记录和 prompts 只保存为 .md，说明具体行为变化，不另建结构化清单。
2. [架构差异](./architecture-context.md)及[上次同步历史](../sync-diffs/v1.20.3-to-v1.21.7.md)。历史的“已验证”只适用于当时版本。
3. 涉及浏览器行为时，读取[前端当前约束](../../../frontend/.ai/prompts/current-customizations.md)。

已确认的官方目标是 v1.21.7 `dfa796e0f4d11e0e4f5e9710042630969eda0871`，本地同步落点 `34fd88c842febe406252fa0d3fd2686622e73832`；台账已提交快照是 `dc46571fe94d2be00c9718ede28ce5c3914242c3`。它不是运行时 HEAD 的保证。

先执行只读 `git status --short --untracked-files=all`、`git log`，比较实际 HEAD 和台账。把既有未提交改动与本次工作分开，尤其是 UTC/02170 迁移；不要 reset、覆盖、代为提交或将它宣称为已发布。

## 文档归属

- README 只保留项目介绍、必要快速开始、部署警告和文档入口，不写 agent 任务进度、排障过程、测试输出或临时开发状态。
- 长期接口/数据层开发、目录职责、构建和兼容边界写入[后端开发指南](../../doc/development-zh.md)及[英文版](../../doc/development.md)；部署操作写入[安装指南](../../doc/install-zh.md)，配置字段写入配置指南，专项功能放到对应主题文档。
- 具体改动、同步范围、排障结论和带版本的验证结果归入已有 `.ai/sync-diffs`；可复用 agent 约束归入本目录 prompts。所有整理记录只保存 .md，不把 README 当持久记忆文本。
- 保持中英文文档和入口链接一致；不要覆盖他人并行内容，也不要把未提交工作或旧测试结果描述成已发布事实。

## 任务

在保留以下不变量的前提下实现 `<任务>`；先说明受影响的台账编号 B01–B07 / W01，再做精确改动。

- 保持 entgo、Hertz 兼容层和本地 logic/model/repository 分层；不能把上游 GORM/DDD 文件直接覆盖过来。
- schema 改动必须同时核对已嵌入的 MySQL/PostgreSQL SQL 升级链。`go generate .\ent` 只生成代码，不代替数据库升级；安装/升级入口执行的是 golang-migrate。不要复用已部署的迁移编号，也不要占用工作树已有的 02170。
- 节点协议实例以 `type:id` 为键，公共节点响应保留字符串 `id/name` 与 `type/ratio`。不要按 type 去重或改成数值 ID。
- 保留无限流量、无到期语义、到期分组、稳定订阅引用、插件、Edge 订阅、V2 订单能力票据与事件版本、支付锁/幂等/原子预留。
- Stripe 渠道编辑要比较解析后的旧/新 notify URL；凭据未变不代表 URL 未变。保留 endpoint 创建、持久化失败清理及旧 endpoint 清理流程；禁止让前端直接标记支付成功。
- 业务错误当前依然可能是 HTTP 200 + 非成功 body.code。`HttpResult` 必须把错误交给 `ctx.Error` 供日志使用。改变 HTTP 状态政策需要单独需求与前后端兼容验证。

## OAuth：必须共同维护的两阶段契约

1. `/v1/auth/oauth/login/token` 验证提供方；老用户正常登录。
2. 强制邀请的新身份缺少邀请码时，响应 data 是 `invite_required: true` + `oauth_token`，`token` 为空。只在 Redis 缓存已验证身份（10 分钟），不写用户相关持久记录。
3. 前端收集邀请码，POST `/v1/auth/oauth/register`，载荷 `{oauth_token, invite}`。有效邀请码之后才走创建账号/发会话；失败允许重试，不重放已消费的 Telegram/OAuth 回调。
4. 临时票据不是登录 token，不能持久化为登录态。当前代码成功后 DEL，不是已经证明并发严格单次消费的原子协议。
5. 不要声称 OAuth 发起/换 token 已验证所有验证码字段：当前发起只有 method/redirect，换 token 额外支持 invite；以实际请求类型为准。
6. 修改类型/路由后，使用兄弟前端的 `api:refresh`、`api:check`、`api:verify`，核对手工 common 类型与生成层，不能从浮动上游 Swagger 覆盖本地契约。

## 时区：分开存储、日界线与显示

已提交行为：昨日排名直接查 traffic_log；昨日查询失败不阻断今日面板；cron 用 `time.Local`。未提交 W01 正把连接存储改为 UTC，并把通知/日期取值显式转到 `time.Local`。

接手 W01 时先重新核对文件状态与哈希，不运行共享数据库。旧数据不一定全是上海墙上时间；历史 UTC 流量桶、无到期哨兵、已是 timestamptz 的列、MySQL session time_zone 和回滚范围都要逐一确认。不得因为存在 up/down 文件就宣称迁移幂等或可无损回退。

## 最小验证与交付

- 只运行覆盖本次改动的现有测试；错误响应优先 [pkg/result](../../pkg/result/)，时区优先 [pkg/orm](../../pkg/orm/)、[traffic model](../../internal/model/traffic/)、[scheduler](../../scheduler/)。具体命令和覆盖缺口见[台账验证节](../sync-diffs/v1.21.7-local-2026-11-19.md#6-接手验证与证据边界)。
- 数据库测试会写库；只在显式配置的隔离环境使用 `PPANEL_TEST_MYSQL_DSN` / `PPANEL_TEST_POSTGRES_DSN`，不要复用开发/生产配置。真实 Stripe/OAuth 测试不得混同 mock 或静态检查。
- 文档-only 不需跑业务构建；检查链接、提交/文件清单与 `git diff --check` 即可。不得把未执行的检查写成通过。
- 输出本次文件/行为变化、保护的定制、不兼容项、实际验证和未验证项。更新相关 sync-diffs 与本 prompt；只把本次引入/修复行为的必要测试补到现有位置。
