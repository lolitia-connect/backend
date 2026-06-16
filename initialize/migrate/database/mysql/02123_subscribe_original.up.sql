SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'show_original_price');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `subscribe` ADD COLUMN `show_original_price` TINYINT(1) NOT NULL DEFAULT 0 COMMENT ''display the original price: 0 not display, 1 display'' AFTER `created_at`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
