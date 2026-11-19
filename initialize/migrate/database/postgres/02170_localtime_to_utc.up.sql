-- 02170 localtime -> UTC data upgrade (PostgreSQL).
--
-- The panel historically stored naive "timestamp without time zone" values in
-- its local timezone, whose default has always been Asia/Shanghai (+08:00).
-- The database is now unified on UTC storage, so every naive timestamp column
-- is promoted to "timestamptz", interpreting the stored wall-clock in the
-- panel's historical local zone (per-row, so DST would be handled correctly).
-- This matches migration 02148, which already converted servers.last_reported_at
-- to timestamptz; columns that are already timestamptz are skipped here.
--
-- NOTE: this assumes legacy data was written in Asia/Shanghai, which was the
-- shipped default everywhere (orm TimeZone, scheduler, mysql2postgres).
-- Deployments that overrode the timezone must adapt this migration.
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT c.table_name, c.column_name
        FROM information_schema.columns c
        JOIN information_schema.tables t
          ON t.table_schema = c.table_schema AND t.table_name = c.table_name
        WHERE c.table_schema = current_schema()
          AND t.table_type = 'BASE TABLE'
          AND c.data_type = 'timestamp without time zone'
    LOOP
        EXECUTE format(
            'ALTER TABLE %I.%I ALTER COLUMN %I TYPE timestamptz USING %I AT TIME ZONE %L',
            current_schema(), r.table_name, r.column_name, r.column_name, 'Asia/Shanghai'
        );
    END LOOP;
END $$;
