-- 02170 UTC -> localtime rollback (PostgreSQL).
--
-- Revert the timestamptz columns produced by the up migration back to naive
-- "timestamp without time zone", rendering the instant in the panel's
-- historical local zone (Asia/Shanghai).
--
-- servers.last_reported_at predates this migration (it became timestamptz in
-- 02148) and must remain timestamptz, so it is explicitly excluded.
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
          AND c.data_type = 'timestamp with time zone'
          AND NOT (c.table_name = 'servers' AND c.column_name = 'last_reported_at')
    LOOP
        EXECUTE format(
            'ALTER TABLE %I.%I ALTER COLUMN %I TYPE timestamp USING %I AT TIME ZONE %L',
            current_schema(), r.table_name, r.column_name, r.column_name, 'Asia/Shanghai'
        );
    END LOOP;
END $$;
