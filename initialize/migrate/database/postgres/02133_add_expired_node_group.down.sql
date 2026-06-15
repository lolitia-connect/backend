-- Rollback expired traffic fields from user_subscribe table
ALTER TABLE "user_subscribe"
DROP COLUMN IF EXISTS "expired_upload",
DROP COLUMN IF EXISTS "expired_download";

-- Rollback expired node group fields from node_group table
DROP INDEX IF EXISTS "idx_node_group_is_expired_group";
ALTER TABLE "node_group"
DROP COLUMN IF EXISTS "speed_limit",
DROP COLUMN IF EXISTS "max_traffic_gb_expired",
DROP COLUMN IF EXISTS "expired_days_limit",
DROP COLUMN IF EXISTS "is_expired_group";
