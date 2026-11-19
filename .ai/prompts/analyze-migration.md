# Analyze Migration Prompt

> Read [current-customizations.md](./current-customizations.md), [the latest ledger supplement](../sync-diffs/v1.21.7-local-2026-11-19.md#8-截止后增量至-57973bf2) and [the actual migration strategy](./architecture-context.md#2-migration-strategy-dual-system) first. The examples below describe schema intent, not deployable instructions or remaining feature work. This fork runs embedded MySQL **and PostgreSQL** SQL via golang-migrate at setup/upgrade; ent generation alone does not create deployed tables. For every schema decision below, also supply/check the SQL upgrade path in both dialects. Check tracked and untracked migration numbers: 02170 was committed in `a6b11dfe`; the old W01 worktree snapshot is historical. It is not an available number or evidence of safe application/rollback. Review the legacy Shanghai/UTC assumptions and dialect-specific rollback scope in B08. Never run conversion against shared/production data as part of analysis.

You are analyzing an upstream SQL migration and translating it to the local entgo-based system.

## Context

- **Upstream migration**: `<MIGRATION_PATH>`
- **Upstream ORM**: GORM
- **Local ORM**: entgo
- **Local migration location**: `initialize/migrate/database/mysql/`

For detailed architecture context, see `architecture-context.md`.
For GORM to entgo translation patterns, see `gorm-to-entgo-translation.md`.

## Task

1. Read the upstream migration SQL
2. Understand the intent (schema change, data migration, index optimization, etc.)
3. Determine the best local approach:
   - **entgo schema change** (preferred): Update `ent/schema/*.go`
   - **Hand-written SQL migration**: Create `initialize/migrate/database/mysql/0XXXX_*.sql`

## Steps

```bash
# Read the migration
git show <TAG>:<MIGRATION_PATH>

# Check existing local migrations
ls initialize/migrate/database/mysql/ | sort | tail -5

# Check current entgo schema
cat ent/schema/<ENTITY>.go
```

## Decision Tree

```
Is it a simple column add/remove/rename?
  → YES: Update ent schema/generated code AND the SQL upgrade path.

Is it a complex data migration (moving data between tables)?
  → YES: Write hand-written SQL for both supported dialects.

Is it an index optimization (BRIN, partial, fillfactor)?
  → YES: Write dialect-appropriate SQL and document non-applicable variants.

Is it a new table?
  → YES: Create ent schema/generated code AND table-creation SQL.

Is it dropping a column with data?
  → YES: Migrate data before the destructive schema/SQL step.
```

## Migration Numbering

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

## When to Use Hand-Written Migrations

- Data migrations (moving data between tables)
- Complex index creation (BRIN, partial indexes, fillfactor)
- Performance optimizations that entgo cannot express
- Upstream SQL migrations that cannot be translated to entgo schema changes

## Output

Provide:
1. Summary of what the migration does
2. Recommended approach (entgo vs hand-written SQL)
3. If entgo: the schema changes needed
4. If hand-written: the SQL migration files to create
5. Any logic changes needed in `internal/logic/`

## Example: Wallet Table Extraction

**Upstream migration**:
```sql
-- 02144_create_user_wallet.up.sql
CREATE TABLE `user_wallet` (
  `user_id` BIGINT NOT NULL,
  `balance` BIGINT NOT NULL DEFAULT 0,
  `gift_amount` BIGINT NOT NULL DEFAULT 0,
  `commission` BIGINT NOT NULL DEFAULT 0,
  `created_at` TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  `updated_at` TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`user_id`)
);

INSERT INTO `user_wallet` (`user_id`, `balance`, `gift_amount`, `commission`)
SELECT `id`, `balance`, `gift_amount`, `commission` FROM `user`;

ALTER TABLE `user` DROP COLUMN `balance`;
ALTER TABLE `user` DROP COLUMN `gift_amount`;
ALTER TABLE `user` DROP COLUMN `commission`;
```

**Local approach**:

1. **Create entgo schema** (`ent/schema/user_wallet.go`):
```go
func (UserWallet) Fields() []ent.Field {
    return []ent.Field{
        field.Int64("user_id").Unique(),
        field.Int64("balance").Default(0),
        field.Int64("gift_amount").Default(0),
        field.Int64("commission").Default(0),
    }
}

func (UserWallet) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("user", User.Type).
            Ref("wallet").
            Field("user_id").
            Unique().
            Required(),
    }
}
```

2. **Write hand-written data migration** (`initialize/migrate/database/mysql/0XXXX_migrate_user_balance_to_wallet.up.sql`):
```sql
INSERT INTO `user_wallet` (`user_id`, `balance`, `gift_amount`, `commission`, `created_at`, `updated_at`)
SELECT `id`, `balance`, `gift_amount`, `commission`, `created_at`, `updated_at` FROM `user`
WHERE `balance` > 0 OR `gift_amount` > 0 OR `commission` > 0;
```

3. **Update user schema** to drop old fields
4. **Run** `go generate ./ent`
5. **Update logic** to use UserWallet instead of User.balance
