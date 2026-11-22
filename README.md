# PPanel 服务端

<div align="center">

[![License](https://img.shields.io/github/license/lolitia-connect/backend)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.27%2B-blue)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Available-blue)](Dockerfile)
[![CI/CD](https://img.shields.io/github/actions/workflow/status/lolitia-connect/backend/release.yml)](.github/workflows/release.yml)

**PPanel 是一个纯净、专业、完美的开源代理面板工具，旨在成为您学习和实际使用的理想选择。**

[中文](README.md) | [English](README_EN.md) | [报告问题](https://github.com/lolitia-connect/backend/issues/new) | [功能请求](https://github.com/lolitia-connect/backend/issues/new)

> 本仓库是 [perfect-panel/server](https://github.com/perfect-panel/server) 的
> [lolitia-connect/backend](https://github.com/lolitia-connect/backend) fork，使用 entgo，
> 支持多协议实例、WASM 插件、无限流量、Stripe Checkout Sessions 和 Edge 订阅。
> 开发与兼容边界见[开发指南](doc/development-zh.md)。

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

使用仓库的 [Dockerfile](Dockerfile) 和 [docker-compose.yml](docker-compose.yml)。
容器以 uid 65532 运行；首次启动必须提供 `PPANEL_DB` / `PPANEL_REDIS`，或挂载预填的配置文件。
安装向导仅监听容器内回环地址，不能通过映射端口访问。

构建、配置文件权限、启动、健康检查和优雅停机步骤见[安装指南的 Docker 部分](doc/install-zh.md#docker)。

### Nekoproxy L4 集成

部署支持 `/api/v1/l4/*` Token API 的 Nekoproxy control，然后更新本后端与管理端。后端启动时执行 MySQL/PostgreSQL 的 `02171_nekoproxy_bindings` 迁移；无需改写现有节点表数据。

在管理端「节点管理 → Nekoproxy 连接设置」填写 Nekoproxy 服务地址和 `nkp_` API Token，点击「连接并保存」。地址可使用服务根地址（含反向代理前缀）或完整 `/api/v1/l4` 地址。后端仅调用独立开放 API，不使用 Nekoproxy 面板 JWT 或管理 API。凭据保存在后端系统配置中，不返回浏览器、不写入请求日志；数据库及备份应按密钥存储管理。

节点管理顶部的「Nekoproxy 连接设置」只配置全局连接；新建、绑定和同步请在各节点的表单底部操作。迁移 `02172_nekoproxy_sync_on_save` 为每条绑定增加默认开启的「保存时同步」。手动同步只更新本节点的入口地址和端口。保存时同步会将入口、其余编辑和偏好一起写入；远端失败不会部分保存，关闭开关后可以离线编辑。解除绑定和删除节点均保留远端转发。

迁移 `02173_nekoproxy_managed_target_port` 记录跟随服务器协议的目标端口。请先部署支持 Token 接口 `PATCH /api/v1/l4/services/{id}/target-port` 的 Nekoproxy control。开启保存时同步后，切换协议会更新转发及活跃会话的目标端口，不重新分配入口，也不覆盖手动指定的目标端口。旧绑定仅在目标端口仍与原协议一致时开始跟随。

创建节点可关联现有活跃会话，或同步创建转发并签发入口。新建失败会回滚本地事务并补偿删除已创建的远端服务；若远端补偿失败，错误会显示需要手动清理的转发 ID。请求超时且未收到转发 ID 时，应先检查 Nekoproxy，避免直接重试造成重复创建。使用说明见[节点管理](../frontend/docs/zh/admin/maintenance/nodes.md)。

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

## 📚 文档

- [安装与运维](doc/install-zh.md)：二进制、Docker、初始化、升级和健康检查。
- [配置指南](doc/config-zh.md)与[配置示例](config.yaml.example)。
- [开发指南](doc/development-zh.md)：目录职责、接口/数据层开发、多平台构建和 fork 兼容边界。
- 专项文档：[Edge 订阅](docs/edge-subscribe.md)、[节点 Protobuf 接口](api/server/v1/README.md)、[MySQL → PostgreSQL](tools/mysql2postgres/README.md)。
- 维护者参考：[同步台账](.ai/sync-diffs/README.md)、[开发 agent 指引](.ai/prompts/README.md)。

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
