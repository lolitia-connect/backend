CREATE INDEX IF NOT EXISTS idx_subscribe_id ON user_subscribe (subscribe_id);

DROP INDEX IF EXISTS idx_user_subscribe_plan_status_id;
DROP INDEX IF EXISTS idx_user_auth_methods_type_user;
DROP INDEX IF EXISTS idx_user_subscribe_lifecycle_traffic;
DROP INDEX IF EXISTS idx_user_subscribe_active_expire;
