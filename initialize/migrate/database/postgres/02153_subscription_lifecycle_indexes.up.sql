-- Cover the subscription lifecycle sweeps and the per-user auth-method lookups.
-- Ported from upstream v1.20.3 (02161/02162 subscription expiry, 02152 lifecycle
-- indexes, 02174 user_subscribe hot index).
--
-- The partial index replaces upstream's plain (status, finished_at, expire_time)
-- one: only active, unfinished subscriptions are ever swept by expiry.

CREATE INDEX IF NOT EXISTS idx_user_subscribe_active_expire
    ON user_subscribe (expire_time, id)
    WHERE status IN (0, 1) AND finished_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_subscribe_lifecycle_traffic
    ON user_subscribe (status, traffic);

CREATE INDEX IF NOT EXISTS idx_user_auth_methods_type_user
    ON user_auth_methods (auth_type, user_id);

CREATE INDEX IF NOT EXISTS idx_user_subscribe_plan_status_id
    ON user_subscribe (subscribe_id, status, id);

-- Superseded by idx_user_subscribe_plan_status_id.
DROP INDEX IF EXISTS idx_subscribe_id;
