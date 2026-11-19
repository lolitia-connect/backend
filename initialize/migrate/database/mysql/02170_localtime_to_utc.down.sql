-- 02170 localtime <-> UTC data upgrade (MySQL) [down].
--
-- The panel historically stored naive DATETIME values in its local timezone,
-- whose default has always been Asia/Shanghai (+08:00, no DST in the data era),
-- so the database is now unified on UTC storage by shifting every legacy naive
-- DATETIME by 8 hours: up subtracts 8h (local -> UTC), down adds 8h (UTC ->
-- local). DATE_SUB/DATE_ADD work across the whole DATETIME range (1000-9999),
-- unlike CONVERT_TZ which only covers the TIMESTAMP window (1970-2038) and would
-- leave far-future expiries and epoch sentinels unconverted.
--
-- Every column is converted through a guarded dynamic statement: the UPDATE runs
-- only when that datetime/timestamp column actually exists in the current schema,
-- so the migration is safe across deployments whose table set drifted from ent
-- (e.g. legacy "server" vs "servers", or an absent "subscribe_group"). The shift
-- is wrapped in COALESCE so sentinel values it cannot shift (0000-00-00 zero
-- dates, or results outside the DATETIME range) keep their original value instead
-- of nulling a NOT NULL column.
--
-- NOTE: assumes legacy data was written in Asia/Shanghai, the shipped default
-- (orm loc, scheduler, mysql2postgres). Deployments that overrode the timezone
-- must adapt this migration before applying.

