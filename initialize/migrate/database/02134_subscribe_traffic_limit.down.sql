-- Purpose: Rollback traffic_limit rules from subscribe
-- Author: Claude Code
-- Date: 2026-03-12

-- ===== Remove traffic_limit column from subscribe table =====
ALTER TABLE `subscribe` DROP COLUMN IF EXISTS `traffic_limit`;
