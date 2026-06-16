SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'server_rule_group' AND COLUMN_NAME = '`default`');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `server_rule_group` ADD COLUMN `default` TINYINT(1) NOT NULL DEFAULT 0 COMMENT ''Is Default Group''', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'server_rule_group' AND COLUMN_NAME = 'type');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `server_rule_group` ADD COLUMN `type` VARCHAR(100) NOT NULL DEFAULT '''' COMMENT ''Rule Group Type''', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;