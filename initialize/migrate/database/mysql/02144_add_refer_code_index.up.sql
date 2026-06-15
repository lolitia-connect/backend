-- Add index on refer_code column for faster lookup (idempotent)
SET @idx_exists := (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND INDEX_NAME = 'idx_refer_code'
);
SET @query := IF(
    @idx_exists = 0,
    'ALTER TABLE `user` ADD INDEX `idx_refer_code` (`refer_code`)',
    'SELECT 1'
);
PREPARE stmt FROM @query;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
