-- Remove short_code column from user_device table
ALTER TABLE "user_device" DROP COLUMN IF EXISTS "short_code";
