-- The money columns leave the identity-owned user row now that every reader and
-- writer uses the billing-owned `user_wallet` table (created and backfilled by
-- 02158). Ported from upstream v1.20.3 (02144_drop_user_wallet_columns).

ALTER TABLE "user"
    DROP COLUMN IF EXISTS "balance",
    DROP COLUMN IF EXISTS "gift_amount",
    DROP COLUMN IF EXISTS "commission";
