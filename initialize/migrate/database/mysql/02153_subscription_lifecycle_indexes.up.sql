-- Cover the subscription lifecycle sweeps and the per-user auth-method lookups.
-- Ported from upstream v1.20.3 (02152_subscription_lifecycle_indexes plus the
-- user_subscribe hot index from 02174).

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND INDEX_NAME = 'idx_user_subscribe_lifecycle_expire');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_user_subscribe_lifecycle_expire ON user_subscribe (status, finished_at, expire_time)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND INDEX_NAME = 'idx_user_subscribe_lifecycle_traffic');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_user_subscribe_lifecycle_traffic ON user_subscribe (status, traffic)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_auth_methods' AND INDEX_NAME = 'idx_user_auth_methods_type_user');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_user_auth_methods_type_user ON user_auth_methods (auth_type, user_id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- The plan-scoped listing pages by (subscribe_id, status, id); the single-column
-- index is a strict prefix of it and would only cost writes.
SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND INDEX_NAME = 'idx_user_subscribe_plan_status_id');
SET @sql = IF(@index_exists = 0, 'CREATE INDEX idx_user_subscribe_plan_status_id ON user_subscribe (subscribe_id, status, id)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND INDEX_NAME = 'idx_subscribe_id');
SET @sql = IF(@index_exists = 1, 'DROP INDEX idx_subscribe_id ON user_subscribe', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
