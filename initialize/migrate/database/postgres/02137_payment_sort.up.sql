-- 2026-04-02 00:00:00
-- Purpose: Add sort column for payment methods

ALTER TABLE "payment"
    ADD COLUMN IF NOT EXISTS "sort" bigint NOT NULL DEFAULT 0;

UPDATE "payment"
SET "sort" = "id"
WHERE "sort" = 0;
