-- Purpose: Add traffic_limit rules to subscribe
-- Author: Claude Code
-- Date: 2026-03-12

-- ===== Add traffic_limit column to subscribe table =====
SET @column_exists = (
  SELECT COUNT(*)
  FROM INFORMATION_SCHEMA.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'subscribe'
    AND COLUMN_NAME = 'traffic_limit'
);

SET @sql = IF(
  @column_exists = 0,
  'ALTER TABLE `subscribe` ADD COLUMN `traffic_limit` TEXT NULL COMMENT ''Traffic Limit Rules (JSON)'' AFTER `node_group_id`',
  'SELECT ''Column traffic_limit already exists in subscribe table'''
);

PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
