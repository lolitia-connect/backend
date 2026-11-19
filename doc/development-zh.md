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
