-- Traffic aggregation and the per-user reporting pages read traffic_log by
-- (server_id, timestamp) and (user_id, subscribe_id, timestamp). The BRIN index
-- covers the append-only timestamp column cheaply.
-- Ported from upstream v1.20.3 (02163 pg_traffic_brin, 02169 drop redundant index).

CREATE INDEX IF NOT EXISTS idx_traffic_server_time
    ON traffic_log (server_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_traffic_user_sub_time
    ON traffic_log (user_id, subscribe_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_traffic_log_timestamp_brin
    ON traffic_log USING BRIN (timestamp)
    WITH (pages_per_range = 128, autosummarize = on);

-- Superseded by idx_traffic_server_time.
DROP INDEX IF EXISTS idx_server_id;

-- Superseded by idx_traffic_user_sub_time and the BRIN index.
DROP INDEX IF EXISTS idx_user_id;
DROP INDEX IF EXISTS idx_timestamp;
