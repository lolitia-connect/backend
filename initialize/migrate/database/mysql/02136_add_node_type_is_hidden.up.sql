SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'nodes' AND COLUMN_NAME = 'node_type');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `nodes` ADD COLUMN `node_type` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT ''landing'' COMMENT ''Node type: front, landing'' AFTER `enabled`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'nodes' AND COLUMN_NAME = 'is_hidden');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `nodes` ADD COLUMN `is_hidden` tinyint(1) NOT NULL DEFAULT 0 COMMENT ''Hidden - users cannot see hidden nodes'' AFTER `node_type`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE `nodes`
SET `node_type` = 'landing'
WHERE `node_type` = '';