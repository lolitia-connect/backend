# Port Feature Prompt

You are porting a specific feature from upstream into this fork.

## Context

- **Feature**: `<FEATURE_NAME>`
- **Upstream commits**: `<COMMIT_HASHES>`
- **Local ORM**: entgo
- **Upstream ORM**: GORM

For detailed architecture context, see `architecture-context.md`.
For GORM to entgo translation patterns, see `gorm-to-entgo-translation.md`.

## Task

Port the `<FEATURE_NAME>` feature from upstream to local.

## Steps

1. **Analyze upstream implementation**:
   ```bash
   # Read the commits
   git show <COMMIT_HASH>
   
   # Find affected files
   git diff-tree --no-commit-id --name-only -r <COMMIT_HASH>
   ```

2. **Identify schema changes**:
   - What new tables/columns are needed?
   - Can they be expressed in entgo schema?
   - Do we need a hand-written migration for data?

3. **Translate business logic**:
   - Read upstream GORM-based logic
   - Translate to entgo queries
   - Place in corresponding `internal/logic/` package

4. **Update API layer**:
   - Add new handlers in `internal/handler/`
   - Add new types in `internal/types/`
   - Update routes if needed

5. **Test**:
   ```bash
   go test ./ent/...
   go test ./internal/logic/...
   ```

## Output

Provide:
1. List of files created/modified
2. Schema changes (entgo or hand-written migration)
3. Logic changes with code snippets
4. API changes (new endpoints, types)
5. Test results

## Example Features

### High Priority (Core Features)

- **Wallet table extraction** — New `user_wallet` table, drop `user.balance/gift_amount/commission` columns (Breaking change)
- **Nowhere protocol** — Add to `server` protocol enum
- **Cryptomus payment** — New payment channel in `payment` schema
- **Facebook OAuth** — New auth method type
- **GitHub OAuth** — New auth method type
- **Domain event bus** — Architecture pattern, uses asynq
- **Argon2id password migration** — `user.password` + `user.algo` fields
- **Order creation audit logs** — New `order_event` table or extend `system_log`
- **Risk metadata in logs** — Extend `system_log` with IP/UA/risk fields
- **V2 checkout events** — New checkout flow tables
- **EPay mapi checkout** — Extend payment channel

### Medium Priority (Enhancements)

- **Telegram admin commands** — Bot command interface
- **Email subject configuration** — Extend `system` config
- **Subscription template params** — Extend `subscribe` or `application`
- **Reality key generation** — Extend `server` security fields
- **ShadowsocksR multi-user** — Extend `server` protocol enum
- **Signed subscription manifest** — New API endpoint
- **Protobuf server API** — gRPC/protobuf transport
- **Date range filters in logs** — Query enhancement
- **Commission withdrawal fix** — `user_withdrawal` sign fix
- **Email canonicalization** — `user_auth_method` identifier normalization

### Low Priority (Infrastructure/Perf)

- **PostgreSQL BRIN indexes** — Index optimization
- **PostgreSQL fillfactor** — Table storage tuning
- **MySQL hot indexes** — Index optimization
- **Task progress tracking** — New `task_progress`, `task_error` tables
- **Domain outbox pattern** — New outbox tables
- **Subscription lifecycle indexes** — Index on `user_subscribe`
- **Native Hertz binding** — HTTP layer refactor

## Directory Structure Mapping

When porting features, use this mapping to find local equivalents:

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

## Important Rules

- Never copy GORM code directly
- Always check `m != nil` before accessing fields when `IsNotFound` is allowed
- Use sequential migration numbers (check existing migrations first)
- Maintain local directory structure (don't adopt upstream's DDD modules)
- Document changes in `.ai/sync-diffs/<last-tag>-to-<new-tag>.md`
