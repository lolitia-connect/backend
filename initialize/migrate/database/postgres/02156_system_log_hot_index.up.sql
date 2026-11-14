-- The audit log list filters by type and drills down by object id, ordered by id.
-- The full-text index on content is never queried and only costs writes.
-- Ported from upstream v1.20.3 (02166 pg_drop_unused_system_log_fts).

CREATE INDEX IF NOT EXISTS idx_system_log_type_object_id
    ON system_logs (type, object_id, id);

DROP INDEX IF EXISTS idx_content_gin;
