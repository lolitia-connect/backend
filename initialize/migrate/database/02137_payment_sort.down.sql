-- 2026-04-02 00:00:00
-- Purpose: Remove sort column for payment methods

SET FOREIGN_KEY_CHECKS = 0;

SET @column_exists = (SELECT COUNT(*)
                      FROM INFORMATION_SCHEMA.COLUMNS
                      WHERE TABLE_SCHEMA = DATABASE()
                        AND TABLE_NAME = 'payment'
                        AND COLUMN_NAME = 'sort');
SET @sql = IF(@column_exists > 0,
              'ALTER TABLE `payment` DROP COLUMN `sort`',
              'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET FOREIGN_KEY_CHECKS = 1;