SET @ppanel_old_sql_mode = @@SESSION.sql_mode;
SET SESSION sql_mode = 'NO_ENGINE_SUBSTITUTION';

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `ads` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'ads' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `ads` SET `end_time` = COALESCE(DATE_ADD(`end_time`, INTERVAL 8 HOUR), `end_time`) WHERE `end_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'ads' AND COLUMN_NAME = 'end_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `ads` SET `start_time` = COALESCE(DATE_ADD(`start_time`, INTERVAL 8 HOUR), `start_time`) WHERE `start_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'ads' AND COLUMN_NAME = 'start_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `ads` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'ads' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `announcement` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'announcement' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `announcement` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'announcement' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `auth_method` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'auth_method' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `auth_method` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'auth_method' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `coupon` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'coupon' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `coupon` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'coupon' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `document` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'document' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `document` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'document' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `group_history` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_history' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `group_history` SET `end_time` = COALESCE(DATE_ADD(`end_time`, INTERVAL 8 HOUR), `end_time`) WHERE `end_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_history' AND COLUMN_NAME = 'end_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `group_history` SET `start_time` = COALESCE(DATE_ADD(`start_time`, INTERVAL 8 HOUR), `start_time`) WHERE `start_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_history' AND COLUMN_NAME = 'start_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `group_history_detail` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'group_history_detail' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `node_group` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `node_group` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'node_group' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `nodes` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'nodes' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `nodes` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'nodes' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `order` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `order` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `order_event` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order_event' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `order_event` SET `published_at` = COALESCE(DATE_ADD(`published_at`, INTERVAL 8 HOUR), `published_at`) WHERE `published_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'order_event' AND COLUMN_NAME = 'published_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `redemption_code` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'redemption_code' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `redemption_code` SET `deleted_at` = COALESCE(DATE_ADD(`deleted_at`, INTERVAL 8 HOUR), `deleted_at`) WHERE `deleted_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'redemption_code' AND COLUMN_NAME = 'deleted_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `redemption_code` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'redemption_code' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `redemption_record` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'redemption_record' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `redemption_record` SET `redeemed_at` = COALESCE(DATE_ADD(`redeemed_at`, INTERVAL 8 HOUR), `redeemed_at`) WHERE `redeemed_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'redemption_record' AND COLUMN_NAME = 'redeemed_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `server` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'server' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `server` SET `last_reported_at` = COALESCE(DATE_ADD(`last_reported_at`, INTERVAL 8 HOUR), `last_reported_at`) WHERE `last_reported_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'server' AND COLUMN_NAME = 'last_reported_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `server` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'server' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `server_config_overrides` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'server_config_overrides' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `server_config_overrides` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'server_config_overrides' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `servers` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'servers' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `servers` SET `last_reported_at` = COALESCE(DATE_ADD(`last_reported_at`, INTERVAL 8 HOUR), `last_reported_at`) WHERE `last_reported_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'servers' AND COLUMN_NAME = 'last_reported_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `servers` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'servers' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscribe` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscribe` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscribe_application` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe_application' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscribe_application` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe_application' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscribe_group` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe_group' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscribe_group` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe_group' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_entitlement` SET `grace_until` = COALESCE(DATE_ADD(`grace_until`, INTERVAL 8 HOUR), `grace_until`) WHERE `grace_until` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_entitlement' AND COLUMN_NAME = 'grace_until'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_entitlement` SET `revoked_at` = COALESCE(DATE_ADD(`revoked_at`, INTERVAL 8 HOUR), `revoked_at`) WHERE `revoked_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_entitlement' AND COLUMN_NAME = 'revoked_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_entitlement` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_entitlement' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_entitlement_revision` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_entitlement_revision' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_period` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_period' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_period` SET `end_at` = COALESCE(DATE_ADD(`end_at`, INTERVAL 8 HOUR), `end_at`) WHERE `end_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_period' AND COLUMN_NAME = 'end_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_period` SET `start_at` = COALESCE(DATE_ADD(`start_at`, INTERVAL 8 HOUR), `start_at`) WHERE `start_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_period' AND COLUMN_NAME = 'start_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `subscription_period` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscription_period' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `system` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'system' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `system` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'system' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `system_logs` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'system_logs' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `task` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'task' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `task` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'task' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `ticket` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'ticket' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `ticket` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'ticket' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `ticket_follow` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'ticket_follow' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `traffic_batch` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_batch' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `traffic_log` SET `timestamp` = COALESCE(DATE_ADD(`timestamp`, INTERVAL 8 HOUR), `timestamp`) WHERE `timestamp` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'traffic_log' AND COLUMN_NAME = 'timestamp'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user` SET `deleted_at` = COALESCE(DATE_ADD(`deleted_at`, INTERVAL 8 HOUR), `deleted_at`) WHERE `deleted_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'deleted_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_auth_methods` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_auth_methods' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_auth_methods` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_auth_methods' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_device` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_device' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_device` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_device' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_device_online_record` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_device_online_record' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_device_online_record` SET `offline_time` = COALESCE(DATE_ADD(`offline_time`, INTERVAL 8 HOUR), `offline_time`) WHERE `offline_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_device_online_record' AND COLUMN_NAME = 'offline_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_device_online_record` SET `online_time` = COALESCE(DATE_ADD(`online_time`, INTERVAL 8 HOUR), `online_time`) WHERE `online_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_device_online_record' AND COLUMN_NAME = 'online_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_subscribe` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_subscribe` SET `expire_time` = COALESCE(DATE_ADD(`expire_time`, INTERVAL 8 HOUR), `expire_time`) WHERE `expire_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'expire_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_subscribe` SET `finished_at` = COALESCE(DATE_ADD(`finished_at`, INTERVAL 8 HOUR), `finished_at`) WHERE `finished_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'finished_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_subscribe` SET `start_time` = COALESCE(DATE_ADD(`start_time`, INTERVAL 8 HOUR), `start_time`) WHERE `start_time` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'start_time'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_subscribe` SET `traffic_reset_at` = COALESCE(DATE_ADD(`traffic_reset_at`, INTERVAL 8 HOUR), `traffic_reset_at`) WHERE `traffic_reset_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'traffic_reset_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_subscribe` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_wallet` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_wallet' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `user_wallet` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_wallet' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `withdrawals` SET `created_at` = COALESCE(DATE_ADD(`created_at`, INTERVAL 8 HOUR), `created_at`) WHERE `created_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'withdrawals' AND COLUMN_NAME = 'created_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET @ppanel_tz_sql := (SELECT IF(COUNT(*) = 1,
  'UPDATE `withdrawals` SET `updated_at` = COALESCE(DATE_ADD(`updated_at`, INTERVAL 8 HOUR), `updated_at`) WHERE `updated_at` IS NOT NULL',
  'DO 0')
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'withdrawals' AND COLUMN_NAME = 'updated_at'
    AND DATA_TYPE IN ('datetime', 'timestamp'));
PREPARE ppanel_tz_stmt FROM @ppanel_tz_sql;
EXECUTE ppanel_tz_stmt;
DEALLOCATE PREPARE ppanel_tz_stmt;

SET SESSION sql_mode = @ppanel_old_sql_mode;
