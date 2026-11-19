# Upstream Sync Prompt

You are syncing an explicit upstream tag `<TAG>` from the `offical-upstream` remote into this fork. Do not infer the target from the highest local tag or reset the worktree.

**Read first:** [current-customizations.md](./current-customizations.md), [current customization ledger](../sync-diffs/v1.21.7-local-2026-11-19.md) and its [Markdown file/change index](../sync-diffs/v1.21.7-local-2026-11-19.files.md). Keep all future diff records and prompts in `.md`, explaining concrete behavior changes rather than creating a separate structured manifest. Last confirmed official target: v1.21.7 (`dfa796e0f4d11e0e4f5e9710042630969eda0871`); local landing: `34fd88c842febe406252fa0d3fd2686622e73832`. Inspect actual HEAD/worktree before acting. The recorded UTC/02170 work is uncommitted and must not be overwritten or silently adopted as a completed release.

## Context

- **Local ORM**: entgo (code-first schema in `ent/schema/*.go`)
- **Upstream ORM**: GORM (SQL migrations in `internal/app/migration/schema/database/mysql/`)
- **Local structure**: `internal/{logic,handler,model,types,svc}`
- **Upstream structure**: `internal/{module,repository,transport,app}`
- **Migration strategy**: Maintain entgo schema/generated queries AND the embedded MySQL/PostgreSQL SQL upgrade chain; startup uses golang-migrate, not an assumed ent auto-migration

For detailed architecture context, see `architecture-context.md`.
For GORM to entgo translation patterns, see `gorm-to-entgo-translation.md`.

## Task

Sync upstream tag `<TAG>` into the current branch.

## Steps

### Step 1: Review Upstream Changes

```powershell
git --no-pager status --short --untracked-files=all
git --no-pager log -5 --oneline
$LastOfficial = 'dfa796e0f4d11e0e4f5e9710042630969eda0871'
$TargetTag = '<TAG>'
$TargetRef = "refs/upstream-tags/offical-upstream/$TargetTag"
# Only when an upstream sync/fetch is authorized; do not overwrite local tags.
git fetch offical-upstream "refs/tags/${TargetTag}:${TargetRef}"
$TargetCommit = git rev-parse "$TargetRef^{commit}"
git --no-pager log --no-merges --oneline "$LastOfficial..$TargetCommit"
git --no-pager diff --name-status $LastOfficial $TargetCommit
git ls-tree -r --name-only $TargetCommit | Select-String 'migration/schema/database/(mysql|postgres)'
```

Record the resolved official SHA. Analyze this official-to-official range separately from local changes since the sync landing. Build a feature map with explicit port/preserve/not-applicable/pending decisions; compare against B01–B07 and W01 in the current ledger rather than merging upstream GORM files wholesale.

### Step 2: Identify Schema Changes

For each migration at the pinned official target:
1. Read the SQL and compare with local schemas and both SQL dialect trees. Map intent, not upstream numbering.
2. Express entity/query changes in [ent/schema](../../ent/schema/) and regenerate with `go generate .\ent` when needed.
3. Check [initialize/config.go](../../initialize/config.go), [initialize/version.go](../../initialize/version.go) and [the embedded migration loader](../../initialize/migrate/migrate.go). Generated ent code is not a database upgrade; add the required MySQL **and** PostgreSQL SQL up/down files for deployed databases.
4. Choose an unused local migration number after checking tracked **and untracked** migrations. Do not reuse the existing WIP 02170; never overwrite applied migrations. Explain any deliberately irreversible down migration.
5. First check embedded migration reachability. Test data conversion and rollback only against explicitly isolated databases with known historical timezone/data semantics.

### Step 3: Port Business Logic

For each upstream feature commit:
1. Read the GORM-based logic in `internal/module/*/`
2. Translate to entgo queries using `client.<Entity>.Query()` / `Create()` / `Update()`
3. Place in the corresponding `internal/logic/` package
4. Update `internal/handler/` if new API endpoints are added
5. Update `internal/types/` for new request/response types

### Step 4: Handle Breaking Changes

The wallet/email examples below illustrate translation intent, **not outstanding tasks**: wallet extraction and email canonicalization already exist in this fork. Do not reapply their data migrations. The “entgo creates the table” shorthand in the example requires a matching SQL migration in this repository; generation alone does not deploy DDL. Preserve multi-instance protocol IDs, two-stage OAuth invitation signup, Stripe callback-URL refresh, HTTP-200 business envelopes, traffic ranking fallback and local-time cron behavior from the current ledger.

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

Run the smallest existing test selection for the changed behavior; see [the current verification matrix](../sync-diffs/v1.21.7-local-2026-11-19.md#6-接手验证与证据边界). Escalate to `go test .\...`, build and lint only when the implementation scope requires them. Do not start the service against shared configuration merely to validate documentation: startup can apply SQL migrations. Real migration/OAuth/payment checks require explicitly isolated services. Mark skipped, unavailable and not-run checks separately from passed checks.

If routes/types change, refresh and verify the sibling frontend's local backend contract (`api:refresh`, `api:check`, `api:verify`), including manual service types where still used.

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
- Document the official range in `.ai/sync-diffs/<last-tag>-to-<new-tag>.md`; also update the dated local-customization ledger, full file manifest, both directory indexes and [current-customizations.md](./current-customizations.md). Preserve old verification evidence as history and clearly identify committed versus worktree-only changes.

## References

- See `architecture-context.md` for detailed architecture differences
- See `gorm-to-entgo-translation.md` for translation patterns
- See `.ai/sync-diffs/` for previous sync records
