# PPanel Configuration Guide

This document describes the configuration file of this fork
([lolitia-connect/backend](https://github.com/lolitia-connect/backend), a fork
of [perfect-panel/server](https://github.com/perfect-panel/server)). The
configuration file is YAML and defines settings for the server, logging,
tracing, the database, Redis, email, the plugin system and the first
administrator.

Where upstream documents a setting this fork does not ship (or the reverse), the
divergence is called out explicitly. The authoritative sources are
[`config.yaml.example`](../config.yaml.example) and
[`internal/config/config.go`](../internal/config/config.go).

## 1. Configuration File Overview

- **Default Path**: `./config.yaml` — relative to the working directory.
  **This is a fork divergence:** upstream v1.21.7 uses `./etc/ppanel.yaml`. The
  Docker image and `docker-compose.yml` here mount `/app/config.yaml`.
- **Custom Path**: specify with the `--config` startup parameter, for example
  `ppanel-server run --config /opt/ppanel-server/config.yaml`.
- **Format**: YAML, supports comments.
- **Permissions**: the file holds the JWT secret and the database credentials.
  Keep it readable by the service user only (`chmod 0600`). The server creates a
  missing file with mode `0600`, the setup wizard writes it with `0600`, and the
  environment-variable installation re-applies `0600`.
- **Missing/empty file**: if `Database.Addr` is empty and `PPANEL_DB` /
  `PPANEL_REDIS` are not set, the server starts the setup wizard instead of the
  API (section 5).

## 2. Configuration File Structure

The example file shipped as [`config.yaml.example`](../config.yaml.example),
with the struct-tag defaults from `internal/config/config.go` filled in. Note
that the example itself already fills in a production-shaped profile — it ships
`AccessSecret: CHANGE_ME_TO_A_RANDOM_SECRET` (a placeholder you must replace),
`Driver: postgres`, `Level: debug` and the logging sizes listed in 3.5 — so a
value from the example overrides the tag default shown here.

```yaml
Model: "prod"                       # "dev" returns verification codes in the API response (see 3.12)
Host: "0.0.0.0"                     # Server listening address
Port: 8080                          # Server listening port
Debug: false                        # Debug mode (disables release mode)

# Empty trusts loopback and private networks. Use ["none"] to ignore forwarding headers.
TrustedProxies: []
# Empty preserves permissive CORS. Set exact browser origins for an allowlist.
AllowedOrigins: []

HTTP: # Listener limits
  ReadTimeoutSeconds: 180           # Time allowed to read a request; 0 = unlimited
  WriteTimeoutSeconds: 0            # Time allowed to write a response; 0 = unlimited (streaming)
  IdleTimeoutSeconds: 180           # Idle time before a keep-alive connection is closed
  MaxRequestBodyMB: 4               # Largest accepted request body, in MB

GeoIP: # MaxMind GeoLite2 city database, used for the login/audit logs
  Path: "./cache/GeoLite2-City.mmdb"
  Download: true                    # Download when Path is missing or fails SHA256
  DownloadURL: ""                   # Empty uses the built-in database URL
  SHA256: ""                        # Optional 64-character hex SHA-256 digest
  Required: false                   # true = refuse to start without a valid database

Transport:
  Driver: "hertz"                   # Fork-specific: the listener implementation

TLS: # Serve HTTPS from the server itself (usually left to a reverse proxy)
  Enable: false
  CertFile: ""
  KeyFile: ""

JwtAuth: # JWT authentication settings
  AccessSecret: ""                  # Access token secret (required; see 3.4)
  AccessExpire: 604800              # Access token expiration (seconds)

Logger: # Logging configuration
  ServiceName: "PPanel"             # Service name for log identification
  Mode: "file"                      # Log output mode (console, file, volume)
  Encoding: "json"                  # Log format (json, plain)
  TimeFormat: "2006-01-02 15:04:05.000"  # Custom time format
  Path: "logs"                      # Log file directory
  FilePath: ""                      # Explicit log file path (optional)
  Color: true                       # Colored console output
  Level: "info"                     # Log level (debug, info, error, severe)
  MaxContentLength: 0               # Truncate long log lines (0 = no limit)
  Compress: false                   # Enable log compression
  Stat: true                        # Collect log statistics
  KeepDays: 0                       # Log retention period (days)
  MaxAge: 0                         # Log retention period (days, legacy alias)
  StackCooldownMillis: 100          # Stack trace cooldown (milliseconds)
  MaxBackups: 0                     # Maximum number of log backups
  MaxBackup: 0                      # Maximum number of log backups (legacy alias)
  MaxSize: 0                        # Maximum log file size (MB)
  Rotation: "daily"                 # Log rotation strategy (daily, size)
  FileTimeFormat: "2006-01-02T15:04:05.000Z07:00"

Trace: # OpenTelemetry tracing
  Name: "ppanel"                    # Service name recorded in the traces
  Endpoint: ""                      # Collector endpoint; empty = no export
  Sampler: 1.0                      # Fraction of traces sampled
  Batcher: "jaeger"                 # jaeger, otlpgrpc, otlphttp, zipkin or file
  OtlpHeaders: {}                   # Extra headers for the OTLP exporters
  OtlpHttpPath: "/v1/traces"        # Path for otlphttp with a host:port Endpoint
  OtlpHttpSecure: false             # HTTPS for otlphttp with a host:port Endpoint
  OtlpGrpcSecure: false             # TLS for the otlpgrpc exporter; false = plaintext
  Disabled: false                   # true = tracing off

Database:
  Driver: "postgres"                # mysql or postgres (struct-tag default: mysql; the example ships postgres)
  Addr: ""                          # Database address (required)
  Username: ""                      # Database username (required)
  Password: ""                      # Database password (required)
  Dbname: ""                        # Database name (required)
  Config: "sslmode=disable&TimeZone=UTC"  # Database stores UTC; PostgreSQL; MySQL: charset=utf8mb4&parseTime=true&loc=UTC
  MaxIdleConns: 10                  # Maximum idle connections
  MaxOpenConns: 10                  # Maximum open connections
  SlowThreshold: 1000               # Slow query threshold (milliseconds)

# MySQL: Deprecated. Read only when Database is empty; migrated on write.
#   Same fields as Database. Use Database instead.

Redis:
  Host: "localhost:6379"            # Redis address
  Pass: ""                          # Redis password
  DB: 0                             # Redis database of cache, sessions and limits
  PoolSize: 100                     # Maximum connections in the pool
  MinIdleConns: 10                  # Minimum idle connections kept
  MaxRetries: 3                     # Retries on a network error
  PoolTimeout: 4                    # Seconds to wait for a free connection
  IdleTimeout: 300                  # Seconds before an idle connection is reclaimed
  MaxConnAge: 0                     # Maximum connection lifetime (seconds; 0 = unlimited)
  DialTimeout: 5                    # Connection timeout (seconds)
  ReadTimeout: 3                    # Read timeout (seconds)
  WriteTimeout: 3                   # Write timeout (seconds)

Administrator: # First administrator, created on the first start
  Email: "admin@ppanel.dev"         # Admin login email
  Password: ""                      # Admin login password; empty = generate one

# The sections below exist in the configuration model but are not part of
# config.yaml.example; they are normally managed from the panel at runtime.
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

The `Model` key at the top of the file is described in section 3.12.

`Log.AutoClear` controls automatic traffic detail cleanup, and `Log.ClearDays`
sets the retention period. After daily statistics commit, a separate cleanup
job deletes expired details in batches of at most 1000 rows using a fixed cutoff.
Each batch commits independently, so cleanup failures do not roll back statistics.
Both jobs rely on Asynq task retries without retrying database errors internally.
A task lock and the committed daily summary prevent duplicate statistics; a global
cleanup task lock prevents concurrent cleanup across instances. Disabling
`AutoClear` also skips cleanup jobs that are already queued.

## 3. Configuration Details

### 3.1 Server Settings

- **`Host`**: Address the server listens on. Default: `0.0.0.0`.
  It is only a bind address. Public links such as payment callbacks are built
  from the site host (`Site.Host`, 3.11) or a payment method's own domain, and OAuth
  redirects are pinned to the configured site origin.
- **`Port`**: Port the server listens on. Default: `8080`.
  The container image runs as an unprivileged user (uid 65532), which cannot
  bind a port below 1024: publish a low port with a mapping (`-p 443:8080`)
  rather than changing `Port`.
- **`Debug`**: Enables debug mode. Default: `false`. When off, the server runs
  Hertz in release mode; when on it disables release mode and background logging.
- **`TrustedProxies`**: Reverse proxies whose `X-Forwarded-For` and `X-Real-IP`
  headers name the real client, as IP addresses or CIDR ranges, for example
  `["127.0.0.1", "10.0.0.0/8"]`.
  - Default: the loopback interface and the private networks
    (`127.0.0.0/8`, `::1/128`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
    `fc00::/7`), so nginx on the same host, a Docker bridge or an internal load
    balancer is trusted without configuration. A configured list replaces the
    default; the entry `private` stands for the default networks
    (`["private", "203.0.113.10"]` adds a public proxy), and `["none"]` trusts no
    header at all, for a server its clients reach directly.
  - The client address feeds the login and audit logs and every per-IP limit
    (verification codes, device login, availability checks, subscription
    fetches, registrations). List the proxies only, never the clients: an entry
    such as `0.0.0.0/0` lets anyone choose the address the server records.
- **`AllowedOrigins`**: Browser origins admitted by CORS, as
  `scheme://host[:port]`, for example
  `["https://user.example.com", "https://admin.example.com"]`.
  - Default: empty, which keeps the permissive behaviour of reflecting the
    request's `Origin`: any website's scripts may call the API from a browser.
  - When set, a listed origin is matched exactly (case-insensitively) and
    receives the CORS headers, with credentials allowed; an unlisted origin
    receives no CORS headers at all, so the browser blocks the call. The device
    WebSocket (`/v1/app/ws/...`) applies the same list to browser clients.
  - Requests without an `Origin` header (clients, nodes, subscriptions) are not
    affected.
- **`HTTP`**: Limits of the API listener.
  - **`ReadTimeoutSeconds`**: Time allowed to read a request, headers and body.
    Default: `180`; `0` = unlimited.
  - **`WriteTimeoutSeconds`**: Time allowed to write a response. Default: `0` =
    unlimited, which the streaming endpoints (order events over SSE, the node
    WebSocket) need; a bound here would cut them off.
  - **`IdleTimeoutSeconds`**: Idle time before a keep-alive connection is
    closed. Default: `180`.
  - **`MaxRequestBodyMB`**: Largest request body accepted, in MB; larger
    requests are rejected before they are read. Default: `4`.
- **`Transport.Driver`**: Listener implementation. Default: `hertz`.
  **Fork-specific:** upstream v1.21.7 has no `Transport` section.
  Note: the server currently never reads this value — `internal/config` declares
  it and nothing branches on it — so it is accepted and ignored. It is documented
  because it is part of the file schema, not because it changes behaviour.

### 3.2 GeoIP (`GeoIP`)

The MaxMind GeoLite2 city database gives the login and audit logs a location
for each address. Without it the server runs normally and records the addresses
alone.

- **`Path`**: Path of the `GeoLite2-City.mmdb` file, relative to the working
  directory. Default: `./cache/GeoLite2-City.mmdb`. The container image ships a
  writable `/app/cache`. (`NewIPLocation` also has a hard-coded fallback download
  to this default path from the built-in mirror; `OpenIPLocation`, the configured
  path, is what the server uses at startup.)
- **`Download`**: Download the database at startup when the file at `Path` does
  not exist. Default: `true`.
  - **Divergence:** upstream also fetches an ASN database (`GeoLite2-ASN.mmdb`)
    and documents it as living next to the city database. This fork's
    `internal/svc/mmdb.go` handles the **city database only**; no ASN file is
    downloaded or read.
  - Upstream re-downloads an existing file whose SHA-256 does not match. This
    fork downloads **only when the file is missing**: an existing file with a
    mismatching digest is not re-fetched, it fails the `SHA256` check below.
- **`DownloadURL`**: Where to download the city database from. Default: empty,
  the built-in mirror (a copy of GeoLite2-City published on GitHub). Point it at
  your own copy, or at MaxMind's download link with your licence key, to control
  what the server loads.
- **`SHA256`**: Hex SHA-256 digest the city database must have. Default: empty,
  no check. When set, it is verified against the file actually opened, and a
  downloaded file that does not match never replaces the existing one (the
  download goes to a temporary file, is verified, and is only then renamed into
  place). Set it when `DownloadURL` names a file you control.
- **`Required`**: Make a missing or invalid city database fatal at startup.
  Default: `false`: the server starts without geolocation and logs a warning.

### 3.3 TLS (`TLS`)

Serve HTTPS from the server itself.

- **`Enable`**: Default `false`. The usual deployment terminates TLS at a
  reverse proxy, which is then listed in `TrustedProxies`.
- **`CertFile`**, **`KeyFile`**: Paths of the PEM certificate chain and private
  key, readable by the service user. An invalid certificate fails startup.

The `healthcheck` subcommand follows these settings (section 6).

### 3.4 JWT Authentication (`JwtAuth`)

- **`AccessSecret`**: Secret key for access tokens. Sessions, order event
  tickets and guest-checkout signatures are all keyed by it, so an empty secret
  would let anyone forge them.
  - Required: the server panics at startup when it is empty
    (`JwtAuth.AccessSecret must not be empty`). Use a long random value.
  - Only a new installation gets one generated: when the configuration file has
    no secret, the setup wizard (or the `PPANEL_DB` / `PPANEL_REDIS` environment
    variables, section 5) writes a generated secret into it. A pre-filled file
    must contain one.
- **`AccessExpire`**: Token expiration time in seconds. Default: `604800`
  (7 days).

### 3.5 Logging (`Logger`)

- **`ServiceName`**: Identifier for logs; in `volume` mode it names the log
  directory. Default: `PPanel`.
- **`Mode`**: Log output destination: `console` (stdout/stderr), `file` (to a
  directory), `volume` (Docker volume). Default: `file`.
- **`Encoding`**: Log format: `json` (structured) or `plain` (plain text with
  colors). Default: `json`.
- **`TimeFormat`**: Custom time format for logs. Default:
  `2006-01-02 15:04:05.000`.
- **`Path`**: Directory for log files. Default: `logs`, relative to the working
  directory. `access.log` is written here; `logging.ReadLastNLines` reads it.
- **`FilePath`**: Explicit log file path, when a single file is wanted instead
  of the directory. Default: empty.
- **`Color`**: Colored console output. Default: `true`.
- **`Level`**: Log filtering level. Default: `info`.
  - `debug` → debug and above; `error` → error and above;
    `severe` / `fatal` / `alert` → fatal and above, which silences the log
    because the server writes no entry above `error`.
  - **Anything else — including `info`, `warn` and `panic` — maps to `info`.**
    `config.yaml.example` lists `warn` and `panic` in its comment, but
    `pkg/logging.logging.go`'s `level()` has no case for them: they behave as
    `info`, not as a warning-only or panic-only filter. Use `error` to reduce
    volume.
  - `config.yaml.example` ships `Level: debug`.
- **`MaxContentLength`**: Truncate long log lines; `0` = no limit. Default: `0`.
- **`Compress`**: Enable compression for log files (only in `file` mode).
  Default: `false`.
- **`Stat`**: Collect log statistics. Default: `true`.
- **`KeepDays`** / **`MaxAge`**: Retention period for log files, in days.
  `MaxAge` is the legacy alias. Default: `0`.
  **Divergence:** upstream's default is `KeepDays: 30`; this fork defaults to
  `0` (no automatic retention) unless the file sets it. `config.yaml.example`
  ships `MaxAge: 30`.
- **`StackCooldownMillis`**: Cooldown for stack trace logging to prevent log
  flooding. Default: `100`.
- **`MaxBackups`** / **`MaxBackup`**: Maximum number of log backups (for `size`
  rotation). Default: `0`. **Divergence:** upstream defaults to `30`;
  `config.yaml.example` ships `MaxBackup: 3`.
- **`MaxSize`**: Maximum log file size in MB (for `size` rotation). Default: `0`.
  **Divergence:** upstream defaults to `100`; `config.yaml.example` ships `50`.
- **`Rotation`**: Log rotation strategy: `daily` or `size`. Default: `daily`.
- **`FileTimeFormat`**: Timestamp format used in log file names. Default:
  `2006-01-02T15:04:05.000Z07:00`. Rotations with the same timestamp get a
  numbered suffix. Compressed and uncompressed backups count toward `MaxBackups`.

> The defaults above come from the struct tags in `pkg/logging/logging.go`.
> `config.yaml.example` sets a production-oriented profile
> (`FilePath: logs/ppanel.log`, `MaxSize: 50`, `MaxBackup: 3`, `MaxAge: 30`,
> `Compress: true`, `Level: debug`, `Color: true`); a value in the file always
> wins over the tag default.

Routine `HTTP Request` entries are kept in local logs only. When backend Sentry
Tracing is enabled, requests are visible in Sentry Traces. `HTTP Error` and other
logs continue to follow the backend Sentry settings.

### 3.6 Tracing (`Trace`)

OpenTelemetry tracing of the HTTP requests, the database calls and the queued
tasks. Nothing is exported until `Endpoint` is set.

- **`Name`**: Service name recorded on the traces (`service.name`). Default:
  `ppanel`.
- **`Endpoint`**: Where the batcher sends the spans; empty exports nothing.
  - For the `jaeger` batcher it is Jaeger's OTLP/HTTP receiver, either a URL such
    as `http://jaeger:4318` (posting to `/v1/traces` unless the URL names another
    path; the scheme selects TLS) or a bare `host:port`.
  - For `otlpgrpc` it is the collector's gRPC address (`host:4317`), for
    `otlphttp` a URL or `host:port` (see `OtlpHttpPath` and `OtlpHttpSecure`),
    for `zipkin` the Zipkin API URL and for `file` a file path.
- **`Sampler`**: Fraction of the traces kept, from `0` to `1`. Default: `1.0`
  — **divergence:** upstream defaults to `0.1`; `config.yaml.example` ships
  `1.0`.
- **`Batcher`**: The exporter: `jaeger`, `zipkin`, `otlpgrpc`, `otlphttp` or
  `file`. Default: `jaeger`.
- **`OtlpGrpcSecure`**: Transport security of the `otlpgrpc` exporter.
  Default: `false`: the gRPC connection to the collector is made without TLS.
  Set it to `true` for a collector that serves TLS; the system's root
  certificates verify it, and the client does not silently downgrade to
  plaintext. `otlphttp` selects TLS through the URL scheme or `OtlpHttpSecure`
  instead.
- **`OtlpHeaders`**: Headers sent with every OTLP export, for example an
  `uptrace-dsn` or an `Authorization` header for a hosted collector.
- **`OtlpHttpPath`**: Path of the OTLP/HTTP receiver when `Endpoint` is a bare
  `host:port`. `config.yaml.example` ships `/v1/traces`.
- **`OtlpHttpSecure`**: Use HTTPS for `otlphttp` when `Endpoint` is a bare
  `host:port`. Default: `false`.
- **`Disabled`**: Turns tracing off entirely. Default: `false`.

### 3.7 Database (`Database`)

- **`Driver`**: Database dialect: `mysql` (MySQL 8 and MariaDB) or `postgres`.
  Default from the struct tag: `mysql`. **`config.yaml.example` ships
  `postgres`** (with a PostgreSQL `Config`), so a file copied from the example
  without editing `Driver` connects with the PostgreSQL driver. Set both
  `Driver` and `Config` to match your database.
- **`Addr`**, **`Username`**, **`Password`**, **`Dbname`**: Connection details.
  Required.
- **`Config`**: Dialect-specific connection parameters.
  - MySQL default:
    `charset=utf8mb4&parseTime=true&loc=UTC`.
  - PostgreSQL example:
    `sslmode=disable&TimeZone=UTC`.
  - **Timezone basis:** the database stores UTC; the connection is **pinned**
    to `loc=UTC` / `TimeZone=UTC` (any timezone in `Config` is overridden), and
    the application renders values in the process-local timezone (`time.Local`)
    after reading. To upgrade a legacy database, apply migration
    `02170_localtime_to_utc` (below).
  - **Divergence:** upstream default for PostgreSQL is
    `sslmode=prefer&TimeZone=<AppLocation>&application_name=perfect-panel`, and
    it injects `TimeZone` automatically. This fork has no `AppLocation` setting
    and does not rewrite `Config`; set the zone yourself. For a database reached
    over a network use `sslmode=require` or `verify-full`; `sslmode=disable` is
    only for a database on the same host or in a private network.
- **`MaxIdleConns`**: Maximum idle connections. Default: `10`.
- **`MaxOpenConns`**: Maximum open connections. Default: `10`.
  **Divergence:** upstream also has `ConnMaxLifetime` (`1800`) and
  `ConnMaxIdleTime` (`300`); this fork's `pkg/orm.Config` does not define them,
  so they are not read.
- **`SlowThreshold`**: Threshold for slow query logging (milliseconds).
  Default: `1000`.
- **`MySQL`**: **Deprecated**, kept for compatibility. Read only when
  `Database` names no connection, and removed from the file when the server
  writes the configuration (`SetDatabaseConfig` clears it). Use `Database`.

### 3.8 Redis (`Redis`)

- **`Host`**: Redis server address. Default: `localhost:6379`.
- **`Pass`**: Redis password. Default: `""` (no password).
- **`DB`**: Redis database index of the cache, sessions and rate limits.
  Default: `0`.
- **`PoolSize`**: Maximum connections in the pool. Default: `100`.
- **`MinIdleConns`**: Minimum idle connections kept. Default: `10`.
- **`MaxRetries`**: Retries on a network error. Default: `3`.
- **`PoolTimeout`**: Seconds to wait for a free connection. Default: `4`.
- **`IdleTimeout`**: Seconds before an idle connection is reclaimed. Default:
  `300`.
- **`MaxConnAge`**: Maximum connection lifetime in seconds; `0` = unlimited.
  Default: `0`.
- **`DialTimeout`**: Connection timeout in seconds. Default: `5`.
- **`ReadTimeout`**: Read timeout in seconds. Default: `3`.
- **`WriteTimeout`**: Write timeout in seconds. Default: `3`.

> **Divergence:** upstream has `Redis.QueueDB` (default `5`) to select the
> database of the task queue. This fork's `RedisConfig` has no `QueueDB` field;
> the queue database is **hard-coded to `5`** in `queue/queue.go`
> (`asynq.RedisClientOpt{..., DB: 5}`). Deployments that share one Redis server
> must therefore keep `DB` distinct from `5`, or one panel would consume the
> other's tasks. Making it configurable is a code change, not a configuration one.

### 3.9 Email Delivery (`Email` and SMTP)

The `Email` section carries the template and subject strings used by the
notification emails:

- **`Enable`**: Enable email delivery. Default: `true`.
- **`Platform`**, **`PlatformConfig`**: Provider selector and its JSON settings.
- **`EnableVerify`**, **`EnableNotify`**, **`EnableDomainSuffix`**,
  **`DomainSuffixList`**: Verification, notification and domain-suffix policy.
- **`VerifyEmailTemplate`**, **`ExpirationEmailTemplate`**,
  **`MaintenanceEmailTemplate`**, **`TrafficExceedEmailTemplate`**: Templates.
- **`VerifyEmailSubject`**, **`ExpirationEmailSubject`**,
  **`MaintenanceEmailSubject`**, **`TrafficExceedEmailSubject`**: Subjects.

The SMTP relay settings are stored in the database (system settings, email) and
edited in the panel. `pkg/email/smtp` reads:

| Field | Meaning |
|---|---|
| `host`, `port` | Relay address. Port 465 is implicit TLS (SMTPS); 25, 587 and 2525 start in plaintext and upgrade with STARTTLS. |
| `user`, `pass` | Relay credentials; empty to send without authentication. |
| `from`, `reply_to` | Sender address and, optionally, the reply address. |
| `siteName` | Display name used on the messages. |
| `ssl` | Encryption required. On port 465 (or with `implicit_tls`) the connection starts with TLS; on any other port the session must upgrade with STARTTLS, and a relay that does not offer it is refused before the credentials are sent. |
| `implicit_tls` | Start the connection with a TLS handshake on a port other than 465. Only for relays that serve SMTPS on a non-standard port; never for a STARTTLS port. |
| `insecure_skip_verify` | Accept any relay certificate. Only for relays with a self-signed certificate; certificates are verified by default. |

### 3.10 First Administrator (`Administrator`)

Seeds the first administrator: at every start, when the database has no
administrator, one is created from these values.

- **`Email`**: Admin login email. Default: `admin@ppanel.dev`.
- **`Password`**: Admin login password. Default: empty. When it is empty, a
  random password is generated and printed once in the startup log
  (`docker logs`, `journalctl -u ppanel`). Sign in with it and change it. A
  configured value is used as given.
  The setup wizard writes the installer's email here and never the password
  (`cfg.Administrator.Password = ""`).

### 3.11 Other sections

These exist in `internal/config/config.go`; `config.yaml.example` does not list
them because the panel manages them at runtime. Their defaults are in section 2.

- **`Site`**: `Host` (the public URL of the panel; payment callbacks are built
  from it and Apple/Telegram OAuth redirects are pinned to it), `SiteName`,
  `SiteDesc`, `SiteLogo`, `Keywords`, `CustomHTML`, `CustomData`.
- **`Node`**: `NodeSecret`, pull/push intervals, `TrafficReportThreshold`,
  `IPStrategy`, `DNS`, `Block`, `Outbound`.
- **`Mobile`**, **`Device`**, **`Verify`**, **`VerifyCode`**, **`Register`**,
  **`Log`**, **`Currency`**, **`Invite`**, **`Telegram`**: client app, device
  admission, captcha, verification-code, registration, log-retention, currency
  and referral settings. Initializing an SMS provider keeps its configuration and signing credentials out of the application log.
- **`Subscribe`**: subscription delivery settings, including
  `ProfileUpdateInterval` and `ProfileWebPageURL`, which are surfaced to clients
  that refresh a profile on their own schedule (`0` / empty omits the field).
- **`Plugin`**: **fork-specific plugin system.** `Enabled` (default `true`),
  `Directory` (default `plugins`), `MaxMemoryMB` (default `64`),
  `TimeoutSec` (default `30`), `AllowList` (empty = allow every plugin),
  `BlockList`. WASM plugin binaries are git-ignored
  (`plugins/**/*.wasm`) because they are built elsewhere.
- **`EdgeSubscribe`**: **fork-specific signed Edge Manifest API**
  (`GET /api/edge/v1/manifest`), consumed by a trusted Cloudflare Worker. It is
  disabled by default because enabling it exposes an HMAC-authenticated endpoint
  that mints user credentials. Each node uses its configured protocol instance. If that
  instance is removed, the manifest omits the node and includes a notice. Legacy nodes
  without an instance ID use the first matching protocol type.
  - **`Enabled`**: Default `false`.
  - **`MaxClockSkewSeconds`**: Allowed clock skew for the request timestamp.
  - **`Keys`**: A list of `{ID, Secret}` HMAC credentials. Rotation is additive:
    add a new key, deploy the workers using it, then remove the old one.
  - See [`docs/edge-subscribe.md`](../docs/edge-subscribe.md) for the protocol.

### 3.12 Runtime mode (`Model`)

- **`Model`**: Default `"prod"`. It is read at runtime, not at build time.
  - `Config.Model` is compared against `"dev"` (`constant.DevMode`) by the
    verification-code paths (`sendSmsCodeLogic`, `sendEmailCodeLogic`, and the
    queued SMS task). In `dev` mode the generated code is returned **in the API
    response** instead of only being sent.
  - **Leave it at `prod` outside development.** `Model: dev` on a deployment
    that anyone can reach hands the verification code to the caller.
  - `config.yaml.example` deliberately does not list this key.

## 4. Fork-specific behaviour not expressed in the configuration file

- **entgo, not GORM.** The persistence layer is `entgo.io/ent`; the generated
  client lives in `ent/` and `pkg/orm` opens the connection. Upstream v1.21.7
  uses GORM. Nothing in the configuration file changes because of this, but the
  documentation of migrations in
  [`initialize/migrate`](../initialize/migrate) applies.
- **Multi-protocol node instances keyed `type:id`.** A server may host several
  protocol instances; node references are `type:id` rather than a bare id. This
  is a data-model difference, not a configuration one.
- **Unlimited traffic (`traffic_unlimited`).** A subscription can be marked
  unlimited; the traffic calculations then ignore the quota. The flag is stored
  per subscription and set from the panel, not from this file.
- **Stripe Checkout Sessions.** The Stripe payment method uses hosted Checkout
  Sessions (`CreateCheckoutSession`), including async success handling. The
  credentials are configured per payment method in the panel.
- **EPay V1/V2.** The EPay method supports both protocol versions; V2 API
  endpoints may be site-relative. Configured in the panel, per payment method.
- **Payment callbacks.** Checkout and the payment method list use the channel's
  domain, falling back to the first public host in `Site.Host`. Gateway mode
  adds `/api` before `/v1/notify/`. A blank callback on an external payment
  method means its public address needs configuring.
- **Payment billing descriptions.** A payment method's `BillDesc` supports
  `{order_no}`, `{item_name}`, `{amount}`, and `{trade_no}`. For recharge orders,
  `{item_name}` is `充值`; subscription-related orders use the plan name.
  `{amount}` is the order amount formatted to two decimal places.

## 5. First start and environment variables

With an empty configuration file (no `Database.Addr`) the server does not start
the API. It starts the **setup wizard on `127.0.0.1` only**, at the configured
`Port` (8080 by default) and never on a public interface, so
`http://<server>:8080/init` is not reachable from a browser on another machine.
A remote host needs an SSH tunnel:

```shell
ssh -L 8080:127.0.0.1:8080 user@your-server
# then open http://127.0.0.1:8080/init
```

The wizard tests the connections, applies the migrations, creates the
administrator (password of at least 8 characters), writes the configuration file
with `0600` and hands over to the API server. Should a step fail after the file
was written, restart the server: it resumes the installation from the file.

For containers and unattended installations, two environment variables complete
an empty configuration file on the first start:

| Environment Variable | Configuration Section | Format | Example |
|---|---|---|---|
| `PPANEL_DB` | `Database` | MySQL DSN `user:password@tcp(host:port)/dbname[?params]`, or a URL: `mysql://user:password@host:3306/dbname`, `postgres://user:password@host:5432/dbname[?sslmode=require]` | `ppanel:secret@tcp(127.0.0.1:3306)/ppanel` |
| `PPANEL_REDIS` | `Redis` | `redis://[:password@]host[:port][/db]` (port `6379` and database `0` when omitted) | `redis://:secret@127.0.0.1:6379/0` |

- Both are needed, and they are read only while the configuration file (the
  default `config.yaml` or the one given with `--config`) has no
  `JwtAuth.AccessSecret`; once the file has a secret they are ignored.
- The server writes the file back with a generated secret, the database
  connection and the Redis connection, **keeping every other setting the file
  already had** (`Host`, `Port`, `TLS`, `Logger`, `Trace`, `EdgeSubscribe`, …)
  by editing the YAML document in place. A deprecated `MySQL` key is removed.
- The file must be writable by the user the server runs as; the container image
  runs as uid 65532.

## 6. Health Checks

| Endpoint | Meaning | Response |
|---|---|---|
| `GET /healthz` | Liveness: the process and its listener are up. | `200` |
| `GET /readyz` | Readiness: start-up has completed and the database and Redis answer (the probe result is cached for 5 seconds). | `200`, or `503` with `{"status":"unavailable","reason":"…"}`, the reason being `database unreachable`, `redis unreachable`, or `database unavailable` / `redis unavailable` when the resource was never constructed |

Both are unauthenticated and disclose nothing beyond the reason, so they can be
exposed to monitoring and load balancers; point load-balancer health checks at
`/readyz`. Neither is served while the setup wizard is running.

`ppanel-server healthcheck [--config config.yaml] [--timeout 3s]` reads the
listener settings from the configuration file and requests `/healthz` on it:
`127.0.0.1:<Port>`, or `<Host>:<Port>` when `Host` is not `0.0.0.0`/`::`, over
HTTPS when `TLS.Enable` is on. Redirects are not followed. It exits with a
non-zero status when it gets no healthy answer within the timeout. The container
image's `HEALTHCHECK` runs it, and a systemd or cron watchdog can too.

## 7. Best Practices

- **Security**: Change the first administrator's password after the first
  sign-in. If the file still carries an `Administrator.Password` once an
  administrator exists, it holds a credential nothing needs; remove it.
- **Reverse proxy**: A proxy on the same host or in a private network is trusted
  by default; a proxy with a public address goes into `TrustedProxies`, and a
  server its clients reach directly sets `TrustedProxies: ["none"]`. Set
  `AllowedOrigins` to the frontends' origins.
- **Site host**: Set `Site.Host` in the system settings before enabling Apple or
  Telegram sign-in. Telegram login and account linking require a bot token in
  the form `<bot_id>:<secret>`. The panel return URL can include query parameters.
- **Logging**: Use `file` or `volume` mode for production to persist logs. Set
  `Level` to `error` to reduce log volume; `severe` writes nothing at all.
- **Database**: Ensure `Database` and `Redis` credentials are secure and not
  exposed in version control, and keep `config.yaml` at mode `0600`. Use
  `sslmode=require` or `verify-full` for a PostgreSQL server reached over a
  network.
- **JWT**: Specify a strong `AccessSecret` for `JwtAuth` to enhance security.
- **Guessable secrets**: The settings that hold secrets (Turnstile secret,
  currency access key, node outbound passwords and UUIDs) are masked in the
  admin API responses and preserved when the mask is submitted unchanged.

For further assistance, refer to the official PPanel documentation or contact
support.
