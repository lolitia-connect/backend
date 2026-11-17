# 安装说明

本指南针对本 fork：[lolitia-connect/backend](https://github.com/lolitia-connect/backend)，
它跟随上游 [perfect-panel/server](https://github.com/perfect-panel/server)。发布包在本仓库发布，
配置文件为 `config.yaml`（上游使用 `etc/ppanel.yaml`）——见 [config-zh.md](config-zh.md)。

## 前置系统要求

- 数据库：MySQL 8.0+ / MariaDB，或 PostgreSQL 16+。
- Redis 6.0+（推荐 7.x）。
- 下文的服务安装还需要：带 systemd 的 Linux，以及 `curl`、`tar`、`sha256sum`。发布包也提供 Windows 与
  macOS 版本，启动方式相同（`ppanel-server run --config config.yaml`）。

配置文件说明见 [config-zh.md](config-zh.md)；容器镜像见 [README](../README.md#-docker-部署)。

## 快速安装（Linux，脚本）

[`script/install.sh`](../script/install.sh) 会为当前机器架构（amd64 或 arm64）下载最新发布包，用发布包自带的
`.sha256` 文件校验，安装到 `/opt/ppanel-server`，创建 `ppanel` 系统用户与 systemd 服务并启动：

```shell
curl -fsSL https://raw.githubusercontent.com/lolitia-connect/backend/master/script/install.sh -o install.sh
less install.sh    # 先看清你将以 root 运行的脚本
sudo bash install.sh
```

再次运行会升级二进制并保留 `config.yaml`。无人值守首次启动可在运行前导出 `PPANEL_DB` 与
`PPANEL_REDIS`（见[首次启动](#4-首次启动)）：脚本会把它们写入 `/opt/ppanel-server/config.env`，
由服务单元加载。

## 手动安装

### 1. 下载与校验

发布包地址：<https://github.com/lolitia-connect/backend/releases>。每个版本为各平台提供一个压缩包，
每个压缩包旁边有一个 `.sha256` 文件：

| 平台 | 文件 |
|---|---|
| Linux | `ppanel-server-linux-386.tar.gz`、`ppanel-server-linux-amd64.tar.gz`、`ppanel-server-linux-arm64.tar.gz` |
| Windows | `ppanel-server-windows-386.zip`、`ppanel-server-windows-amd64.zip`、`ppanel-server-windows-arm64.zip` |
| macOS | `ppanel-server-darwin-amd64.tar.gz`、`ppanel-server-darwin-arm64.tar.gz` |

二进制为静态链接（`CGO_ENABLED=0`、`-trimpath`），因此 Linux 压缩包可在同架构的任何发行版上运行，
包括 Alpine。以 Linux amd64 与版本 `v1.3.9` 为例：

```shell
VERSION=v1.3.9
BASE="https://github.com/lolitia-connect/backend/releases/download/$VERSION"
curl -fsSLO "$BASE/ppanel-server-linux-amd64.tar.gz"
curl -fsSLO "$BASE/ppanel-server-linux-amd64.tar.gz.sha256"
sha256sum -c ppanel-server-linux-amd64.tar.gz.sha256
```

`sha256sum` 必须输出 `OK`；校验失败的压缩包不要安装。

压缩包解出的是以文件名命名的目录，内含二进制与 LICENSE：

```
ppanel-server-linux-amd64/
├── ppanel-server       # 二进制
└── LICENSE
```

### 2. 安装文件

服务以专用系统用户运行，该用户只拥有自己的目录：

```shell
id ppanel >/dev/null 2>&1 || sudo useradd --system --home-dir /opt/ppanel-server --no-create-home --shell /usr/sbin/nologin ppanel
sudo mkdir -p /opt/ppanel-server/{logs,cache}
sudo tar -xzf ppanel-server-linux-amd64.tar.gz -C /tmp
sudo install -m 0755 -o root -g root /tmp/ppanel-server-linux-amd64/ppanel-server /opt/ppanel-server/ppanel-server
sudo touch /opt/ppanel-server/config.yaml
sudo chown -R ppanel:ppanel /opt/ppanel-server
sudo chmod 0750 /opt/ppanel-server
sudo chmod 0600 /opt/ppanel-server/config.yaml
```

首次启动后的目录结构：`/opt/ppanel-server/ppanel-server`（二进制）、`config.yaml`（配置，保存 JWT 密钥
与数据库凭据）、`logs/`（`Logger.Path`）、`cache/`（`GeoIP.Path`）。

### 3. 创建 systemd 服务

```shell
sudo tee /etc/systemd/system/ppanel.service > /dev/null <<'EOF'
[Unit]
Description=PPanel Server
Documentation=https://github.com/lolitia-connect/backend/blob/master/doc/install-zh.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ppanel
Group=ppanel
WorkingDirectory=/opt/ppanel-server
# 可选：无人值守首次启动所需的 PPANEL_DB 与 PPANEL_REDIS（权限 0600）。
EnvironmentFile=-/opt/ppanel-server/config.env
ExecStart=/opt/ppanel-server/ppanel-server run --config /opt/ppanel-server/config.yaml
Restart=on-failure
RestartSec=5s
# 停机时会先排空 HTTP，再依次停止调度器、任务 worker 与链路追踪导出。
TimeoutStopSec=25
# 服务只在自己的目录下写入。
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/ppanel-server

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now ppanel
```

- `run` 是启动服务的子命令；直接运行二进制只会打印用法并退出，在 `Restart=always` 下会无限重启。
- `WorkingDirectory` 很重要：日志目录（`Logger.Path`，`logs`）与 GeoIP 数据库（`GeoIP.Path`，
  `./cache/...`）都相对于它。
- `ExecStart` 显式指定了配置文件；`--config` 默认相对于工作目录的 `config.yaml`，在这里是同一个文件。

### 4. 首次启动

配置文件为空（没有 `Database.Addr`）时，服务不会启动 API，而是启动**仅监听 `127.0.0.1` 的安装向导**，
端口为配置的 `Port`（默认 8080），绝不监听公网接口，因此浏览器无法访问
`http://<服务器>:8080/init`。请用以下两种方式之一完成安装。

**方式 A：通过 SSH 隧道使用向导。** 在自己的电脑上执行：

```shell
ssh -L 8080:127.0.0.1:8080 user@your-server
```

然后在浏览器打开 <http://127.0.0.1:8080/init>，填写数据库、Redis 与首位管理员（密码至少 8 个字符）。
向导会测试连接、执行数据库迁移、创建管理员、写入 `config.yaml`（只写管理员邮箱，不写密码），然后交棒给
API 服务，监听 `Host:Port`（默认 `0.0.0.0:8080`）。若在文件写入之后的某步失败，重启服务即可：它会从文件
继续安装。

**方式 B：使用环境变量无人值守。** 通过 `PPANEL_DB` 与 `PPANEL_REDIS` 把连接信息交给首次启动
（格式见 [config-zh.md 第 5 节](config-zh.md#5-首次启动与环境变量)）。服务会在空配置文件中补上它们与
生成的 `JwtAuth.AccessSecret`，然后正常启动：执行迁移并创建首位管理员，生成的密码在日志中打印一次。
配合上面的服务单元：

```shell
sudo install -m 0600 -o ppanel -g ppanel /dev/null /opt/ppanel-server/config.env
sudo tee /opt/ppanel-server/config.env > /dev/null <<'EOF'
PPANEL_DB=ppanel:secret@tcp(127.0.0.1:3306)/ppanel
PPANEL_REDIS=redis://:secret@127.0.0.1:6379/0
EOF
sudo systemctl restart ppanel
sudo journalctl -u ppanel -n 50      # 首位管理员的密码只在这里打印一次
```

PostgreSQL：`PPANEL_DB=postgres://ppanel:secret@127.0.0.1:5432/ppanel?sslmode=require`。
文件中已有密钥后这些变量会被忽略，因此文件可以保留；若不想重复保存凭据也可删除。

### 5. 验证

```shell
systemctl status ppanel
curl -fsS http://127.0.0.1:8080/healthz    # 存活探针：进程与监听器正常
curl -fsS http://127.0.0.1:8080/readyz     # 就绪探针：启动完成、数据库与 Redis 可达（否则 503）
/opt/ppanel-server/ppanel-server healthcheck --config /opt/ppanel-server/config.yaml --timeout 3s
```

`healthcheck` 会向配置的监听地址请求 `/healthz`（`Host` 不为 `0.0.0.0` 时用它，启用 `TLS` 时走 HTTPS），
在 `--timeout` 内没有响应就以非零状态退出；可在看门狗中使用。`/readyz` 返回 `503` 时会在 JSON 中给出原因
（`database unreachable`、`redis unreachable`）。请用首位管理员登录管理面板并修改密码。

## 运维

- **服务**：`systemctl start|stop|restart|status ppanel`；开机自启用 `systemctl enable ppanel`。
- **日志**：`journalctl -u ppanel -f` 查看控制台输出（启动信息、生成的管理员密码）。请求日志在
  `Logger.Mode` 为 `file`（默认）时写入 `/opt/ppanel-server/logs/`。
- **升级**：先备份数据库，按第 1 步下载并校验新压缩包，然后：

  ```shell
  sudo systemctl stop ppanel
  sudo install -m 0755 -o root -g root /tmp/ppanel-server-linux-amd64/ppanel-server /opt/ppanel-server/ppanel-server
  sudo systemctl start ppanel
  ```

  迁移在启动时执行。一旦执行过迁移就不再支持降级：唯一的回滚方式是恢复数据库备份。重新运行
  `script/install.sh` 会执行同样的升级。
- **停机**：`systemctl stop ppanel`（容器用 `docker stop --time 20`）会等待优雅停机：先排空 HTTP，
  再依次停止调度器、任务 worker 与链路追踪导出；请预留约 20 秒再强制结束。
- **从 MySQL 迁移到 PostgreSQL**：`ppanel-server migrate mysql2postgres` 会把 MySQL 数据库复制到空的
  PostgreSQL 数据库。请用 `--location <IANA 时区>`（默认 `Asia/Shanghai`）指明 MySQL `DATETIME` 值
  所存储的时区，以保持每个时间点不变；见
  [tools/mysql2postgres/README.md](../tools/mysql2postgres/README.md)。
- **配置**：`config.yaml` 的每个键都在 [config-zh.md](config-zh.md) 中有说明。该文件保存 JWT 密钥与数据库
  凭据，请保持 `0600` 权限。

## Docker

容器镜像见 [README](../README.md#-docker-部署)。简而言之：镜像以 uid 65532 运行，其 `HEALTHCHECK`
执行 `ppanel healthcheck`；由于安装向导在容器内只监听 `127.0.0.1`，容器部署必须设置 `PPANEL_DB` 与
`PPANEL_REDIS`，或挂载预先填好的 `config.yaml`。使用仓库的 `docker-compose.yml` 时，首次
`docker compose up` 之前 `./config.yaml` 必须以文件形式存在（否则 Docker 会创建同名目录），且对
uid 65532 可写。停止容器请用 `docker stop --time 20`，以便优雅停机完成。

## NGINX 反向代理

以下示例把服务代理到 `api.example.com`。请在这里终止 TLS（例如用 Certbot），而不是在服务内部终止。

```nginx
# 放在 http 段：WebSocket 与 SSE 端点需要。
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    server_name api.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        # 流式响应（订单事件、节点 WebSocket）保持时间比普通请求更长。
        proxy_read_timeout 300s;
        proxy_buffering off;

        add_header X-Cache $upstream_cache_status;
    }
}
```

同机的 nginx 默认被信任：`TrustedProxies` 覆盖回环接口与私有网络，因此无需配置即可让
`X-Forwarded-For` 指向真实客户端。只有当代理具有公网地址时（或为明确起见）才需要列出代理，同时请列出
前端的来源。在 `config.yaml` 中：

```yaml
TrustedProxies: ["127.0.0.1"]                       # 此处可选：同机 nginx 已被默认值覆盖
AllowedOrigins: ["https://user.example.com", "https://admin.example.com"]
```

### Cloudflare

在 Cloudflare 后面，nginx 看到的是 Cloudflare 的地址；访客地址在 `CF-Connecting-IP` 头中。请让 nginx
还原它，使转发的 `X-Real-IP` / `X-Forwarded-For` 指向访客。这需要 `ngx_http_realip_module`
（用 `nginx -V` 检查；发行版软件包已包含）。在 `http` 段中：

```nginx
# cloudflare Start：只信任 Cloudflare 的网段，https://www.cloudflare.com/ips/
set_real_ip_from 173.245.48.0/20;
set_real_ip_from 103.21.244.0/22;
# ... 该页面列出的其余 IPv4 与 IPv6 网段 ...
real_ip_header CF-Connecting-IP;
# cloudflare END
```

不要使用 `set_real_ip_from 0.0.0.0/0`：那会让任何客户端伪造自己的地址。`config.yaml` 中的
`TrustedProxies` 仍只填写 nginx 主机（或保持默认），绝不填写 Cloudflare 的网段。
