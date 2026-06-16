SET @column_exists = (
    SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'user'
      AND COLUMN_NAME = 'rules'
);
SET @sql = IF(@column_exists = 0,
    'ALTER TABLE `user` ADD COLUMN `rules` TEXT NULL COMMENT ''User rules for subscription'' AFTER `created_at`',
    'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
