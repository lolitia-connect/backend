# Architecture Context

This document provides context about the architectural differences between the local fork and upstream.

## Overview

| Aspect | Local (Fork) | Upstream |
|--------|-------------|------------------|
| **ORM** | `entgo.io/ent v0.14.6` | `gorm.io/gorm v1.25.12` |
| **Schema** | `ent/schema/*.go` (code-first) + `initialize/migrate/database/mysql/*.sql` (hand-written migrations) | `internal/app/migration/schema/database/mysql/*.sql` (SQL migrations) |
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

Local uses a **dual migration system**:

1. **entgo auto-migration** (primary): Schema changes in `ent/schema/*.go` are automatically applied on startup
2. **Hand-written SQL migrations** (secondary): Complex data migrations or DDL that entgo cannot handle, stored in `initialize/migrate/database/mysql/`

**Migration priority**:
- **First**: Try to express the change in `ent/schema/*.go` and let entgo handle it
- **Second**: If entgo cannot handle it (complex data migrations, index optimizations, raw DDL), write a hand-written SQL migration

**How to determine the next migration number**:
```bash
# Check the latest upstream tag's migration files
git ls-tree -r --name-only offical-upstream/master | grep "migration/schema/database/mysql" | sort | tail -5

# Check local hand-written migrations
ls initialize/migrate/database/mysql/ | sort | tail -5

# Use the next sequential number after the highest existing migration
# Example: if upstream has 02178 and local has 02006, next local migration is 02007
```

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
3. **Migration priority**: Always try entgo schema changes first. Only use hand-written SQL migrations when entgo cannot express the change.
4. **GORM tags**: Upstream uses `gorm:"..."` struct tags. entgo uses schema DSL in `ent/schema/*.go`.
5. **Repository pattern**: Upstream's `internal/repository/` is a thin wrapper over GORM. Local's `internal/repository/` wraps entgo. Do not mix.
6. **Module boundaries**: Upstream enforces DDD module boundaries. Local uses flat `internal/logic/` packages. Maintain local structure.
7. **Config keys**: Upstream stores config in `system` table as JSON. Local uses the same pattern but field names may differ.
8. **Table name differences**: Upstream and local may use different table names. Always verify table names when porting SQL migrations.
9. **One protocol type == many configs**: a server holds a list of protocol instances keyed by `type:id`, and `MarshalProtocols` (`internal/model/node/server.go`) auto-increments `id` per type. Never de-duplicate by type alone, and never assume "one inbound per protocol type" when porting a distribution/sanitisation change.
10. **Validate against the node, not just upstream**: this fork's node is `node-backend/`. Its accept-list and rules live in `node-backend/core/validate.go`, and it builds inbounds in `node-backend/core/inbound/`. Upstream changed some protocol semantics (e.g. it moved Shadowsocks obfs into a plugin system this node does not have), so a faithful copy can break working servers — see `node-backend/.ai/panel-sync-notes.md` for the panel ↔ node contract and the checks that must not be copied verbatim.
