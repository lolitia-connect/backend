-- Add expired node group fields to node_group table
ALTER TABLE "node_group"
ADD COLUMN "is_expired_group" BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN "expired_days_limit" INTEGER NOT NULL DEFAULT 7,
ADD COLUMN "max_traffic_gb_expired" bigint DEFAULT 0,
ADD COLUMN "speed_limit" INTEGER NOT NULL DEFAULT 0;

-- Add index
CREATE INDEX IF NOT EXISTS "idx_node_group_is_expired_group" ON "node_group" ("is_expired_group");

-- Add expired traffic fields to user_subscribe table
ALTER TABLE "user_subscribe"
ADD COLUMN "expired_download" bigint NOT NULL DEFAULT 0,
ADD COLUMN "expired_upload" bigint NOT NULL DEFAULT 0;
