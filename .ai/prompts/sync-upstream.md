# Upstream Sync Prompt

You are syncing upstream changes from `offical-upstream` remote into this fork.

## Context

- **Local ORM**: entgo (code-first schema in `ent/schema/*.go`)
- **Upstream ORM**: GORM (SQL migrations in `internal/app/migration/schema/database/mysql/`)
- **Local structure**: `internal/{logic,handler,model,types,svc}`
- **Upstream structure**: `internal/{module,repository,transport,app}`
- **Migration strategy**: Prefer entgo auto-migration, use hand-written SQL only when necessary

For detailed architecture context, see `architecture-context.md`.
For GORM to entgo translation patterns, see `gorm-to-entgo-translation.md`.

## Task

Sync upstream tag `<TAG>` into the current branch.

## Steps

### Step 1: Review Upstream Changes

```bash
# Fetch latest upstream
git fetch offical-upstream --tags --force

# Find the latest upstream tag
LATEST_TAG=$(git tag -l "v*" --sort=-v:refname | head -1)
echo "Latest upstream tag: $LATEST_TAG"

# List commits since last sync (v1.3.8)
git log --oneline v1.3.8..$LATEST_TAG --no-merges

# Check upstream migration files in latest tag
git ls-tree -r --name-only $LATEST_TAG | grep "migration/schema/database/mysql" | sort | tail -10

# Diff specific files (translate GORM → entgo mentally)
git diff v1.3.8..$LATEST_TAG -- internal/module/billing/entity/
```

### Step 2: Identify Schema Changes

For each upstream migration in the latest tag:
1. Read the SQL to understand the intent: `git show $LATEST_TAG:path/to/migration.up.sql`
2. **First**: Try to express the change in `ent/schema/*.go`
   - Add new fields, edges, or indexes
   - Run `go generate ./ent` to regenerate code
   - entgo will auto-apply on next startup
3. **Second**: If entgo cannot handle it (complex data migration, raw DDL, performance tuning):
   - Check the highest local migration number: `ls initialize/migrate/database/mysql/ | sort | tail -1`
   - Create the next sequential migration: `initialize/migrate/database/mysql/0XXXX_description.{up,down}.sql`
   - Adapt the upstream SQL to local schema (table names may differ)
4. Test with `go test ./ent/...`

### Step 3: Port Business Logic

For each upstream feature commit:
1. Read the GORM-based logic in `internal/module/*/`
2. Translate to entgo queries using `client.<Entity>.Query()` / `Create()` / `Update()`
3. Place in the corresponding `internal/logic/` package
4. Update `internal/handler/` if new API endpoints are added
5. Update `internal/types/` for new request/response types

### Step 4: Handle Breaking Changes

**Wallet extraction** (most complex):
```go
// Upstream migration drops user.balance/gift_amount/commission
// Local entgo equivalent:

// 1. Create new schema: ent/schema/user_wallet.go
// 2. Add edge from User to UserWallet (O2O)
// 3. Run go generate ./ent (entgo creates the table)
// 4. Write hand-written data migration:
//    initialize/migrate/database/mysql/0XXXX_migrate_user_balance_to_wallet.up.sql
//    - INSERT INTO user_wallet (user_id, balance, gift_amount, commission)
//      SELECT id, balance, gift_amount, commission FROM user
// 5. Update ent/schema/user.go to drop old fields
// 6. Run go generate ./ent again
// 7. Update all logic that reads/writes balance to use UserWallet
```

**Email canonicalization**:
```go
// Before storing email in auth_method:
email = strings.ToLower(strings.TrimSpace(email))
// Apply in all registration/login/reset flows
```

### Step 5: Test

```bash
# Run entgo tests
go test ./ent/...

# Run logic tests
go test ./internal/logic/...

# Integration test with dev database
docker-compose -f docker-compose.dev.yml up -d
go run . run -c config.dev.yaml
```

## File Checklist

When syncing a new upstream version, check these files:

**Schema changes** (entgo first, then hand-written migrations if needed):
- [ ] `ent/schema/user_wallet.go` — new wallet table (if upstream has wallet extraction)
- [ ] `ent/schema/user.go` — remove balance/gift/commission fields (after data migration)
- [ ] `ent/schema/server.go` — add new protocols (Nowhere, Reality keys, SSR multi-user)
- [ ] `ent/schema/payment.go` — add new payment channels (Cryptomus, etc.)
- [ ] `ent/schema/auth_method.go` — add new OAuth types (Facebook, GitHub, etc.)
- [ ] `ent/schema/system_log.go` — add risk metadata fields
- [ ] `ent/schema/order.go` — add audit log fields or new order_event table
- [ ] `initialize/migrate/database/mysql/0XXXX_*.sql` — hand-written migrations for complex changes

**Logic changes**:
- [ ] `internal/logic/common/sendEmailCodeLogic.go` — canonicalize email
- [ ] `internal/logic/auth/*Logic.go` — canonicalize email in all flows
- [ ] `internal/logic/public/portal/purchaseLogic.go` — V2 checkout flow
- [ ] `internal/logic/admin/server/*Logic.go` — Reality key generation
- [ ] `internal/logic/node/*Logic.go` — new protocol handling
- [ ] `internal/handler/` — new endpoints for new OAuth providers
- [ ] `internal/config/config.go` — new config fields (email subjects, etc.)
- [ ] `config.yaml.example` — document new config options

## Important Rules

- Never copy GORM code directly
- Always check `m != nil` before accessing fields when `IsNotFound` is allowed
- Use sequential migration numbers (check existing migrations first)
- Maintain local directory structure (don't adopt upstream's DDD modules)
- Document changes in `.ai/sync-diffs/<TAG>.md`

## References

- See `architecture-context.md` for detailed architecture differences
- See `gorm-to-entgo-translation.md` for translation patterns
- See `.ai/sync-diffs/` for previous sync records
