-- Add index on refer_code column for faster lookup
CREATE INDEX IF NOT EXISTS "idx_user_refer_code" ON "user" ("refer_code");
