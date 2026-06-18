SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'group_id');
SET @sql = IF(@column_exists > 0, 'ALTER TABLE `subscribe` DROP COLUMN `group_id`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'language');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `subscribe` ADD COLUMN `language` VARCHAR(255) NOT NULL DEFAULT '''' COMMENT ''Language'' AFTER `name`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

DROP TABLE IF EXISTS `subscribe_group`;