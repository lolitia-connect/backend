SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND COLUMN_NAME = 'group_type');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `node_group` ADD COLUMN `group_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT ''common'' COMMENT ''Node group type: common, subscribe, app'' AFTER `name`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE `node_group`
SET `group_type` = 'common'
WHERE `group_type` = '';
