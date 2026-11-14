-- Order listing, reconciliation and reporting all filter on status and page by
-- id or created_at. Ported from upstream v1.20.3 (02173 mysql_order_hot_indexes).

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_status_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_order_status_id ON `order` (status, id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_status_created_at');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_order_status_created_at ON `order` (status, created_at)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_user_status_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_order_user_status_id ON `order` (user_id, status, id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND INDEX_NAME = 'idx_order_subscribe_status_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_order_subscribe_status_id ON `order` (subscribe_id, status, id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
