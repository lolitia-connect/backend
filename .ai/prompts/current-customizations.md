# 当前后端定制保护 prompt

适用于日常开发、故障修复和上游移植。可以直接把本文交给开发 agent；`<任务>` 替换为本次需求。本文件只指导工作，不授权提交、部署、运行生产迁移或处理真实凭据。

## 开始前读取

1. [当前定制台账](../sync-diffs/v1.21.7-local-2026-11-19.md)，优先[第 8 节 B08–B16](../sync-diffs/v1.21.7-local-2026-11-19.md#8-截止后增量至-57973bf2)，再读[原始 Markdown 索引](../sync-diffs/v1.21.7-local-2026-11-19.files.md)和[259 项增量明细](../sync-diffs/v1.21.7-local-2026-11-19-2220.files.md)。后续差异记录和 prompts 只保存为 .md，说明具体行为变化，不另建结构化清单。
2. [架构差异](./architecture-context.md)及[上次同步历史](../sync-diffs/v1.20.3-to-v1.21.7.md)。历史的“已验证”只适用于当时版本。
3. 涉及浏览器行为时，读取[前端当前约束](../../../frontend/.ai/prompts/current-customizations.md)。

已确认的官方目标是 v1.21.7 `dfa796e0f4d11e0e4f5e9710042630969eda0871`，本地同步落点 `34fd88c842febe406252fa0d3fd2686622e73832`；最新补账固定至 `57973bf2be4d1805c0cb35bda0a01363ff00caee`，累计 9 个提交（7 非 merge）、277 个路径。原 `dc46571f` 与 W01 是保留的历史采样，不是当前状态。

先执行只读 `git status --short`、`git log`，比较实际 HEAD 和台账；必要时再针对相关未跟踪目录展开。UTC/02170 已随 `a6b11dfe` 提交，不得重用编号；23:06:02 +08:00 采样时编辑前工作树 clean。已提交、工作树、测试通过、生产应用是不同状态；不要 reset、覆盖、代为提交或把代码入库宣称为已发布。

## 文档归属

- README 只保留项目介绍、必要快速开始、部署警告和文档入口，不写 agent 任务进度、排障过程、测试输出或临时开发状态。
- 长期接口/数据层开发、目录职责、构建和兼容边界写入[后端开发指南](../../doc/development-zh.md)及[英文版](../../doc/development.md)；部署操作写入[安装指南](../../doc/install-zh.md)，配置字段写入配置指南，专项功能放到对应主题文档。
- 具体改动、同步范围、排障结论和带版本的验证结果归入已有 `.ai/sync-diffs`；同日补账追加独立 SHA/采样时间/行为编号及完整 Markdown 明细，保留旧哈希和原验证。累计路径从同步落点重算，不相加两期条目。可复用约束归本目录 prompts，不把 README 当持久记忆文本。
- 保持中英文文档和入口链接一致；不要覆盖他人并行内容，也不要把未提交工作或旧测试结果描述成已发布事实。

## 任务

在保留以下不变量的前提下实现 `<任务>`；先说明受影响的台账编号 B01–B16，再做精确改动；W01 只作历史证据。

- 保持 entgo、Hertz 兼容层和本地 logic/model/repository 分层；不能把上游 GORM/DDD 文件直接覆盖过来。
- schema 改动必须同时核对已嵌入的 MySQL/PostgreSQL SQL 升级链。`go generate .\ent` 只生成代码，不代替数据库升级；安装/升级入口执行的是 golang-migrate。02170 已提交，不能复用；实际迁移是否已应用须另查隔离环境证据，不能覆盖已部署迁移。
- 节点协议实例以 `type:id` 为键，公共节点响应保留字符串 `id/name` 与 `type/ratio`。不要按 type 去重或改成数值 ID。
- 保留无限流量、无到期语义、到期分组、稳定订阅引用、插件、Edge 订阅、V2 订单能力票据与事件版本、支付锁/幂等/原子预留。
- Stripe 渠道编辑要比较解析后的旧/新 notify URL；凭据未变不代表 URL 未变。保留 endpoint 创建、持久化失败清理及旧 endpoint 清理流程；禁止让前端直接标记支付成功。
- 业务错误当前依然可能是 HTTP 200 + 非成功 body.code。`HttpResult` 必须把错误交给 `ctx.Error` 供日志使用。改变 HTTP 状态政策需要单独需求与前后端兼容验证。

## 请求、账号与查询契约

- 检查所有使用到的 ShouldBind/JSON/Query 返回值，失败调用 ParamErrorResult 并立即 return；GET/HEAD 优先 query，即使 Content-Type 为 JSON。参数错误沿用 HTTP 200 + body.code=400；写入的 false/0 不丢失。
- 手机注册入口复用现有注册策略，Handler 提供 ClientIP/User-Agent；手机号身份按 E.164。用户 Insert 不会自动保存关联，手机号注册/管理创建用户必须显式 InsertUserAuthMethods，账号、身份、初始钱包在同一事务内；查重非 NotFound 错误不可当不存在。
- 头像仅在值变化且非空时校验图片数据；原 OAuth URL 在启停/资料更新时保留，新无效头像仍拒绝。不要把此修复扩为免验证任意 URL 或改变前端全资料载荷契约。
- 认证方式按 method 找原记录，只有请求提供 enabled/config 时更新；未传 config 保留原值，不能 DeepCopy 请求清空 ID/配置。config 必须对象，email/mobile 字段类型错误在保存前拒绝。
- 用户订阅列表保留 ID 排序、page/size 和完整 total；访问日志按用户/订阅/类型过滤并解析 Content。单用户订阅流量的 start_time/end_time 是 Unix 毫秒，独立 GTE/LT 边界；不能混成秒或只在双边均给定时筛选。
- 流量汇总的 start_date/end_date 包含首尾日期、优先于 date，允许单边；今日与历史共用筛选，跨区间分页使用 offset 补足并保留总数。明细日历范围按 time.Local 和次日 AddDate 构成半开区间，不能固定 24h。
- 用户规则省略/nil 不更新，显式 `[]` 清空，非空替换；不能再用 len>0 吞掉清空请求。

## OAuth：必须共同维护的两阶段契约

1. `/v1/auth/oauth/login/token` 验证提供方；老用户正常登录。
2. 新身份缺少必要信息时延迟注册：响应 data 带 `oauth_token` 与所缺标志，`token` 为空。强制邀请缺码置 `invite_required: true`；提供方未给邮箱（如 Telegram）置 `email_required: true`（开启邮箱验证再附 `email_verify: true`）。只在 Redis 缓存已验证身份（10 分钟），不写用户相关持久记录。
3. 禁止伪造占位邮箱：Telegram 不再写 `<id>@ppanel.com` 并视为已验证。提供方给的邮箱按已验证入库；用户补填的邮箱仅在邮箱验证开启且校验码通过后才 `Verified:true`，否则 `Verified:false`，与普通邮箱注册语义一致。
4. 前端按标志收集邀请码/真实邮箱(+校验码)，POST `/v1/auth/oauth/register`，载荷 `{oauth_token, invite?, email?, code?}`。满足要求后才创建账号/发会话；失败允许重试，不重放已消费的 Telegram/OAuth 回调。
5. 临时票据不是登录 token，不能持久化为登录态。当前代码成功后 DEL，不是已经证明并发严格单次消费的原子协议。
6. 不要声称 OAuth 发起/换 token 已验证所有验证码字段：当前发起只有 method/redirect，换 token 额外支持 invite；register 额外支持 email/code；以实际请求类型为准。
7. 修改类型/路由后，使用兄弟前端的 `api:refresh`、`api:check`、`api:verify`，核对手工 common 类型与生成层，不能从浮动上游 Swagger 覆盖本地契约。

## 时区：分开存储、日界线与显示

昨日排名直接查 traffic_log、昨日失败不阻断今日面板、cron 用 `time.Local` 的原行为继续保留。原 W01 已在 `a6b11dfe` 提交：有效 DSN query 固定 UTC，显示与日/月分组显式转 `time.Local`；无效 query 原样返回，MySQL loc 不等于 session time_zone。

接手 B08 时先核对固定 SQL、历史数据和实际应用版本，不运行共享数据库。旧数据不一定全是上海墙上时间；历史 UTC 流量桶、无到期哨兵、既有 timestamptz 列与 MySQL session 时区需逐项确认。MySQL 02170 的 ±8h 数据重写不是任意重跑幂等，PG down 没有保存每列来源、仅排除 servers.last_reported_at；不能因为 up/down 已提交就宣称可无损回退或迁移已完成。

## 最小验证与交付

- 只运行覆盖本次改动的现有测试；错误响应优先 [pkg/result](../../pkg/result/)，绑定优先 [hertzx](../../pkg/hertzx/)及 [handler 测试](../../internal/handler/binding_test.go)，其余按逻辑包选择。新增头像/账号/配置/订阅/流量/规则选择器见[最新验证矩阵](../sync-diffs/v1.21.7-local-2026-11-19.md#本轮证据与后续最小验证)；原范围见[历史入口](../sync-diffs/v1.21.7-local-2026-11-19.md#6-接手验证与证据边界)。命令和测试存在不是本轮通过记录。
- 数据库测试会写库；只在显式配置的隔离环境使用 `PPANEL_TEST_MYSQL_DSN` / `PPANEL_TEST_POSTGRES_DSN`，不要复用开发/生产配置。真实 Stripe/OAuth 测试不得混同 mock 或静态检查。
- 文档-only 不需跑业务构建；检查链接、提交/文件清单与 `git diff --check` 即可。不得把未执行的检查写成通过。
- 输出本次文件/行为变化、保护的定制、不兼容项、实际验证和未验证项。更新相关 sync-diffs 与本 prompt；只把本次引入/修复行为的必要测试补到现有位置。
