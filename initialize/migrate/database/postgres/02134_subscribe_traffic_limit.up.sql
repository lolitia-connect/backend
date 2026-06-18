-- Purpose: Add traffic_limit rules to subscribe
-- Author: Claude Code
-- Date: 2026-03-12

-- ===== Add traffic_limit column to subscribe table =====
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'subscribe' AND column_name = 'traffic_limit') THEN
        ALTER TABLE "subscribe" ADD COLUMN "traffic_limit" TEXT DEFAULT NULL;
    END IF;
END $$;
