-- The audit log list filters by type and drills down by object id, ordered by id.
-- The full-text index on content is never queried and only costs writes.
-- Ported from upstream v1.20.3 (02176 mysql_system_log_hot_index).

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'system_logs' AND INDEX_NAME = 'idx_system_log_type_object_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_system_log_type_object_id ON system_logs (type, object_id, id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
