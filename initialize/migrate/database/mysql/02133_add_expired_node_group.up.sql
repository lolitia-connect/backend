-- 为 node_group 表添加过期节点组相关字段
SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND COLUMN_NAME = 'is_expired_group');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `node_group` ADD COLUMN `is_expired_group` tinyint(1) NOT NULL DEFAULT 0 COMMENT ''Is Expired Group: 0=normal, 1=expired group'' AFTER `for_calculation`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND COLUMN_NAME = 'expired_days_limit');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `node_group` ADD COLUMN `expired_days_limit` int NOT NULL DEFAULT 7 COMMENT ''Expired days limit (days)'' AFTER `is_expired_group`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND COLUMN_NAME = 'max_traffic_gb_expired');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `node_group` ADD COLUMN `max_traffic_gb_expired` bigint DEFAULT 0 COMMENT ''Max traffic for expired users (GB)'' AFTER `expired_days_limit`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND COLUMN_NAME = 'speed_limit');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `node_group` ADD COLUMN `speed_limit` int NOT NULL DEFAULT 0 COMMENT ''Speed limit (KB/s)'' AFTER `max_traffic_gb_expired`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- 添加索引
SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND INDEX_NAME = 'idx_is_expired_group');
SET @sql = IF(@index_exists = 0, 'ALTER TABLE `node_group` ADD INDEX `idx_is_expired_group` (`is_expired_group`)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- 为 user_subscribe 表添加过期流量统计字段
SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'expired_download');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `user_subscribe` ADD COLUMN `expired_download` bigint NOT NULL DEFAULT 0 COMMENT ''Expired period download traffic (bytes)'' AFTER `upload`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'expired_upload');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `user_subscribe` ADD COLUMN `expired_upload` bigint NOT NULL DEFAULT 0 COMMENT ''Expired period upload traffic (bytes)'' AFTER `expired_download`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
