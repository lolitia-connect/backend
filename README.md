# PPanel 服务端

<div align="center">

[![License](https://img.shields.io/github/license/lolitia-connect/backend)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.27%2B-blue)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Available-blue)](Dockerfile)
[![CI/CD](https://img.shields.io/github/actions/workflow/status/lolitia-connect/backend/release.yml)](.github/workflows/release.yml)

**PPanel 是一个纯净、专业、完美的开源代理面板工具，旨在成为您学习和实际使用的理想选择。**

[中文](README.md) | [English](README_EN.md) | [报告问题](https://github.com/lolitia-connect/backend/issues/new) | [功能请求](https://github.com/lolitia-connect/backend/issues/new)

> **本仓库是 [perfect-panel/server](https://github.com/perfect-panel/server) 的 fork**
> （[lolitia-connect/backend](https://github.com/lolitia-connect/backend)），会定期合并上游的发布 tag
> （见 [`.ai/sync-diffs/`](.ai/sync-diffs/)）。与上游相比，本 fork 使用 entgo 而非 GORM、支持多协议实例
> （以 `type:id` 为键）、内置 WASM 插件系统、无限流量（`traffic_unlimited`）、Stripe Checkout Sessions
> 与 Edge 订阅（见 [docs/edge-subscribe.md](docs/edge-subscribe.md)）。配置文件为 `config.yaml`
> （上游为 `etc/ppanel.yaml`）。

</div>

> **第一条**  
> 人人生而自由，在尊严与权利上一律平等。  
> 他们赋有理性与良知，应当以兄弟般的精神彼此相待。  
>
> **第十二条**  
> 任何人的隐私、家庭、住宅和通信不得任意干涉，其名誉与荣誉不得加以攻击。  
> 人人有权受到法律的保护，以免遭受这种干涉或攻击。  
>
> **第十九条**  
> 人人有思想与表达的自由；此项自由包括持有主张而不受干预，以及通过任何媒介、无论国界，自由寻求、接受和传播信息与思想。  
>
> *来源： [United Nations – Universal Declaration of Human Rights (UN.org)](https://www.un.org/sites/un2.un.org/files/2021/03/udhr.pdf)*

## 📋 概述

PPanel 服务端是 PPanel 项目的后端组件，为代理服务提供强大的 API 和核心功能。它基于 Go 语言开发，注重性能、安全性和可扩展性。

### 核心特性

- **多协议支持**：支持 Shadowsocks、V2Ray、Trojan 等多种加密协议。
- **隐私保护**：不收集用户日志，确保隐私和安全。
- **极简设计**：简单易用，保留完整的业务逻辑。
- **用户管理**：完善的认证和授权系统。
- **订阅管理**：处理用户订阅和服务开通。
- **支付集成**：支持多种支付网关。
- **订单管理**：跟踪和处理用户订单。
- **工单系统**：内置客户支持和问题跟踪。
- **节点管理**：监控和控制服务器节点。
- **API 框架**：提供全面的 RESTful API，供前端集成。

## 🚀 快速开始

### 前提条件

- **Go**：1.27 或更高版本（`go.mod` 要求 `go 1.27.4`）
- **数据库**：MySQL 8.0+ / MariaDB，或 PostgreSQL 16+，以及 Redis 6.0+
- **Docker**：可选，用于容器化部署
- **Git**：用于克隆仓库

需要部署已发布的二进制而不是自行构建？请看[安装指南](doc/install.md)（`script/install.sh`、systemd
服务、校验和验证）和[配置指南](doc/config.md)。

### 通过源代码运行

1. **克隆仓库**：
   ```bash
   git clone https://github.com/lolitia-connect/backend.git
   cd backend
   ```

2. **安装依赖**：
   ```bash
   go mod download
   ```

3. **构建项目**（二进制输出到 `bin/`；请选择将要运行它的机器对应的目标）：
   ```bash
   make linux-amd64
   ```

4. **启动服务器**：
   ```bash
  ./bin/ppanel-server-linux-amd64 run --config config.yaml
   ```
   配置文件为空时，首次启动会在 `127.0.0.1` 的配置端口 `Port` 上提供安装向导（默认
   `http://127.0.0.1:8080/init`），只能从本机访问。远程主机请建立 SSH 隧道
   （`ssh -L 8080:127.0.0.1:8080 user@host`），或设置 `PPANEL_DB` 与 `PPANEL_REDIS` 进行无人值守安装；
   两种方式都在[安装指南](doc/install-zh.md#4-首次启动)中说明。

### 🐳 Docker 部署

镜像以非特权用户（uid 65532）运行，监听 8080 端口，读取 `/app/config.yaml`，并通过
`ppanel healthcheck`（`HEALTHCHECK`）报告健康状态。**首次运行的安装向导在容器内只监听
`127.0.0.1`，无法通过映射的端口访问**，因此容器部署必须用以下两种方式之一提供连接信息：

- 设置 `PPANEL_DB` 与 `PPANEL_REDIS`：首次启动时服务用它们和生成的 JWT 密钥补全空的配置文件，执行迁移并
  创建首位管理员，其密码会在 `docker logs` 中打印一次；或
- 挂载预先填好的 `config.yaml`（见[配置指南](doc/config-zh.md)）。

| 变量 | 格式 | 示例 |
|---|---|---|
| `PPANEL_DB` | MySQL DSN `user:password@tcp(host:port)/dbname`，或 URL：`mysql://…`、`postgres://user:password@host:5432/dbname?sslmode=require` | `ppanel:secret@tcp(db:3306)/ppanel` |
| `PPANEL_REDIS` | `redis://[:password@]host[:port][/db]` | `redis://:secret@redis:6379/0` |

1. **构建 Docker 镜像**：
   ```bash
   docker buildx build --platform linux/amd64 -t ppanel-server:latest .
   ```

2. **运行容器**。挂载的配置文件必须存在，且在首次启动（服务补全配置）时对 uid 65532 可写：
   ```bash
   mkdir -p etc && touch config.yaml && sudo chown 65532:65532 config.yaml
   docker run -d --name ppanel-server -p 8080:8080 \
     -e PPANEL_DB='ppanel:secret@tcp(db:3306)/ppanel' \
     -e PPANEL_REDIS='redis://:secret@redis:6379/0' \
     -v "$(pwd)/config.yaml:/app/config.yaml" \
     ppanel-server:latest
   docker logs -f ppanel-server   # 首位管理员的密码只打印一次
   ```

3. **使用 Docker Compose**。仓库中的 [`docker-compose.yml`](docker-compose.yml) 构建镜像、发布 8080
   端口、挂载 `./config.yaml` 并检查容器健康状态：
   ```yaml
   services:
     ppanel:
       container_name: ppanel-server
       build:
         context: .
         dockerfile: Dockerfile
       ports:
         - "8080:8080"
       environment:
         PPANEL_DB: ${PPANEL_DB:-}
         PPANEL_REDIS: ${PPANEL_REDIS:-}
       volumes:
         - ./config.yaml:/app/config.yaml
         - ppanel-logs:/app/logs
         - ppanel-cache:/app/cache
       healthcheck:
         test: ["CMD", "/app/ppanel", "healthcheck", "--config", "/app/config.yaml"]
         interval: 30s
         timeout: 5s
         retries: 3
         start_period: 30s
       restart: always

   volumes:
     ppanel-logs:
     ppanel-cache:
   ```
   首次 `docker compose up` 之前，`./config.yaml` 必须以**文件**形式存在（否则 Docker 会创建同名目录），
   且对 uid 65532 可写；在 `.env` 或环境中给出 `PPANEL_DB` / `PPANEL_REDIS`，或预先填好文件，然后：
   ```bash
   mkdir -p logs cache && touch config.yaml && sudo chown 65532:65532 config.yaml
   PPANEL_DB='ppanel:secret@tcp(db:3306)/ppanel' PPANEL_REDIS='redis://:secret@redis:6379/0' docker compose up -d
   ```
   停止容器请用 `docker stop --time 20`，为优雅停机（先排空 HTTP，再依次停止调度器、任务 worker 和链路
   追踪导出）留出时间。

4. **拉取已发布的镜像**（CI/CD 发布后）：
   ```bash
   docker pull ppanel/ppanel-server:latest
   docker run --rm -p 8080:8080 ppanel/ppanel-server:latest
   ```

服务提供 `GET /healthz`（存活探针）与 `GET /readyz`（就绪探针：数据库与 Redis 可达）供监控和负载均衡
使用，见[配置指南](doc/config-zh.md#6-健康检查)。

## 📖 API 文档

查看完整的 API 文档：

- **Swagger 文档**：[https://ppanel.dev/zh-CN/swagger/ppanel](https://ppanel.dev/zh-CN/swagger/ppanel)

文档涵盖所有 API 端点、请求/响应格式及认证要求。根目录的 `ppanel.json` 是完整的 Swagger 2.0 文档。

## 🔗 相关项目

| 项目               | 描述           | 链接                                                    |
|------------------|--------------|-------------------------------------------------------|
| PPanel Web       | PPanel 前端应用  | [GitHub](https://github.com/perfect-panel/frontend) |
| PPanel User Web  | PPanel 用户界面  | [预览](https://user.ppanel.dev)                         |
| PPanel Admin Web | PPanel 管理员界面 | [预览](https://admin.ppanel.dev)                        |
| 上游服务端        | 本 fork 的上游    | [GitHub](https://github.com/perfect-panel/server)      |

## 🔒 安全

请通过 [GitHub Security Advisories](https://github.com/lolitia-connect/backend/security/advisories/new)
私下报告安全漏洞，不要在公开 issue 中提交。[SECURITY.md](SECURITY.md) 列出了本 fork 受支持的版本与处理流程。

## 🌐 官方网站

访问 [ppanel.dev](https://ppanel.dev) 获取更多信息。

## 🏛 系统架构

![Architecture Diagram](./doc/image/architecture-zh.png)

## 📁 目录结构

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

## 💻 开发

### 添加新接口

本分支的 HTTP 层是手写的 Hertz 处理器（`internal/handler/`），不再有 go-zero 的 `.api`
生成步骤：上游已在 `chore(codegen): remove legacy go-zero generation assets` 中移除该工具链，
本分支同步移除。新增接口的步骤是：

1. 在 `internal/types/` 增加请求/响应类型。
2. 在 `internal/logic/` 实现业务逻辑。
3. 在 `internal/handler/` 添加处理器，并在 `internal/handler/routes.go` 注册路由。
4. 若前端需要接入，在 `frontend/` 中运行 `bun run api:refresh` 重新导出本地后端的路由与类型
   （契约由 `frontend/scripts/backend-contract.go` 从本仓库的 Go AST 直接导出，见
   [frontend 台账](../../frontend/.ai/sync-diffs/v1.22.0-to-v1.23.0.md)）。

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
[`.github/workflows/release.yml`](.github/workflows/release.yml)）为上述平台提供
`ppanel-server-<os>-<arch>.tar.gz`（Windows 为 `.zip`），每个压缩包附带一个 `.sha256` 校验文件。

## 🧩 fork 与上游的差异

| 方面 | 本 fork | 上游 v1.21.7 |
|---|---|---|
| ORM | entgo（`ent/`、`pkg/orm`） | GORM |
| 节点协议实例 | 多协议实例，以 `type:id` 为键 | 单协议引用 |
| 插件 | 内置 WASM 插件系统（`internal/plugin`、`Plugin` 配置段） | 无 |
| 无限流量 | 支持 `traffic_unlimited` | — |
| Stripe | Checkout Sessions（`CreateCheckoutSession`） | — |
| 订阅渲染 | Edge 订阅签名接口，见 [docs/edge-subscribe.md](docs/edge-subscribe.md) | 无 |
| 配置文件路径 | `config.yaml` | `etc/ppanel.yaml` |
| 配置段 | 有 `Transport`、`Device`、`Plugin`、`EdgeSubscribe`；`MySQL` 为废弃别名 | 有 `AppLocation`、`Redis.QueueDB`、`Database.ConnMaxLifetime/ConnMaxIdleTime` |
| 队列 Redis 库 | 硬编码为 `5`（`queue/queue.go`） | `Redis.QueueDB` 可配置 |

完整说明见[配置指南](doc/config-zh.md)。

## 🤝 贡献

欢迎各种贡献，包括功能开发、错误修复和文档改进。请查看[贡献指南](CONTRIBUTING_ZH.md)了解详情。

## ✨ 特别感谢

感谢以下优秀的开源项目，它们为本项目的开发提供了强大的支持！ 🚀

<div style="overflow-x: auto;">
<table style="width: 100%; border-collapse: collapse; margin: 20px 0;">
  <thead>
    <tr style="background-color: #f5f5f5;">
      <th style="padding: 10px; text-align: center;">项目</th>
      <th style="padding: 10px; text-align: left;">描述</th>
      <th style="padding: 10px; text-align: center;">项目</th>
      <th style="padding: 10px; text-align: left;">描述</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://www.cloudwego.io/zh/docs/hertz/" style="text-decoration: none;">
          <strong>Hertz</strong><br/>
          <img src="https://img.shields.io/github/stars/cloudwego/hertz?style=social" alt="Hertz Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        高性能的 Go HTTP 框架<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://entgo.io/" style="text-decoration: none;">
          <img src="https://entgo.io/images/assets/ent-logo.svg" width="50" alt="ent" style="border-radius: 8px;" /><br/>
          <strong>ent</strong><br/>
          <img src="https://img.shields.io/github/stars/ent/ent?style=social" alt="ent Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        类型安全的 Go 实体框架<br/>
      </td>
    </tr>
    <tr>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://github.com/hibiken/asynq" style="text-decoration: none;">
          <img src="https://user-images.githubusercontent.com/11155743/114697792-ffbfa580-9d26-11eb-8e5b-33bef69476dc.png" width="50" alt="Asynq" style="border-radius: 8px;" /><br/>
          <strong>Asynq</strong><br/>
          <img src="https://img.shields.io/github/stars/hibiken/asynq?style=social" alt="Asynq Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        Go 语言的异步任务队列<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://goswagger.io/" style="text-decoration: none;">
          <img src="https://goswagger.io/go-swagger/logo.png" width="30" alt="Go-Swagger" style="border-radius: 8px;" /><br/>
          <strong>Go-Swagger</strong><br/>
          <img src="https://img.shields.io/github/stars/go-swagger/go-swagger?style=social" alt="Go-Swagger Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        完整的 Go Swagger 工具集<br/>
      </td>
    </tr>
    <tr>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://go-zero.dev/" style="text-decoration: none;">
          <img src="https://raw.githubusercontent.com/zeromicro/zero-doc/main/doc/images/go-zero.png" width="30" alt="Go-Zero" style="border-radius: 8px;" /><br/>
          <strong>Go-Zero</strong><br/>
          <img src="https://img.shields.io/github/stars/zeromicro/go-zero?style=social" alt="Go-Zero Stars" />
        </a>
      </td>
      <td colspan="3" style="padding: 15px; vertical-align: middle;">
        Go 微服务框架（本项目的 API 生成器，基于 Go-Zero 实现）<br/>
      </td>
    </tr>
  </tbody>
</table>
</div>

---

🎉 **致敬开源**：感谢开源社区，让开发变得更简单、更高效！欢迎为这些项目点亮 ⭐，支持开源事业！

## 📄 许可证

本项目采用 [GPL-3.0 许可证](LICENSE) 授权。
