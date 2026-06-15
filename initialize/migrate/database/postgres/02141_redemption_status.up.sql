-- Add status column to redemption_code table (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'redemption_code' AND column_name = 'status') THEN
        ALTER TABLE "redemption_code" ADD COLUMN "status" SMALLINT NOT NULL DEFAULT 1;
    END IF;
END $$;
