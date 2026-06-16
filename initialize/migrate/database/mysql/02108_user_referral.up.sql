SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'referral_percentage');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `user` ADD COLUMN `referral_percentage` TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT ''Referral Percentage'' AFTER `commission`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'only_first_purchase');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `user` ADD COLUMN `only_first_purchase` TINYINT(1) NOT NULL DEFAULT 1 COMMENT ''Only First Purchase'' AFTER `referral_percentage`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
