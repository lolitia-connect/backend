SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'system_logs' AND INDEX_NAME = 'idx_type_date');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_type_date ON system_logs (type, date)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
