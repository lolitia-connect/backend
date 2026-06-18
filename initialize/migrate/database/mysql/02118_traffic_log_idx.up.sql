SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_log' AND INDEX_NAME = 'idx_timestamp');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_timestamp ON traffic_log (timestamp)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

