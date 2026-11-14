CREATE INDEX IF NOT EXISTS idx_timestamp ON traffic_log (timestamp);
CREATE INDEX IF NOT EXISTS idx_user_id ON traffic_log (user_id);
CREATE INDEX IF NOT EXISTS idx_server_id ON traffic_log (server_id);

DROP INDEX IF EXISTS idx_traffic_log_timestamp_brin;
DROP INDEX IF EXISTS idx_traffic_user_sub_time;
DROP INDEX IF EXISTS idx_traffic_server_time;
