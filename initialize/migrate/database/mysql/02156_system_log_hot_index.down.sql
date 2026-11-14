SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'system_logs' AND INDEX_NAME = 'idx_system_log_type_object_id');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_system_log_type_object_id ON system_logs', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
