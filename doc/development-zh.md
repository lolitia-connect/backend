# 后端开发指南

本文记录目录职责、接口与数据层开发、构建和兼容边界。安装与运行请看 [安装指南](install-zh.md)，配置请看 [配置指南](config-zh.md)，PR 流程见 [贡献须知](../CONTRIBUTING_ZH.md)。具体同步记录、任务过程和验证结果保留在 [同步台账](../.ai/sync-diffs/README.md)，不要追加到 README。

[English](development.md)

## 目录结构

```
.
├── cmd/              # 应用程序入口（run、healthcheck、migrate、version）
├── doc/              # 文档（config.md、install.md）
├── docs/             # 专项文档（如 edge-subscribe.md）
├── config.yaml.example # 配置文件示例
├── ent/              # entgo 生成的 ORM 客户端与 schema
├── initialize/       # 系统初始化与数据库迁移
├── internal/         # 内部模块
│   ├── config/       # 配置文件解析
│   ├── handler/      # HTTP 处理程序
│   ├── middleware/   # HTTP 中间件
│   ├── logic/        # 业务逻辑
│   ├── model/        # 数据模型
│   ├── plugin/       # WASM 插件系统
│   ├── svc/          # 服务层
│   ├── transport/    # HTTP 服务器与健康检查端点
│   └── types/        # 类型定义
├── pkg/              # 公共工具代码
├── plugins/          # 插件目录（WASM 二进制不入库）
├── queue/            # 队列服务
├── scheduler/        # 定时任务
├── script/           # 安装脚本与辅助脚本
├── scripts/          # 性能测试与维护脚本
├── go.mod            # Go 模块定义
├── Makefile          # 构建自动化
└── Dockerfile        # Docker 配置
```

## 开发流程

### 添加新接口

本 fork 使用手写的 Hertz 处理器（`internal/handler/`），不依赖 go-zero `.api` 生成步骤。
新增接口时：

1. 在 `internal/types/` 增加请求/响应类型。
2. 在 `internal/logic/` 实现业务逻辑。
3. 在 `internal/handler/` 添加处理器，并在 `internal/handler/routes.go` 注册路由。
4. 若前端需要接入，在 `frontend/` 中运行 `bun run api:refresh` 重新导出本地后端的路由与类型
   （契约由 `frontend/scripts/backend-contract.go` 从本仓库的 Go AST 直接导出，见
   [前端开发指南](../../frontend/docs/zh/guide/frontend-development.md)）。

数据层是 entgo：修改 `ent/schema/` 后运行 `go generate ./ent`，或直接
`go run entgo.io/ent/cmd/ent generate ./ent/schema`。数据库结构变更仍以
`initialize/migrate/database/` 下成对的手写 SQL 迁移为准。

### 请求解析与更新约定

- Handler 必须检查 `ShouldBind` / `ShouldBindJSON` / `ShouldBindQuery` 的返回错误，失败时调用
  `result.ParamErrorResult` 并立即返回，不能用未完成解析的零值继续查询或更新。
  参数错误沿用 HTTP 200 响应包中的业务码 `400`。
- GET/HEAD 从查询参数绑定，即使请求带有 `Content-Type: application/json`；写入请求继续按内容类型解析，
  显式的 `false` 和 `0` 不应丢失。
- 账号、登录方式及初始钱包应在同一事务中创建。ent 的用户 `Insert` 不会自动保存 `AuthMethods`，
  必须显式调用 `InsertUserAuthMethods`；手机号统一使用 E.164 格式。
  `/v1/auth/register/telephone` 复用手机号注册流程，并由 Handler 提供实际客户端 IP 和 User-Agent。
- 更新认证方式时以 `method` 查找原记录；只切换 `enabled` 不应重置现有配置或修改记录 ID。
  非对象配置和邮件/手机配置中的错误字段类型应在保存前拒绝。
