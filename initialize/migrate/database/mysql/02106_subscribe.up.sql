SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'nodes');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `subscribe` ADD COLUMN `nodes` VARCHAR(255) NOT NULL DEFAULT '''' COMMENT ''Node IDs''', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'node_tags');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `subscribe` ADD COLUMN `node_tags` VARCHAR(255) NOT NULL DEFAULT '''' COMMENT ''Node Tags''', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'server');
SET @sql = IF(@column_exists > 0, 'ALTER TABLE `subscribe` DROP COLUMN `server`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'server_group');
SET @sql = IF(@column_exists > 0, 'ALTER TABLE `subscribe` DROP COLUMN `server_group`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

DROP TABLE IF EXISTS `server_rule_group`;
