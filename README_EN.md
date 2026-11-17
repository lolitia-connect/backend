# PPanel Server

<div align="center">

[![License](https://img.shields.io/github/license/lolitia-connect/backend)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.27%2B-blue)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Available-blue)](Dockerfile)
[![CI/CD](https://img.shields.io/github/actions/workflow/status/lolitia-connect/backend/release.yml)](.github/workflows/release.yml)

**PPanel is a pure, professional, and perfect open-source proxy panel tool, designed for learning and practical use.**

[English](README_EN.md) | [中文](README.md) | [Report Bug](https://github.com/lolitia-connect/backend/issues/new) | [Request Feature](https://github.com/lolitia-connect/backend/issues/new)

> **This repository is a fork of [perfect-panel/server](https://github.com/perfect-panel/server)**
> ([lolitia-connect/backend](https://github.com/lolitia-connect/backend)) that merges upstream release tags
> periodically (see [`.ai/sync-diffs/`](.ai/sync-diffs/)). Compared with upstream it uses entgo instead of GORM,
> supports multi-protocol instances keyed `type:id`, ships a WASM plugin system, unlimited traffic
> (`traffic_unlimited`), Stripe Checkout Sessions and Edge subscribe
> (see [docs/edge-subscribe.md](docs/edge-subscribe.md)). The configuration file is `config.yaml`
> (upstream uses `etc/ppanel.yaml`).

</div>

> **Article 1.**  
> All human beings are born free and equal in dignity and rights.  
> They are endowed with reason and conscience and should act towards one another in a spirit of brotherhood.
>
> **Article 12.**  
> No one shall be subjected to arbitrary interference with his privacy, family, home or correspondence, nor to attacks upon his honour and reputation.  
> Everyone has the right to the protection of the law against such interference or attacks.
>
> **Article 19.**  
> Everyone has the right to freedom of opinion and expression; this right includes freedom to hold opinions without interference and to seek, receive and impart information and ideas through any media and regardless of frontiers.
>
> *Source: [United Nations – Universal Declaration of Human Rights (UN.org)](https://www.un.org/sites/un2.un.org/files/2021/03/udhr.pdf)*

## 📋 Overview

PPanel Server is the backend component of the PPanel project, providing robust APIs and core functionality for managing
proxy services. Built with Go, it emphasizes performance, security, and scalability.

### Key Features

- **Multi-Protocol Support**: Supports Shadowsocks, V2Ray, Trojan, and more.
- **Privacy First**: No user logs are collected, ensuring privacy and security.
- **Minimalist Design**: Simple yet powerful, with complete business logic.
- **User Management**: Full authentication and authorization system.
- **Subscription System**: Manage user subscriptions and service provisioning.
- **Payment Integration**: Supports multiple payment gateways.
- **Order Management**: Track and process user orders.
- **Ticket System**: Built-in customer support and issue tracking.
- **Node Management**: Monitor and control server nodes.
- **API Framework**: Comprehensive RESTful APIs for frontend integration.

## 🚀 Quick Start

### Prerequisites

- **Go**: 1.27 or higher (`go.mod` requires `go 1.27.4`)
- **Database**: MySQL 8.0+ / MariaDB or PostgreSQL 16+, plus Redis 6.0+
- **Docker**: Optional, for containerized deployment
- **Git**: For cloning the repository

Deploying a release binary instead of building? See the [installation guide](doc/install.md)
(`script/install.sh`, systemd unit, checksum verification) and the [configuration guide](doc/config.md).

### Installation from Source

1. **Clone the repository**:
   ```bash
   git clone https://github.com/lolitia-connect/backend.git
   cd backend
   ```

2. **Install dependencies**:
   ```bash
   go mod download
   ```

3. **Generate code**:
   ```bash
   chmod +x script/generate.sh
   ./script/generate.sh
   ```

4. **Build the project** (the binaries land in `bin/`; pick the target of the machine that will run it):
   ```bash
   make linux-amd64
   ```

5. **Run the server**:
   ```bash
  ./bin/ppanel-server-linux-amd64 run --config config.yaml
   ```
   With an empty configuration file the first start serves the setup wizard on `127.0.0.1` at the configured
   `Port` (`http://127.0.0.1:8080/init` by default), reachable from the same machine only. On a remote host open
   an SSH tunnel (`ssh -L 8080:127.0.0.1:8080 user@host`) or set `PPANEL_DB` and `PPANEL_REDIS` for a
   non-interactive installation; both are described in the
   [installation guide](doc/install.md#4-first-start).

### 🐳 Docker Deployment

The image runs as an unprivileged user (uid 65532), listens on port 8080, reads `/app/config.yaml` and reports
its health through `ppanel healthcheck` (`HEALTHCHECK`). **The first-run setup wizard listens on `127.0.0.1`
inside the container and cannot be reached through a published port**, so a container deployment provides the
connections in one of two ways:

- set `PPANEL_DB` and `PPANEL_REDIS`: on the first start the server completes the empty configuration file with
  them and a generated JWT secret, applies the migrations and creates the first administrator, whose password is
  printed once in `docker logs`; or
- mount a pre-filled `config.yaml` ([configuration guide](doc/config.md)).

| Variable | Format | Example |
|---|---|---|
| `PPANEL_DB` | MySQL DSN `user:password@tcp(host:port)/dbname`, or a URL: `mysql://…`, `postgres://user:password@host:5432/dbname?sslmode=require` | `ppanel:secret@tcp(db:3306)/ppanel` |
| `PPANEL_REDIS` | `redis://[:password@]host[:port][/db]` | `redis://:secret@redis:6379/0` |

1. **Build the Docker image**:
   ```bash
   docker buildx build --platform linux/amd64 -t ppanel-server:latest .
   ```

2. **Run the container**. The mounted configuration file must exist and be writable by uid 65532 on the first
   start, when the server completes it:
   ```bash
   touch config.yaml && sudo chown 65532:65532 config.yaml
   docker run -d --name ppanel-server -p 8080:8080 \
     -e PPANEL_DB='ppanel:secret@tcp(db:3306)/ppanel' \
     -e PPANEL_REDIS='redis://:secret@redis:6379/0' \
     -v "$(pwd)/config.yaml:/app/config.yaml" \
     ppanel-server:latest
   docker logs -f ppanel-server   # prints the first administrator's password once
   ```

3. **Use Docker Compose**. The repository's [`docker-compose.yml`](docker-compose.yml) builds the image,
   publishes port 8080, mounts `./config.yaml` and checks the container's health:
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
   `./config.yaml` must exist as a **file** before the first `docker compose up` (Docker creates a directory of
   that name otherwise) and be writable by uid 65532; provide `PPANEL_DB` / `PPANEL_REDIS`, then:
   ```bash
   mkdir -p logs cache && touch config.yaml && sudo chown 65532:65532 config.yaml
   PPANEL_DB='ppanel:secret@tcp(db:3306)/ppanel' PPANEL_REDIS='redis://:secret@redis:6379/0' docker compose up -d
   ```
   Stop the container with `docker stop --time 20` so the graceful shutdown (HTTP first, then the scheduler,
   the task worker and the trace exporter) can finish.

4. **Pull a published image** (after CI/CD publishes):
   ```bash
   docker pull ppanel/ppanel-server:latest
   docker run --rm -p 8080:8080 ppanel/ppanel-server:latest
   ```

The server answers `GET /healthz` (liveness) and `GET /readyz` (readiness: database and Redis reachable) for
monitoring and load balancers; see the [configuration guide](doc/config.md#6-health-checks).

## 📖 API Documentation

Explore the full API documentation:

- **Swagger**: [https://ppanel.dev/en-US/swagger/ppanel](https://ppanel.dev/swagger/ppanel)

The documentation covers all endpoints, request/response formats, and authentication details.

## 🔗 Related Projects

| Project          | Description                | Link                                                  |
|------------------|----------------------------|-------------------------------------------------------|
| PPanel Web       | Frontend for PPanel        | [GitHub](https://github.com/perfect-panel/frontend) |
| PPanel User Web  | User interface for PPanel  | [Preview](https://user.ppanel.dev)                    |
| PPanel Admin Web | Admin interface for PPanel | [Preview](https://admin.ppanel.dev)                   |
| Upstream server  | What this fork tracks      | [GitHub](https://github.com/perfect-panel/server)   |

## 🔒 Security

Report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/lolitia-connect/backend/security/advisories/new), not in public
issues. [SECURITY.md](SECURITY.md) lists this fork's supported versions and what to expect.

## 🌐 Official Website

Visit [ppanel.dev](https://ppanel.dev/) for more details.

## 🏛 Architecture

![Architecture Diagram](./doc/image/architecture-en.png)

## 📁 Directory Structure

```
.
├── apis/             # API definition files
├── cmd/              # Application entry point (run, healthcheck, migrate, version)
├── doc/              # Documentation (config.md, install.md)
├── docs/             # Topic documents (e.g. edge-subscribe.md)
├── config.yaml.example # Example configuration file
├── ent/              # entgo generated ORM client and schemas
├── generate/         # Code generation tools
├── initialize/       # System initialization and database migrations
├── internal/         # Internal modules
│   ├── config/       # Configuration parsing
│   ├── handler/      # HTTP handlers
│   ├── middleware/   # HTTP middleware
│   ├── logic/        # Business logic
│   ├── model/        # Data models
│   ├── plugin/       # WASM plugin system
│   ├── svc/          # Service layer
│   ├── transport/    # HTTP server and health endpoints
│   └── types/        # Type definitions
├── pkg/              # Utility code
├── plugins/          # Plugin directory (WASM binaries are not committed)
├── queue/            # Queue services
├── scheduler/        # Scheduled tasks
├── script/           # Installation and CI helper scripts
├── scripts/          # Performance and maintenance scripts
├── go.mod            # Go module definition
├── Makefile          # Build automation
└── Dockerfile        # Docker configuration
```

## 💻 Development

### Format API Files
```bash
goctl api format --dir apis/user.api
```

### Add a New API

1. Create a new API file in `apis/`.
2. Import it in `apis/ppanel.api`.
3. Regenerate code:
   ```bash
   ./script/generate.sh
   ```

### Build for Multiple Platforms

Use the `Makefile` to build for various platforms (e.g., Linux, Windows, macOS):

```bash
make all  # Builds linux-amd64, darwin-amd64, windows-amd64
make linux-arm64  # Build for specific platform
```

Supported platforms include:

- Linux: `386`, `amd64`, `arm64` (plus `linux-amd64-v3`, `linux-armv5`, `linux-armv6`, `linux-armv7`)
- Windows: `386`, `amd64`, `arm64` (plus `windows-amd64-v3`, `windows-armv7`)
- macOS: `amd64`, `arm64` (plus `darwin-amd64-v3`)

Every binary is static (`CGO_ENABLED=0 -trimpath`), with the version metadata injected by the Makefile into
`pkg/constant.Version` / `pkg/constant.BuildTime`. The release
([`.github/workflows/release.yml`](.github/workflows/release.yml)) publishes
`ppanel-server-<os>-<arch>.tar.gz` (`.zip` for Windows) for those platforms, each with a `.sha256` file.

## 🧩 Fork differences from upstream

| Aspect | This fork | Upstream v1.21.7 |
|---|---|---|
| ORM | entgo (`ent/`, `pkg/orm`) | GORM |
| Node protocol instances | Multi-protocol instances keyed `type:id` | Single protocol reference |
| Plugins | Built-in WASM plugin system (`internal/plugin`, `Plugin` section) | None |
| Unlimited traffic | `traffic_unlimited` supported | — |
| Stripe | Checkout Sessions (`CreateCheckoutSession`) | — |
| Subscription rendering | Signed Edge subscribe API, see [docs/edge-subscribe.md](docs/edge-subscribe.md) | None |
| Configuration path | `config.yaml` | `etc/ppanel.yaml` |
| Configuration sections | Has `Transport`, `Device`, `Plugin`, `EdgeSubscribe`; `MySQL` is a deprecated alias | Has `AppLocation`, `Redis.QueueDB`, `Database.ConnMaxLifetime/ConnMaxIdleTime` |
| Queue Redis database | Hard-coded to `5` (`queue/queue.go`) | Configurable as `Redis.QueueDB` |

Full details: [configuration guide](doc/config.md).

## 🤝 Contributing

Contributions are welcome! Please follow the [Contribution Guidelines](CONTRIBUTING.md) for bug fixes, features, or
documentation improvements.

## ✨ Special Thanks

A huge thank you to the following outstanding open-source projects that have provided invaluable support for this
project's development! 🚀

<div style="overflow-x: auto;">
<table style="width: 100%; border-collapse: collapse; margin: 20px 0;">
  <thead>
    <tr style="background-color: #f5f5f5;">
      <th style="padding: 10px; text-align: center;">Project</th>
      <th style="padding: 10px; text-align: left;">Description</th>
      <th style="padding: 10px; text-align: center;">Project</th>
      <th style="padding: 10px; text-align: left;">Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://www.cloudwego.io/docs/hertz/" style="text-decoration: none;">
          <strong>Hertz</strong><br/>
          <img src="https://img.shields.io/github/stars/cloudwego/hertz?style=social" alt="Hertz Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        High-performance Go HTTP framework<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://entgo.io/" style="text-decoration: none;">
          <img src="https://entgo.io/images/assets/ent-logo.svg" width="50" alt="ent" style="border-radius: 8px;" /><br/>
          <strong>ent</strong><br/>
          <img src="https://img.shields.io/github/stars/ent/ent?style=social" alt="ent Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        Type-safe entity framework for Go<br/>
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
        Asynchronous task queue for Go<br/>
      </td>
      <td align="center" style="padding: 15px; vertical-align: middle;">
        <a href="https://goswagger.io/" style="text-decoration: none;">
          <img src="https://goswagger.io/go-swagger/logo.png" width="30" alt="Go-Swagger" style="border-radius: 8px;" /><br/>
          <strong>Go-Swagger</strong><br/>
          <img src="https://img.shields.io/github/stars/go-swagger/go-swagger?style=social" alt="Go-Swagger Stars" />
        </a>
      </td>
      <td style="padding: 15px; vertical-align: middle;">
        Comprehensive Go Swagger toolkit<br/>
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
        Go microservices framework (this project's API generator is built on Go-Zero)<br/>
      </td>
    </tr>
  </tbody>
</table>
</div>

---

🎉 **Salute to Open Source**: Thank you to the open-source community for making development simpler and more efficient!
Please give these projects a ⭐ to support the open-source movement!
## 📄 License

This project is licensed under the [GPL-3.0 License](LICENSE).
