SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_log' AND INDEX_NAME = 'idx_traffic_user_sub_time');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_traffic_user_sub_time ON traffic_log', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_log' AND INDEX_NAME = 'idx_traffic_server_time');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_traffic_server_time ON traffic_log', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_log' AND INDEX_NAME = 'idx_server_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_server_id ON traffic_log (server_id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_log' AND INDEX_NAME = 'idx_user_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_user_id ON traffic_log (user_id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
