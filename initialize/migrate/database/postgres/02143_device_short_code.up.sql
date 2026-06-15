-- Add short_code column to user_device table (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'user_device' AND column_name = 'short_code') THEN
        ALTER TABLE "user_device" ADD COLUMN "short_code" VARCHAR(255) DEFAULT '';
    END IF;
END $$;
