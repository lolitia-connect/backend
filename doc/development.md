# Backend development guide

This guide covers directory responsibilities, API/data-layer development, builds and compatibility boundaries. See [installation](install.md), [configuration](config.md) and [contribution guidelines](../CONTRIBUTING.md) for their respective workflows. Dated sync work, task history and verification results belong in the [sync ledgers](../.ai/sync-diffs/README.md), not the README.

[中文](development-zh.md)

## Directory structure

```
.
├── cmd/              # Application entry point (run, healthcheck, migrate, version)
├── doc/              # Documentation (config.md, install.md)
├── docs/             # Topic documents (e.g. edge-subscribe.md)
├── config.yaml.example # Example configuration file
├── ent/              # entgo generated ORM client and schemas
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

## Development workflow

### Adding a new endpoint

This fork uses hand-written Hertz handlers (`internal/handler/`), not a go-zero `.api` generation step.
To add an endpoint:

1. Add the request/response types under `internal/types/`.
2. Implement the business logic under `internal/logic/`.
3. Add the handler under `internal/handler/` and register the route in `internal/handler/routes.go`.
4. If the frontend needs it, run `bun run api:refresh` from `frontend/` to re-export this
   repository's routes and types (the contract is derived straight from the Go AST by
   `frontend/scripts/backend-contract.go`; see the
   [frontend development guide](../../frontend/docs/guide/frontend-development.md)).

The data layer is entgo: after editing `ent/schema/` run `go generate ./ent`, or
`go run entgo.io/ent/cmd/ent generate ./ent/schema` directly. Schema changes still land as paired
hand-written SQL migrations under `initialize/migrate/database/`.

### Request parsing and update semantics

- Handlers must check errors from `ShouldBind`, `ShouldBindJSON`, and `ShouldBindQuery`, call
  `result.ParamErrorResult`, and return before business logic. Never process the partial zero values
  of an invalid request. Parameter errors retain the existing HTTP 200 envelope with business code `400`.
- GET/HEAD bind query parameters even with `Content-Type: application/json`; write requests still
  follow their content type. Explicit `false` and `0` values must survive binding.
- Create accounts, login identities, and opening wallets in one transaction. The ent user `Insert`
  does not persist `AuthMethods` automatically: call `InsertUserAuthMethods` explicitly, using E.164
  phone identifiers. `/v1/auth/register/telephone` uses the phone registration flow, with the actual
  client IP and User-Agent supplied by its handler.
- Authentication configuration updates resolve the existing record by `method`. Toggling `enabled`
  must not reset saved configuration or replace the row ID. Reject non-object configurations and
  invalid email/mobile field types before saving.
- An explicit empty user-rules array clears the rules; omitting it keeps the existing rules.
  Admin user subscriptions respect pagination. Subscription access logs filter by user, subscription,
  and log type and decode IP, User-Agent, and subscription metadata from their content.
- Traffic logs accept inclusive `start_date` / `end_date` ranges, including open-ended ranges, taking
  precedence over a single `date`. Live and archived rows share the filter and combined pagination
  reports the full total, with an empty list beyond the last page. Traffic details use local calendar
  boundaries `[start midnight, midnight after end)`; without dates they still default to today.
  The per-user subscription traffic endpoint accepts Unix-millisecond `start_time` / `end_time`
  bounds, including the start and excluding the end.

### V2 order payment methods

`POST /v2/public/orders` accepts `payment_id` as a JSON integer. Positive values identify payment channels; `-1` selects balance payment for signed-in purchases, renewals, and traffic resets. Guest purchases and wallet recharges require a positive channel ID. Zero and other negative values are invalid. Balance checkout uses the existing wallet debit and order status transaction, with `Idempotency-Key` preventing duplicate debits on retries.

### Group recalculation and database transactions

New purchases and redemptions that change a subscription request recalculation through the order fulfillment event. The event publisher enqueues an Asynq job before marking the event published. Order-triggered jobs use the configured grouping mode. Scheduled jobs run in traffic mode only.

Recalculation and group reset share a renewable Redis lock. Recalculation reads eligible subscriptions under database row locks in ID order. Assignments, history details and the completed state commit together. The running history is created separately so a failed transaction can record its original error. A conflicting database write returns to the caller on the first failure. Queue workers use Asynq retries.

For the real database lock test, point `PPANEL_GROUP_TEST_MYSQL_DSN` or `PPANEL_GROUP_TEST_POSTGRES_DSN` at an empty disposable test database and run `go test -count=1 ./internal/logic/admin/group -run TestGroupingLocksSubscriptionAgainstConcurrentWriter`.

### Subscription node names

Subscription downloads and admin template previews share `adapter.Client.Build`. Valid nodes
passed to a render are first sorted by ascending node-settings `Sort`, then nodes with exactly
matching names receive ` #1`, ` #2`, ` #3`, and so on, including the first duplicate. Equal sort
values retain input order, and node definitions and group references use the same ordered list.
Each configuration calculates its own numbering; unique names remain unchanged. Sorting and
numbering operate on a copy, preserving database nodes and the original proxy data.

Numbering skips names already present in the configuration. For example, two `Hong Kong` nodes
alongside a `Hong Kong #1` node become `Hong Kong #2` and `Hong Kong #3` in sort order, while the
existing `Hong Kong #1` stays unchanged. Templates use `Name` from `.Proxies` for both node
definitions and group references so they remain consistent.

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
([`.github/workflows/release.yml`](../.github/workflows/release.yml)) publishes
`ppanel-server-<os>-<arch>.tar.gz` (`.zip` for Windows) for those platforms, each with a `.sha256` file.

## Architecture compatibility boundaries

| Aspect | This fork | Upstream v1.21.7 |
|---|---|---|
| ORM | entgo (`ent/`, `pkg/orm`) | GORM |
| Node protocol instances | Multi-protocol instances keyed `type:id` | Single protocol reference |
| Plugins | Built-in WASM plugin system (`internal/plugin`, `Plugin` section) | None |
| Unlimited traffic | `traffic_unlimited` supported | — |
| Stripe | Checkout Sessions (`CreateCheckoutSession`) | — |
| Subscription rendering | Signed Edge subscribe API, see [docs/edge-subscribe.md](../docs/edge-subscribe.md) | None |
| Configuration path | `config.yaml` | `etc/ppanel.yaml` |
| Configuration sections | Has `Transport`, `Device`, `Plugin`, `EdgeSubscribe`; `MySQL` is a deprecated alias | Has `AppLocation`, `Redis.QueueDB`, `Database.ConnMaxLifetime/ConnMaxIdleTime` |
| Queue Redis database | Hard-coded to `5` (`queue/queue.go`) | Configurable as `Redis.QueueDB` |

Full details: [configuration guide](config.md).
