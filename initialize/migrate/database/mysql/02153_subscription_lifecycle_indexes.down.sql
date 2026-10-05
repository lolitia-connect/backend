-- MySQL has no IF EXISTS for DROP INDEX, so every drop is guarded.

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND INDEX_NAME = 'idx_subscribe_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_subscribe_id ON user_subscribe (subscribe_id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
