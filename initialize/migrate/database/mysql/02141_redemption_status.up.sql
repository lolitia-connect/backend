-- Add status column to redemption_code table (idempotent)
SET @col_exists := (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'redemption_code' AND COLUMN_NAME = 'status'
);
SET @query := IF(
    @col_exists = 0,
    'ALTER TABLE `redemption_code` ADD COLUMN `status` TINYINT NOT NULL DEFAULT 1 COMMENT ''Status: 1=enabled, 0=disabled'' AFTER `quantity`',
    'SELECT 1'
);
PREPARE stmt FROM @query;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
