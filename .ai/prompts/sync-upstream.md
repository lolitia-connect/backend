# Upstream Sync Prompt

You are syncing an explicit upstream tag `<TAG>` from the `offical-upstream` remote into this fork. Do not infer the target from the highest local tag or reset the worktree.

**Read first:** [current-customizations.md](./current-customizations.md), [the latest B08–B16 supplement](../sync-diffs/v1.21.7-local-2026-11-19.md#8-截止后增量至-57973bf2), the [original Markdown index](../sync-diffs/v1.21.7-local-2026-11-19.files.md) and [259-path increment](../sync-diffs/v1.21.7-local-2026-11-19-2220.files.md). Keep future diff records and prompts in `.md`, explaining concrete behavior changes rather than creating a structured manifest. Last confirmed official target remains v1.21.7 (`dfa796e0f4d11e0e4f5e9710042630969eda0871`); local landing: `34fd88c842febe406252fa0d3fd2686622e73832`. Latest recorded local HEAD is `57973bf2`; inspect actual HEAD/worktree before acting. UTC/02170 was committed in `a6b11dfe`; old W01 is historical, not a reusable migration number or proof of safe database application/release.

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

Record the resolved official SHA. Analyze this official-to-official range separately from local changes since the sync landing. Build a feature map with explicit port/preserve/not-applicable/pending decisions; compare against B01–B16 in the current ledger rather than merging upstream GORM files wholesale. W01 remains historical; do not re-implement it merely because its old snapshot said uncommitted.

### Step 2: Identify Schema Changes

For each migration at the pinned official target:
1. Read the SQL and compare with local schemas and both SQL dialect trees. Map intent, not upstream numbering.
2. Express entity/query changes in [ent/schema](../../ent/schema/) and regenerate with `go generate .\ent` when needed.
3. Check [initialize/config.go](../../initialize/config.go), [initialize/version.go](../../initialize/version.go) and [the embedded migration loader](../../initialize/migrate/migrate.go). Generated ent code is not a database upgrade; add the required MySQL **and** PostgreSQL SQL up/down files for deployed databases.
4. Choose an unused local migration number after checking tracked **and untracked** migrations. Do not reuse committed 02170; never overwrite applied migrations. Distinguish MySQL driver loc from session time_zone, already-UTC history from Shanghai wall time, and existing timestamptz columns from conversion targets. Explain deliberately irreversible or wider-scope down migrations; do not assume ±8h data rewriting is idempotent.
5. First check embedded migration reachability. Test data conversion and rollback only against explicitly isolated databases with known historical timezone/data semantics.

### Step 3: Port Business Logic

For each upstream feature commit:
1. Read the GORM-based logic in `internal/module/*/`
2. Translate to entgo queries using `client.<Entity>.Query()` / `Create()` / `Update()`
3. Place in the corresponding `internal/logic/` package
4. Update `internal/handler/` if new API endpoints are added
5. Update `internal/types/` for new request/response types

Preserve binding-error early returns, GET/HEAD query binding despite JSON Content-Type, and HTTP-200 business envelopes. Explicitly write login identities in the account/wallet transaction, keep E.164 phone IDs, preserve unchanged OAuth avatars and omitted auth configuration, and distinguish omitted user rules from `[]`. Keep subscription pagination/filtering, inclusive calendar-date ranges and half-open Unix-millisecond traffic windows; do not copy upstream field units or update semantics without comparison.

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

Run the smallest existing test selection for the changed behavior; see [the latest verification matrix](../sync-diffs/v1.21.7-local-2026-11-19.md#本轮证据与后续最小验证) and [historical entry points](../sync-diffs/v1.21.7-local-2026-11-19.md#6-接手验证与证据边界). Escalate to `go test .\...`, build and lint only when the implementation scope requires them. Do not start the service against shared configuration merely to validate documentation: startup can apply SQL migrations. Real migration/OAuth/payment checks require explicitly isolated services. Mark skipped, unavailable and not-run checks separately from passed checks.

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
- Document a new official sync in `.ai/sync-diffs/<last-tag>-to-<new-tag>.md`; also update the dated local-customization ledger, Markdown file index, `.ai/sync-diffs/README.md`, `.ai/prompts/README.md` and [current-customizations.md](./current-customizations.md). Same-day local increments may append a pinned range/time or linked Markdown appendix. Preserve old hashes/verification and recompute cumulative file counts rather than adding overlapping lists.
- Keep project READMEs as entry points. Durable engineering rules go in the [development guide](../../doc/development.md) and its Chinese counterpart; Docker operations go in the installation guides. Do not move dated agent work, test output or temporary state back into README.

## References

- See `architecture-context.md` for detailed architecture differences
- See `gorm-to-entgo-translation.md` for translation patterns
- See `.ai/sync-diffs/` for previous sync records
