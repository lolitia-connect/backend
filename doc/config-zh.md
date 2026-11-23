# PPanel 配置指南

本文档说明本 fork（[lolitia-connect/backend](https://github.com/lolitia-connect/backend)，fork 自
[perfect-panel/server](https://github.com/perfect-panel/server)）的配置文件。配置文件为 YAML 格式，
定义服务器、日志、链路追踪、数据库、Redis、邮件、插件系统与首位管理员等相关设置。

若上游记录了本 fork 并未提供的配置项（或相反），文中会明确标注这一分歧。权威来源为
[`config.yaml.example`](../config.yaml.example) 与
[`internal/config/config.go`](../internal/config/config.go)。

## 1. 配置文件概述

- **默认路径**：`./config.yaml`，相对于工作目录。**这是与上游的分歧：** 上游 v1.21.7 使用
  `./etc/ppanel.yaml`。本仓库的 Docker 镜像与 `docker-compose.yml` 挂载的是 `/app/config.yaml`。
- **自定义路径**：通过启动参数 `--config` 指定，例如
  `ppanel-server run --config /opt/ppanel-server/config.yaml`。
- **格式**：YAML 格式，支持注释。
- **权限**：文件保存 JWT 密钥与数据库凭据，请仅对服务用户可读（`chmod 0600`）。服务在文件缺失时以 `0600`
  创建，安装向导以 `0600` 写入，环境变量安装也会重新应用 `0600`。
- **文件缺失或为空**：若 `Database.Addr` 为空且未设置 `PPANEL_DB` / `PPANEL_REDIS`，服务不会启动 API，
  而是启动安装向导（见第 5 节）。

## 2. 配置文件结构

随仓库提供的 [`config.yaml.example`](../config.yaml.example)，并补上
`internal/config/config.go` 中结构体 tag 的默认值。注意示例文件本身已经填入了一套面向生产的配置——
它提供 `AccessSecret: CHANGE_ME_TO_A_RANDOM_SECRET`（必须替换的占位值）、`Driver: postgres`、
`Level: debug` 以及 3.5 节列出的日志大小——因此示例中的值会覆盖这里显示的 tag 默认值。

```yaml
Model: "prod"                       # 设为 "dev" 会把验证码直接放进 API 响应（见 3.12）
Host: "0.0.0.0"                     # 服务监听地址
Port: 8080                          # 服务监听端口
Debug: false                        # 调试模式（关闭 release 模式）

# 为空时信任回环与私有网络地址；使用 ["none"] 忽略转发头。
TrustedProxies: []
# 为空时保持宽松 CORS；填入确切的浏览器来源即启用白名单。
AllowedOrigins: []

HTTP: # 监听器限制
  ReadTimeoutSeconds: 180           # 读取请求的时间；0 表示不限制
  WriteTimeoutSeconds: 0            # 写入响应的时间；0 表示不限制（流式接口需要）
  IdleTimeoutSeconds: 180           # keep-alive 连接的空闲关闭时间
  MaxRequestBodyMB: 4               # 接受的最大请求体（MB）

GeoIP: # MaxMind GeoLite2 城市库，用于登录与审计日志的归属地
  Path: "./cache/GeoLite2-City.mmdb"
  Download: true                    # Path 缺失或 SHA256 校验失败时下载
  DownloadURL: ""                   # 为空使用内置数据库地址
  SHA256: ""                        # 可选的 64 位十六进制 SHA-256 摘要
  Required: false                   # true 表示没有有效数据库时拒绝启动

Transport:
  Driver: "hertz"                   # fork 专有：监听器实现

TLS: # 由服务自身提供 HTTPS（通常交给反向代理）
  Enable: false
  CertFile: ""
  KeyFile: ""

JwtAuth: # JWT 认证配置
  AccessSecret: ""                  # 访问令牌密钥（必填，见 3.4）
  AccessExpire: 604800              # 访问令牌过期时间（秒）

Logger: # 日志配置
  ServiceName: "PPanel"             # 日志服务标识名称
  Mode: "file"                      # 日志输出模式（console、file、volume）
  Encoding: "json"                  # 日志格式（json、plain）
  TimeFormat: "2006-01-02 15:04:05.000"  # 自定义时间格式
  Path: "logs"                      # 日志文件目录
  FilePath: ""                      # 直接指定日志文件路径（可选）
  Color: true                       # 控制台彩色输出
  Level: "info"                     # 日志级别（debug、info、error、severe）
  MaxContentLength: 0               # 截断过长的日志行（0 表示不限制）
  Compress: false                   # 是否压缩日志文件
  Stat: true                        # 是否统计日志
  KeepDays: 0                       # 日志保留天数
  MaxAge: 0                         # 日志保留天数（旧别名）
  StackCooldownMillis: 100          # 堆栈日志冷却时间（毫秒）
  MaxBackups: 0                     # 最大日志备份数
  MaxBackup: 0                      # 最大日志备份数（旧别名）
  MaxSize: 0                        # 最大日志文件大小（MB）
  Rotation: "daily"                 # 日志轮转策略（daily、size）
  FileTimeFormat: "2006-01-02T15:04:05.000Z07:00"

Trace: # OpenTelemetry 链路追踪
  Name: "ppanel"                    # 记录到链路中的服务名
  Endpoint: ""                      # 采集端地址；为空则不上报
  Sampler: 1.0                      # 采样比例
  Batcher: "jaeger"                 # jaeger、otlpgrpc、otlphttp、zipkin 或 file
  OtlpHeaders: {}                   # OTLP 导出器的额外请求头
  OtlpHttpPath: "/v1/traces"        # Endpoint 为 host:port 时 otlphttp 的路径
  OtlpHttpSecure: false             # Endpoint 为 host:port 时 otlphttp 使用 HTTPS
  OtlpGrpcSecure: false             # otlpgrpc 导出器的 TLS；false 表示明文
  Disabled: false                   # true 表示完全关闭链路追踪

Database:
  Driver: "postgres"                # mysql 或 postgres（结构体 tag 默认 mysql；示例提供 postgres）
  Addr: ""                          # 数据库地址（必填）
  Username: ""                      # 数据库用户名（必填）
  Password: ""                      # 数据库密码（必填）
  Dbname: ""                        # 数据库名（必填）
  Config: "sslmode=disable&TimeZone=UTC"  # 数据库统一以 UTC 存储；PostgreSQL；MySQL 用 charset=utf8mb4&parseTime=true&loc=UTC
  MaxIdleConns: 10                  # 最大空闲连接数
  MaxOpenConns: 10                  # 最大打开连接数
  SlowThreshold: 1000               # 慢查询阈值（毫秒）

# MySQL: 已废弃。仅在 Database 为空时读取，写入时会被迁移移除。
#   字段与 Database 相同，请改用 Database。

Redis:
  Host: "localhost:6379"            # Redis 地址
  Pass: ""                          # Redis 密码
  DB: 0                             # 缓存、会话与限流使用的库
  PoolSize: 100                     # 连接池最大连接数
  MinIdleConns: 10                  # 保持的最小空闲连接数
  MaxRetries: 3                     # 网络错误时的重试次数
  PoolTimeout: 4                    # 等待空闲连接的秒数
  IdleTimeout: 300                  # 空闲连接回收秒数
  MaxConnAge: 0                     # 连接最大生命周期（秒，0 表示不限制）
  DialTimeout: 5                    # 建立连接超时（秒）
  ReadTimeout: 3                    # 读超时（秒）
  WriteTimeout: 3                   # 写超时（秒）

Administrator: # 首位管理员，首次启动时创建
  Email: "admin@ppanel.dev"         # 管理员登录邮箱
  Password: ""                      # 管理员登录密码；为空则自动生成

# 以下配置段存在于配置模型中，但不在 config.yaml.example 里；
# 通常通过面板在运行时管理。
Site: { Host: "", SiteName: "", SiteDesc: "", SiteLogo: "", Keywords: "", CustomHTML: "", CustomData: "" }
Node: { NodeSecret: "", NodePullInterval: 60, NodePushInterval: 60, TrafficReportThreshold: 0, IPStrategy: "", DNS: [], Block: [], Outbound: [] }
Mobile: { Enable: true, Platform: "", PlatformConfig: "", EnableVerify: false, EnableWhitelist: false, Whitelist: [] }
Email: { Enable: true, Platform: "", PlatformConfig: "", EnableVerify: false, EnableNotify: false }
Device: { enable: true, show_ads: false, enable_security: false, only_real_device: false, security_secret: "" }
Verify: { TurnstileSiteKey: "", TurnstileSecret: "", LoginVerify: false, RegisterVerify: false, ResetPasswordVerify: false }
VerifyCode: { ExpireTime: 300, Limit: 15, Interval: 60 }
Register: { StopRegister: false, EnableTrial: false, TrialSubscribe: 0, TrialTime: 0, TrialTimeUnit: "", IpRegisterLimit: 0, IpRegisterLimitDuration: 0, EnableIpRegisterLimit: false, DeviceLimit: 5 }
Subscribe: { SingleModel: false, SubscribePath: "/v1/subscribe/config", SubscribeDomain: "", PanDomain: false, UserAgentLimit: false, UserAgentList: "", ShowTutorial: true, ProfileUpdateInterval: 0, ProfileWebPageURL: "" }
EdgeSubscribe: { Enabled: false, MaxClockSkewSeconds: 0, Keys: [] }
Invite: { ForcedInvite: false, ReferralPercentage: 0, OnlyFirstPurchase: false }
Telegram: { Enable: false, BotID: 0, BotName: "", BotToken: "", EnableNotify: false, WebHookDomain: "" }
Log: { AutoClear: true, ClearDays: 7 }
Currency: { Unit: "USD", Symbol: "$", AccessKey: "" }
Plugin: { Enabled: true, Directory: "plugins", MaxMemoryMB: 64, TimeoutSec: 30, AllowList: [], BlockList: [] }
```

文件顶部的 `Model` 键见第 3.12 节。

`Log.AutoClear` 控制流量明细自动清理，`Log.ClearDays` 指定保留天数。每日统计提交成功后，
系统会投递独立清理任务，按固定截止时间每批最多删除 1000 条过期明细；每批独立提交，
清理失败不会回滚统计。统计和清理均只使用 Asynq 的任务重试，不在任务内部重试数据库错误。
同一天的统计通过任务锁和已提交汇总避免重复写入，清理任务使用全局任务锁避免多个实例并发清理。
关闭 `AutoClear` 也会跳过已经排队的清理任务。

## 3. 配置项说明

### 3.1 服务器设置

- **`Host`**：服务监听地址。默认 `0.0.0.0`。它只是绑定地址；支付回调等公开链接由站点域名
  （`Site.Host`，3.11）或支付方式自身的域名构建，OAuth 跳转则被限制在配置的站点来源上。
- **`Port`**：服务监听端口。默认 `8080`。容器镜像以非特权用户（uid 65532）运行，无法绑定 1024 以下的
  端口：请用端口映射（`-p 443:8080`）而不是修改 `Port`。
- **`Debug`**：是否开启调试模式。默认 `false`。关闭时 Hertz 运行在 release 模式；开启时会关闭 release
  模式并禁用后台日志。
- **`TrustedProxies`**：其 `X-Forwarded-For`、`X-Real-IP` 头可信的反向代理，填写 IP 或 CIDR，例如
  `["127.0.0.1", "10.0.0.0/8"]`。
  - 默认：回环接口与私有网络（`127.0.0.0/8`、`::1/128`、`10.0.0.0/8`、`172.16.0.0/12`、
    `192.168.0.0/16`、`fc00::/7`），因此同机 nginx、Docker 网桥或内网负载均衡无需配置即被信任。
    一旦配置，该列表会替代默认值；条目 `private` 代表上述默认网段（`["private", "203.0.113.10"]`
    表示额外增加一个公网代理），`["none"]` 表示完全不信任任何转发头，适用于客户端直连的服务。
  - 客户端地址用于登录与审计日志，以及所有按 IP 的限流（验证码、设备登录、可用性检测、订阅拉取、
    注册）。只应填写代理地址，绝不填写客户端地址：类似 `0.0.0.0/0` 的条目会让任何人都能伪造被记录的地址。
- **`AllowedOrigins`**：CORS 允许的浏览器来源，格式 `scheme://host[:port]`，例如
  `["https://user.example.com", "https://admin.example.com"]`。
  - 默认：为空，保持反射请求 `Origin` 的宽松行为：任何网站的脚本都可以从浏览器调用 API。
  - 配置后，命中的来源（大小写不敏感地精确匹配）会收到 CORS 响应头且允许携带凭据；未命中的来源完全不会
    收到 CORS 响应头，浏览器会拦截该请求。设备 WebSocket（`/v1/app/ws/...`）对浏览器客户端同样适用该列表。
  - 不带 `Origin` 头的请求（客户端、节点、订阅）不受影响。
- **`HTTP`**：API 监听器的限制。
  - **`ReadTimeoutSeconds`**：读取请求（含头部与正文）的时间。默认 `180`；`0` 表示不限制。
  - **`WriteTimeoutSeconds`**：写入响应的时间。默认 `0`（不限制），流式接口（订单事件 SSE、节点
    WebSocket）需要如此；在此设置上限会中断它们。
  - **`IdleTimeoutSeconds`**：keep-alive 连接的空闲关闭时间。默认 `180`。
  - **`MaxRequestBodyMB`**：接受的最大请求体（MB）；超过的请求会在读取前被拒绝。默认 `4`。
- **`Transport.Driver`**：监听器实现。默认 `hertz`。**fork 专有：** 上游 v1.21.7 没有 `Transport`
  配置段。注意：服务目前不会读取该值——`internal/config` 只是声明了它，没有任何分支依赖它——因此它会被接受
  但被忽略。此处记录它是因为它属于文件 schema，而非因为它会改变行为。

### 3.2 GeoIP (`GeoIP`)

MaxMind GeoLite2 城市库为登录与审计日志提供地址归属地。没有它服务照常运行，只是仅记录地址。

- **`Path`**：`GeoLite2-City.mmdb` 文件路径，相对于工作目录。默认 `./cache/GeoLite2-City.mmdb`。
  容器镜像已提供可写的 `/app/cache`。（`NewIPLocation` 还有一个从内置镜像下载到该默认路径的硬编码回退；
  服务启动时实际使用的是可配置路径 `OpenIPLocation`。）
- **`Download`**：当 `Path` 处的文件不存在时在启动时下载。默认 `true`。
  - **分歧：** 上游还会获取 ASN 数据库（`GeoLite2-ASN.mmdb`），并说明它与城市库同目录。本 fork 的
    `internal/svc/mmdb.go` **只处理城市库**，不会下载或读取任何 ASN 文件。
  - 上游会重新下载已存在但 SHA-256 不匹配的文件。本 fork **仅在文件缺失时下载**：已存在但摘要不符的文件
    不会被重新获取，而是由下面的 `SHA256` 校验判为失败。
- **`DownloadURL`**：城市库的下载地址。默认空，使用内置镜像（发布在 GitHub 上的 GeoLite2-City 副本）。
  可指向自建副本，或使用带授权码的 MaxMind 下载链接，以控制服务加载的内容。
- **`SHA256`**：城市库必须具备的十六进制 SHA-256 摘要。默认空，不校验。设置后会对实际打开的文件进行校验；
  下载文件不匹配时永远不会替换现有文件（下载写入临时文件，校验通过后才重命名就位）。当 `DownloadURL`
  指向自己控制的文件时请设置。
- **`Required`**：城市库缺失或无效时直接导致启动失败。默认 `false`：服务在无归属地的情况下启动并记录警告。

### 3.3 TLS (`TLS`)

由服务自身提供 HTTPS。

- **`Enable`**：默认 `false`。常见部署在反向代理处终止 TLS，此时应把代理填入 `TrustedProxies`。
- **`CertFile`**、**`KeyFile`**：PEM 证书链与私钥路径，需对服务用户可读。证书无效会导致启动失败。

`healthcheck` 子命令会遵循这些设置（见第 6 节）。

### 3.4 JWT 认证 (`JwtAuth`)

- **`AccessSecret`**：访问令牌的密钥。会话、订单事件票据与访客结账签名都由它派生，密钥为空会让任何人都能伪造。
  - 必填：为空时服务在启动时 panic（`JwtAuth.AccessSecret must not be empty`）。请使用足够长的随机值。
  - 只有全新安装才会自动生成：当配置文件中没有密钥时，安装向导（或 `PPANEL_DB` / `PPANEL_REDIS`
    环境变量，见第 5 节）会把生成的密钥写入文件。已预填的文件必须自带密钥。
- **`AccessExpire`**：令牌过期时间（秒）。默认 `604800`（7 天）。

### 3.5 日志配置 (`Logger`)

- **`ServiceName`**：日志的服务标识名称；在 `volume` 模式下用作日志目录名。默认 `PPanel`。
- **`Mode`**：日志输出方式：`console`（标准输出/错误输出）、`file`（写入指定目录）、`volume`（Docker 卷）。
  默认 `file`。
- **`Encoding`**：日志格式：`json`（结构化）或 `plain`（纯文本，带颜色）。默认 `json`。
- **`TimeFormat`**：日志时间格式。默认 `2006-01-02 15:04:05.000`。
- **`Path`**：日志文件目录，默认 `logs`，相对于工作目录。`access.log` 写在这里；
  `logging.ReadLastNLines` 会读取它。
- **`FilePath`**：需要单个日志文件而非目录时的显式文件路径。默认空。
- **`Color`**：控制台彩色输出。默认 `true`。
- **`Level`**：日志过滤级别。默认 `info`。
  - `debug` → debug 及以上；`error` → error 及以上；`severe` / `fatal` / `alert` → fatal 及以上，
    这等于静默，因为服务不会写入 `error` 以上的条目。
  - **其他任何值（包括 `info`、`warn`、`panic`）都会映射为 `info`。**
    `config.yaml.example` 的注释里列出了 `warn` 与 `panic`，但
    `pkg/logging/logging.go` 的 `level()` 没有对应分支：它们的行为等同于 `info`，而不是"仅警告"或
    "仅 panic"过滤器。想减少日志量请用 `error`。
  - `config.yaml.example` 提供 `Level: debug`。
- **`MaxContentLength`**：截断过长的日志行；`0` 表示不限制。默认 `0`。
- **`Compress`**：是否压缩日志文件（仅在 `file` 模式生效）。默认 `false`。
- **`Stat`**：是否统计日志。默认 `true`。
- **`KeepDays`** / **`MaxAge`**：日志保留天数，`MaxAge` 为旧别名。默认 `0`。
  **分歧：** 上游默认为 `KeepDays: 30`；本 fork 默认为 `0`（不自动清理），除非文件另行设置。
  `config.yaml.example` 提供 `MaxAge: 30`。
- **`StackCooldownMillis`**：堆栈日志冷却时间（毫秒），防止日志过多。默认 `100`。
- **`MaxBackups`** / **`MaxBackup`**：最大日志备份数（`size` 轮转时生效）。默认 `0`。
  **分歧：** 上游默认为 `30`；`config.yaml.example` 提供 `MaxBackup: 3`。
- **`MaxSize`**：日志文件最大大小（MB，`size` 轮转时生效）。默认 `0`。**分歧：** 上游默认为 `100`；
  `config.yaml.example` 提供 `50`。
- **`Rotation`**：日志轮转策略：`daily` 或 `size`。默认 `daily`。
- **`FileTimeFormat`**：日志文件名使用的时间戳格式。默认 `2006-01-02T15:04:05.000Z07:00`。

> 上述默认值来自 `pkg/logging/logging.go` 的结构体 tag。`config.yaml.example` 设置的是一套面向生产的
> 配置（`FilePath: logs/ppanel.log`、`MaxSize: 50`、`MaxBackup: 3`、`MaxAge: 30`、`Compress: true`、
> `Level: debug`、`Color: true`）；文件中的值始终优先于 tag 默认值。

常规的 `HTTP Request` 日志只写入本地日志。启用后端 Sentry Tracing 后，可在 Sentry Traces 中查看请求。`HTTP Error` 和其他日志仍按后端 Sentry 设置上报。

### 3.6 链路追踪 (`Trace`)

对 HTTP 请求、数据库调用与队列任务的 OpenTelemetry 追踪。`Endpoint` 未设置时不会上报任何数据。

- **`Name`**：记录到链路中的服务名（`service.name`）。默认 `ppanel`。
- **`Endpoint`**：batcher 上报 span 的目标；为空则不上报。
  - `jaeger` batcher 使用 Jaeger 的 OTLP/HTTP 接收端，可以是 `http://jaeger:4318` 这样的 URL
    （除非 URL 指定了其他路径，否则提交到 `/v1/traces`；scheme 决定是否启用 TLS），也可以是裸的
    `host:port`。
  - `otlpgrpc` 填采集端的 gRPC 地址（`host:4317`）；`otlphttp` 填 URL 或 `host:port`
    （见 `OtlpHttpPath`、`OtlpHttpSecure`）；`zipkin` 填 Zipkin API URL；`file` 填文件路径。
- **`Sampler`**：保留的链路比例，`0` 到 `1`。默认 `1.0` —— **分歧：** 上游默认为 `0.1`；
  `config.yaml.example` 提供 `1.0`。
- **`Batcher`**：导出器：`jaeger`、`zipkin`、`otlpgrpc`、`otlphttp` 或 `file`。默认 `jaeger`。
- **`OtlpGrpcSecure`**：`otlpgrpc` 导出器的传输安全。默认 `false`：到采集端的 gRPC 连接不启用 TLS。
  当采集端提供 TLS 时设为 `true`；由系统根证书校验，客户端不会静默降级为明文。`otlphttp` 则通过 URL
  scheme 或 `OtlpHttpSecure` 选择 TLS。
- **`OtlpHeaders`**：随每次 OTLP 上报发送的请求头，例如托管采集端的 `uptrace-dsn` 或 `Authorization`。
- **`OtlpHttpPath`**：`Endpoint` 为裸 `host:port` 时 OTLP/HTTP 接收端的路径。
  `config.yaml.example` 提供 `/v1/traces`。
- **`OtlpHttpSecure`**：`Endpoint` 为裸 `host:port` 时 `otlphttp` 是否使用 HTTPS。默认 `false`。
- **`Disabled`**：完全关闭链路追踪。默认 `false`。

### 3.7 数据库 (`Database`)

- **`Driver`**：数据库方言：`mysql`（MySQL 8 与 MariaDB）或 `postgres`。结构体 tag 的默认值为
  `mysql`。**`config.yaml.example` 提供的是 `postgres`**（并配套 PostgreSQL 的 `Config`），因此
  直接照抄示例而不修改 `Driver` 会以 PostgreSQL 驱动连接。请让 `Driver` 与 `Config` 都匹配实际数据库。
- **`Addr`**、**`Username`**、**`Password`**、**`Dbname`**：连接信息，均必填。
- **`Config`**：方言相关的连接参数。
  - MySQL 默认：`charset=utf8mb4&parseTime=true&loc=UTC`。
  - PostgreSQL 示例：`sslmode=disable&TimeZone=UTC`。
  - **时区口径：** 数据库统一以 UTC 存储；连接层**强制** `loc=UTC` / `TimeZone=UTC`（不受 `Config` 中时区设置影响），应用读出后按进程本地时区 `time.Local` 呈现。升级旧库请执行迁移 `02170_localtime_to_utc`（见下）。
  - **分歧：** 上游的 PostgreSQL 默认值是
    `sslmode=prefer&TimeZone=<AppLocation>&application_name=perfect-panel`，并会自动注入 `TimeZone`。
    本 fork 没有 `AppLocation` 配置项，也不会改写 `Config`；请自行设置时区。跨网络访问数据库请使用
    `sslmode=require` 或 `verify-full`；`sslmode=disable` 仅适用于同机或私有网络内的数据库。
- **`MaxIdleConns`**：最大空闲连接数。默认 `10`。
- **`MaxOpenConns`**：最大打开连接数。默认 `10`。
  **分歧：** 上游还有 `ConnMaxLifetime`（`1800`）与 `ConnMaxIdleTime`（`300`）；本 fork 的
  `pkg/orm.Config` 未定义这两个字段，因此不会被读取。
- **`SlowThreshold`**：慢查询阈值（毫秒）。默认 `1000`。
- **`MySQL`**：**已废弃**，仅为兼容保留。仅当 `Database` 未给出连接信息时读取，且服务在写回配置时会将其
  从文件中移除（`SetDatabaseConfig` 会清空它）。请改用 `Database`。

### 3.8 Redis 配置 (`Redis`)

- **`Host`**：Redis 服务器地址。默认 `localhost:6379`。
- **`Pass`**：Redis 密码。默认 `""`（无密码）。
- **`DB`**：缓存、会话与限流使用的 Redis 库编号。默认 `0`。
- **`PoolSize`**：连接池最大连接数。默认 `100`。
- **`MinIdleConns`**：保持的最小空闲连接数。默认 `10`。
- **`MaxRetries`**：网络错误时的重试次数。默认 `3`。
- **`PoolTimeout`**：等待空闲连接的秒数。默认 `4`。
- **`IdleTimeout`**：空闲连接回收秒数。默认 `300`。
- **`MaxConnAge`**：连接最大生命周期（秒）；`0` 表示不限制。默认 `0`。
- **`DialTimeout`**：建立连接超时（秒）。默认 `5`。
- **`ReadTimeout`**：读超时（秒）。默认 `3`。
- **`WriteTimeout`**：写超时（秒）。默认 `3`。

> **分歧：** 上游有 `Redis.QueueDB`（默认 `5`）用于选择任务队列的库。本 fork 的 `RedisConfig`
> 没有 `QueueDB` 字段；队列库**在 `queue/queue.go` 中硬编码为 `5`**
> （`asynq.RedisClientOpt{..., DB: 5}`）。因此共用同一 Redis 的部署必须让 `DB` 不等于 `5`，
> 否则一个面板会消费另一个面板的任务。将其变为可配置属于代码改动，而非配置改动。

### 3.9 邮件投递 (`Email` 与 SMTP)

`Email` 配置段保存通知邮件使用的模板与主题：

- **`Enable`**：是否启用邮件投递。默认 `true`。
- **`Platform`**、**`PlatformConfig`**：服务商选择及其 JSON 配置。
- **`EnableVerify`**、**`EnableNotify`**、**`EnableDomainSuffix`**、**`DomainSuffixList`**：
  验证、通知与域名后缀策略。
- **`VerifyEmailTemplate`**、**`ExpirationEmailTemplate`**、**`MaintenanceEmailTemplate`**、
  **`TrafficExceedEmailTemplate`**：模板。
- **`VerifyEmailSubject`**、**`ExpirationEmailSubject`**、**`MaintenanceEmailSubject`**、
  **`TrafficExceedEmailSubject`**：主题。

SMTP 中继设置保存在数据库中（系统设置 → 邮件），在面板中编辑。`pkg/email/smtp` 读取以下字段：

| 字段 | 含义 |
|---|---|
| `host`、`port` | 中继地址。465 端口为隐式 TLS（SMTPS）；25、587、2525 端口以明文开始并通过 STARTTLS 升级。 |
| `user`、`pass` | 中继凭据；留空表示不认证发送。 |
| `from`、`reply_to` | 发件地址，以及可选的回复地址。 |
| `siteName` | 邮件中使用的显示名称。 |
| `ssl` | 强制加密。465 端口（或设置了 `implicit_tls`）时连接即开始 TLS；其他端口则必须通过 STARTTLS 升级，不支持 STARTTLS 的中继会在发送凭据前被拒绝。 |
| `implicit_tls` | 在非 465 端口上以 TLS 握手开始连接。仅用于在非标准端口提供 SMTPS 的中继；绝不可用于 STARTTLS 端口。 |
| `insecure_skip_verify` | 接受任意中继证书。仅用于自签名证书的中继；默认校验证书。 |

### 3.10 首位管理员 (`Administrator`)

用于创建首位管理员：每次启动时，若数据库中没有管理员，就用这里的值创建一个。

- **`Email`**：管理员登录邮箱。默认 `admin@ppanel.dev`。
- **`Password`**：管理员登录密码。默认空。为空时会生成随机密码并在启动日志中打印一次
  （`docker logs`、`journalctl -u ppanel`），请用该密码登录后修改。已配置的值按原样使用。
  安装向导只写入安装者填写的邮箱，从不写入密码（`cfg.Administrator.Password = ""`）。

### 3.11 其他配置段

这些配置段存在于 `internal/config/config.go`；`config.yaml.example` 未列出它们，因为由面板在运行时管理。
默认值见第 2 节。

- **`Site`**：`Host`（面板的公开 URL；支付回调由它构建，Apple/Telegram OAuth 跳转也被限制在它上面）、
  `SiteName`、`SiteDesc`、`SiteLogo`、`Keywords`、`CustomHTML`、`CustomData`。
- **`Node`**：`NodeSecret`、拉取/推送间隔、`TrafficReportThreshold`、`IPStrategy`、`DNS`、`Block`、
  `Outbound`。
- **`Mobile`**、**`Device`**、**`Verify`**、**`VerifyCode`**、**`Register`**、**`Log`**、
  **`Currency`**、**`Invite`**、**`Telegram`**：客户端、设备准入、验证码、注册、日志保留、货币与邀请设置。
- **`Subscribe`**：订阅下发设置，其中 `ProfileUpdateInterval` 与 `ProfileWebPageURL` 会下发给自行
  刷新订阅配置的客户端（`0` / 空值表示响应中省略该字段）。
- **`Plugin`**：**fork 专有的插件系统。** `Enabled`（默认 `true`）、`Directory`（默认 `plugins`）、
  `MaxMemoryMB`（默认 `64`）、`TimeoutSec`（默认 `30`）、`AllowList`（空表示允许全部插件）、
  `BlockList`。WASM 插件二进制被 git 忽略（`plugins/**/*.wasm`），因为它们在别处构建。
- **`EdgeSubscribe`**：**fork 专有的签名 Edge Manifest API**（`GET /api/edge/v1/manifest`），
  由可信的 Cloudflare Worker 消费。默认关闭，因为启用后会暴露一个可签发用户凭据的 HMAC 认证端点。
  - **`Enabled`**：默认 `false`。
  - **`MaxClockSkewSeconds`**：请求时间戳允许的时钟偏差。
  - **`Keys`**：`{ID, Secret}` HMAC 凭据列表。轮换是增量的：先加新密钥，部署使用它的 Worker，
    再删除旧密钥。
  - 协议详见 [`docs/edge-subscribe.md`](../docs/edge-subscribe.md)。

### 3.12 运行模式 (`Model`)

- **`Model`**：默认 `"prod"`。它在运行时读取，而非构建时。
  - `Config.Model` 会与 `"dev"`（`constant.DevMode`）比较：验证码相关路径
    （`sendSmsCodeLogic`、`sendEmailCodeLogic` 以及队列中的短信任务）在 dev 模式下会把生成的验证码
    **直接放进 API 响应**，而不只是发送出去。
  - **开发环境以外请保持 `prod`。** 在任何人可访问的部署上设置 `Model: dev` 等于把验证码交给调用方。
  - `config.yaml.example` 有意不列出该键。

## 4. 未体现在配置文件中的 fork 专有行为

- **使用 entgo 而非 GORM。** 持久层为 `entgo.io/ent`；生成的客户端位于 `ent/`，由 `pkg/orm` 打开连接。
  上游 v1.21.7 使用 GORM。配置文件不因此变化，但 [`initialize/migrate`](../initialize/migrate)
  中的迁移说明适用。
- **多协议实例以 `type:id` 为键。** 一台服务器可承载多个协议实例；节点引用是 `type:id` 而非裸 id。
  这属于数据模型差异，不是配置差异。
- **无限流量（`traffic_unlimited`）。** 订阅可标记为无限流量，此时流量计算忽略配额。该标记按订阅存储，
  由面板设置，而非本文件。
- **Stripe Checkout Sessions。** Stripe 支付方式使用托管的 Checkout Session
  （`CreateCheckoutSession`），包括异步成功处理。凭据在面板中按支付方式配置。
- **EPay V1/V2。** EPay 支付方式同时支持两个协议版本；V2 API 端点可使用站点相对路径。在面板中按支付方式配置。

## 5. 首次启动与环境变量

配置文件为空（没有 `Database.Addr`）时，服务不会启动 API，而是启动**仅监听 `127.0.0.1` 的安装向导**，
端口为配置的 `Port`（默认 8080），绝不监听公网接口，因此其他机器上的浏览器无法访问
`http://<服务器>:8080/init`。远程主机需要建立 SSH 隧道：

```shell
ssh -L 8080:127.0.0.1:8080 user@your-server
# 然后在浏览器打开 http://127.0.0.1:8080/init
```

向导会测试连接、执行迁移、创建管理员（密码至少 8 个字符）、以 `0600` 写入配置文件，然后交棒给 API 服务。
若在文件写入之后的某步失败，重启服务即可：它会从文件继续安装。

容器与无人值守安装可用两个环境变量在首次启动时补全空的配置文件：

| 环境变量 | 配置项 | 格式 | 示例 |
|---|---|---|---|
| `PPANEL_DB` | `Database` | MySQL DSN `user:password@tcp(host:port)/dbname[?params]`，或 URL：`mysql://user:password@host:3306/dbname`、`postgres://user:password@host:5432/dbname[?sslmode=require]` | `ppanel:secret@tcp(127.0.0.1:3306)/ppanel` |
| `PPANEL_REDIS` | `Redis` | `redis://[:password@]host[:port][/db]`（省略时端口为 `6379`、库为 `0`） | `redis://:secret@127.0.0.1:6379/0` |

- 两者必须同时设置，且仅在配置文件（默认 `config.yaml` 或 `--config` 指定的文件）中没有
  `JwtAuth.AccessSecret` 时才会被读取；文件中已有密钥后它们会被忽略。
- 服务会把生成的密钥、数据库连接与 Redis 连接写回文件，并**保留文件中已有的其他所有设置**
  （`Host`、`Port`、`TLS`、`Logger`、`Trace`、`EdgeSubscribe` 等），因为它是在原 YAML 文档上就地修改。
  已废弃的 `MySQL` 键会被移除。
- 文件必须对服务运行所用的用户可写；容器镜像以 uid 65532 运行。

## 6. 健康检查

| 端点 | 含义 | 响应 |
|---|---|---|
| `GET /healthz` | 存活探针：进程与监听器正常。 | `200` |
| `GET /readyz` | 就绪探针：启动已完成且数据库与 Redis 可达（探测结果缓存 5 秒）。 | `200`，或 `503` 加 `{"status":"unavailable","reason":"…"}`，reason 为 `database unreachable`、`redis unreachable`，或在资源从未建立时为 `database unavailable` / `redis unavailable` |

两者都无需认证，除原因外不泄露任何信息，因此可以暴露给监控与负载均衡；请把负载均衡的健康检查指向
`/readyz`。安装向导运行期间不会提供这两个端点。

`ppanel-server healthcheck [--config config.yaml] [--timeout 3s]` 从配置文件读取监听设置并向其请求
`/healthz`：`127.0.0.1:<Port>`，当 `Host` 不是 `0.0.0.0`/`::` 时为 `<Host>:<Port>`，`TLS.Enable`
开启时使用 HTTPS。它不跟随重定向。若在超时内没有拿到健康响应，它会以非零状态退出。容器镜像的
`HEALTHCHECK` 会运行它，systemd 或 cron 看门狗也可以。

## 7. 最佳实践

- **安全性**：首次登录后请修改首位管理员的密码。若管理员已存在而文件中仍保留 `Administrator.Password`，
  该凭据已无人需要，请删除。
- **反向代理**：同机或私有网络内的代理默认被信任；公网地址的代理需填入 `TrustedProxies`；客户端直连的
  服务应设置 `TrustedProxies: ["none"]`。请把 `AllowedOrigins` 设为前端的来源。
- **站点域名**：在启用 Apple 或 Telegram 登录前，先在系统设置中设置 `Site.Host`。
- **日志**：生产环境建议使用 `file` 或 `volume` 模式持久化日志。将 `Level` 设为 `error` 可减少日志量；
  `severe` 则完全不写日志。
- **数据库**：确保 `Database` 与 `Redis` 凭据安全，避免在版本控制中暴露，并保持 `config.yaml` 为
  `0600` 权限。跨网络访问 PostgreSQL 请使用 `sslmode=require` 或 `verify-full`。
- **JWT**：为 `JwtAuth` 设置强 `AccessSecret` 以增强安全性。
- **易猜测的密钥**：保存密钥的设置（Turnstile secret、货币 AccessKey、节点 outbound 的密码与 UUID）
  在管理员 API 响应中会被掩码；原样提交掩码值时保留已存储的值。

如需进一步帮助，请参考 PPanel 官方文档或联系支持团队。