- 用户规则接口传入空数组会清空规则，省略该字段则保留原规则。管理端用户订阅列表遵循分页参数；订阅访问日志按用户、订阅和日志类型筛选，
  从日志内容还原 IP、User-Agent 和订阅信息。
- 流量日志的 `start_date` / `end_date` 为包含首尾日期的范围，优先于单日 `date`；允许仅指定一端。
  今日聚合与历史日志使用同一日期筛选，合并分页返回完整总数，超出最后一页返回空列表。
  流量明细使用本地日历日边界 `[开始日零点, 结束日次日零点)`，未提供日期时仍默认查询今日。
  单个用户订阅流量接口的 `start_time` / `end_time` 使用 Unix 毫秒时间戳，包含开始时刻、不包含结束时刻。

### 订阅节点名称

普通订阅下载与管理端模板预览共用 `adapter.Client.Build`。本次渲染的有效节点先按
节点设置的 `Sort` 升序排列，再为名称完全相同的节点追加 ` #1`、` #2`、` #3` 等后缀，
包括第一个同名节点。排序值相同时保持输入顺序，节点定义和分组引用使用相同的有序列表。
编号按每份配置独立计算，单独出现的名称保持原样；排序和编号都在渲染副本中完成，
不修改数据库节点或原始代理数据。

自动编号会跳过本份配置中已存在的名称。例如两个 `香港` 节点与一个 `香港 #1` 节点
一起输出时，前两个节点按排序命名为 `香港 #2`、`香港 #3`，原有 `香港 #1` 保持不变。
模板通过 `.Proxies` 的 `Name` 同时生成节点定义和分组引用，以保持名称一致。

### 多平台构建

使用 `Makefile` 构建多种平台（如 Linux、Windows、macOS）：

```bash
make all  # 构建 linux-amd64、darwin-amd64、windows-amd64
make linux-arm64  # 构建特定平台
```

支持的平台包括：

- Linux：`386`、`amd64`、`arm64`（`make` 还可构建 `linux-amd64-v3`、`linux-armv5`、`linux-armv6`、
  `linux-armv7`）
- Windows：`386`、`amd64`、`arm64`（以及 `windows-amd64-v3`、`windows-armv7`）
- macOS：`amd64`、`arm64`（以及 `darwin-amd64-v3`）

所有二进制都是静态链接的（`CGO_ENABLED=0 -trimpath`），版本信息由 Makefile 注入到
`pkg/constant.Version` / `pkg/constant.BuildTime`。发布（见
[`.github/workflows/release.yml`](../.github/workflows/release.yml)）为上述平台提供
`ppanel-server-<os>-<arch>.tar.gz`（Windows 为 `.zip`），每个压缩包附带一个 `.sha256` 校验文件。

## 架构兼容边界

| 方面 | 本 fork | 上游 v1.21.7 |
|---|---|---|
| ORM | entgo（`ent/`、`pkg/orm`） | GORM |
| 节点协议实例 | 多协议实例，以 `type:id` 为键 | 单协议引用 |
| 插件 | 内置 WASM 插件系统（`internal/plugin`、`Plugin` 配置段） | 无 |
| 无限流量 | 支持 `traffic_unlimited` | — |
| Stripe | Checkout Sessions（`CreateCheckoutSession`） | — |
| 订阅渲染 | Edge 订阅签名接口，见 [docs/edge-subscribe.md](../docs/edge-subscribe.md) | 无 |
| 配置文件路径 | `config.yaml` | `etc/ppanel.yaml` |
| 配置段 | 有 `Transport`、`Device`、`Plugin`、`EdgeSubscribe`；`MySQL` 为废弃别名 | 有 `AppLocation`、`Redis.QueueDB`、`Database.ConnMaxLifetime/ConnMaxIdleTime` |
| 队列 Redis 库 | 硬编码为 `5`（`queue/queue.go`） | `Redis.QueueDB` 可配置 |

完整说明见[配置指南](config-zh.md)。
