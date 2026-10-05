-- The money columns leave the identity-owned user row now that every reader and
-- writer uses the billing-owned `user_wallet` table (created and backfilled by
-- 02158). Ported from upstream v1.20.3 (02144_drop_user_wallet_columns).

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'balance');
SET @sql = IF(@column_exists > 0, 'ALTER TABLE `user` DROP COLUMN `balance`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'gift_amount');
SET @sql = IF(@column_exists > 0, 'ALTER TABLE `user` DROP COLUMN `gift_amount`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @column_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'commission');
SET @sql = IF(@column_exists > 0, 'ALTER TABLE `user` DROP COLUMN `commission`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
