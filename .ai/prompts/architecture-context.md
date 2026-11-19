# Architecture Context

This document provides context about the architectural differences between the local fork and upstream.

Read [current-customizations.md](./current-customizations.md) and [the ledger supplement through 57973bf2](../sync-diffs/v1.21.7-local-2026-11-19.md#8-截止后增量至-57973bf2) first. The official comparison here remains v1.21.7, not a floating upstream branch. UTC/02170 and local-date presentation were committed in `a6b11dfe`; the old W01 was a historical worktree snapshot, not today's state. The supplement also protects request binding, account identities, auth-config updates and subscription/traffic query semantics; committed code is not database application or deployment evidence.

## Overview

| Aspect | Local (Fork) | Upstream |
|--------|-------------|------------------|
| **ORM** | `entgo.io/ent v0.14.6` | `gorm.io/gorm v1.25.12` |
| **Schema** | `ent/schema/*.go` + embedded MySQL/PostgreSQL SQL migrations in `initialize/migrate/database/` | `internal/app/migration/schema/database/mysql/*.sql` (SQL migrations) |
| **HTTP framework** | Hertz (via `pkg/hertzx`) | Native Hertz (removed go-zero) |
| **Directory layout** | `internal/{logic,handler,model,types,svc}` | `internal/{module,repository,transport,app}` |
| **Module domains** | Flat logic packages | DDD modules: `billing`, `identity`, `network`, `notification`, `platform`, `subscription`, `support` |
| **Cache** | `ariga.io/entcache` | Manual repository caching |
| **Queue** | `hibiken/asynq` | `hibiken/asynq` (same) |

## Key Differences

### 1. ORM Layer (Critical)

**Upstream uses GORM**, local uses **entgo**. All upstream model/repository changes must be manually translated to entgo schema and queries.

- **Upstream model location**: `internal/module/*/entity/` (GORM structs with `gorm:` tags)
- **Local model location**: `ent/schema/*.go` (entgo field definitions)
- **Upstream queries**: GORM method chains (`db.Where().Find()`)
- **Local queries**: entgo fluent API (`client.User.Query().Where(...).All()`)

**Rule**: Never copy upstream GORM code directly. Translate the intent to entgo equivalents.

### 2. Migration Strategy (Dual System)

Local schema maintenance has two coordinated parts:

1. **entgo schema/generated code** defines entities and query APIs.
2. **Embedded SQL migrations** update deployed MySQL/PostgreSQL databases. [initialize/config.go](../../initialize/config.go) and [initialize/version.go](../../initialize/version.go) call golang-migrate; [migrate.go](../../initialize/migrate/migrate.go) embeds both dialect trees. Do not assume ent `Schema.Create` runs on startup.

Express supported schema changes in ent first, then provide the corresponding SQL upgrade path where deployed tables must change. Complex data moves, indexes and timestamp conversion still require explicit SQL and a rollback decision. Updating only ent schema or running `go generate .\ent` is not sufficient deployment work.

Check both dialect directories and untracked work before choosing a migration number. At `57973bf2`, 02170 is already committed (via `a6b11dfe`); do not reuse it, overwrite applied migrations or copy upstream numbering blindly. Its MySQL ±8h rewrites and PostgreSQL column conversions rely on historical data assumptions and need separate isolated-database validation. Pin the official target before comparing migration lists.

**Migration file format**:
```
initialize/migrate/database/mysql/0XXXX_description.up.sql
initialize/migrate/database/mysql/0XXXX_description.down.sql
```

**When to use hand-written migrations**:
- Data migrations (moving data between tables)
- Complex index creation (BRIN, partial indexes, fillfactor)
- Performance optimizations that entgo cannot express
- Upstream SQL migrations that cannot be translated to entgo schema changes

### 3. Directory Structure Mapping

| Upstream path | Local equivalent |
|---------------|------------------|
| `internal/module/billing/entity/order/` | `ent/schema/order.go` + `internal/logic/` |
| `internal/module/billing/internal/checkout/` | `internal/logic/public/portal/` |
| `internal/module/identity/` | `internal/logic/auth/` + `internal/logic/admin/user/` |
| `internal/module/network/` | `internal/logic/admin/server/` + `internal/logic/node/` |
| `internal/module/notification/` | `internal/logic/common/` (email/SMS) |
| `internal/module/subscription/` | `internal/logic/public/user/` (subscribe) |
| `internal/module/support/` | `internal/logic/public/ticket/` |
| `internal/repository/` | `internal/repository/` + `internal/model/` |
| `internal/transport/http/` | `internal/handler/` + `internal/middleware/` |

## Known Pitfalls

1. **Nil pointer on NotFound**: entgo returns `(nil, NotFound)` not `(zero_value, nil)`. Always guard with `m != nil`.
2. **Migration numbering**: Do NOT hardcode version numbers. Always check the latest upstream tag and local migrations to determine the next sequential number.
3. **Migration completeness**: Keep ent schema/generated code and both dialect SQL upgrade chains in sync. Never assume startup ent auto-migration replaces SQL. UTC/02170 has a recorded commit, but SQL application, legacy-data correctness and safe rollback remain unverified by this documentation task; see B08 for scope.
4. **GORM tags**: Upstream uses `gorm:"..."` struct tags. entgo uses schema DSL in `ent/schema/*.go`.
5. **Repository pattern**: Upstream's `internal/repository/` is a thin wrapper over GORM. Local's `internal/repository/` wraps entgo. Do not mix.
6. **Module boundaries**: Upstream enforces DDD module boundaries. Local uses flat `internal/logic/` packages. Maintain local structure.
7. **Config keys**: Upstream stores config in `system` table as JSON. Local uses the same pattern but field names may differ.
8. **Table name differences**: Upstream and local may use different table names. Always verify table names when porting SQL migrations.
9. **One protocol type == many configs**: a server holds a list of protocol instances keyed by `type:id`, and `MarshalProtocols` (`internal/model/node/server.go`) auto-increments `id` per type. Never de-duplicate by type alone, and never assume "one inbound per protocol type" when porting a distribution/sanitisation change.
10. **Validate against the node, not just upstream**: this fork's node is `node-backend/`. Its accept-list and rules live in `node-backend/core/validate.go`, and it builds inbounds in `node-backend/core/inbound/`. Upstream changed some protocol semantics (e.g. it moved Shadowsocks obfs into a plugin system this node does not have), so a faithful copy can break working servers — see `node-backend/.ai/panel-sync-notes.md` for the panel ↔ node contract and the checks that must not be copied verbatim.
