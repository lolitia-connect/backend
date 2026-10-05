SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_subscribe_status_id');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_order_subscribe_status_id ON `order`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_user_status_id');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_order_user_status_id ON `order`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_status_created_at');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_order_status_created_at ON `order`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_status_id');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_order_status_id ON `order`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
