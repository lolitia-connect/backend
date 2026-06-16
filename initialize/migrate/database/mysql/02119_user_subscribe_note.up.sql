SET @column_exists = (
    SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'user_subscribe'
      AND COLUMN_NAME = 'note'
);
SET @sql = IF(@column_exists = 0,
    'ALTER TABLE `user_subscribe` ADD COLUMN `note` VARCHAR(500) NOT NULL DEFAULT '''' COMMENT ''User note for subscription'' AFTER `status`',
    'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
