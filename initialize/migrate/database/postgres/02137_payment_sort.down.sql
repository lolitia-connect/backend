-- 2026-04-02 00:00:00
-- Purpose: Remove sort column for payment methods

ALTER TABLE "payment"
    DROP COLUMN IF EXISTS "sort";
