-- Add short_code column to user_device table (idempotent)
SET @col_exists := (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_device' AND COLUMN_NAME = 'short_code'
);
SET @query := IF(
    @col_exists = 0,
    'ALTER TABLE `user_device` ADD COLUMN `short_code` VARCHAR(255) DEFAULT '''' COMMENT ''Short Code'' AFTER `identifier`',
    'SELECT 1'
);
PREPARE stmt FROM @query;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
