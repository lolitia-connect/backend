-- Purpose: Add node group management tables with multi-group support
-- Author: Tension
-- Date: 2025-02-23
-- Updated: 2025-03-06

-- ===== Create node_group table =====
DROP TABLE IF EXISTS `node_group`;
CREATE TABLE IF NOT EXISTS `node_group` (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT 'Name',
    `description` varchar(500) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci DEFAULT NULL COMMENT 'Group Description',
    `sort` int NOT NULL DEFAULT '0' COMMENT 'Sort Order',
    `for_calculation` tinyint(1) NOT NULL DEFAULT 1 COMMENT 'For Grouping Calculation: 0=false, 1=true',
    `min_traffic_gb` bigint DEFAULT 0 COMMENT 'Minimum Traffic (GB) for this node group',
    `max_traffic_gb` bigint DEFAULT 0 COMMENT 'Maximum Traffic (GB) for this node group',
    `created_at` datetime(3) DEFAULT NULL COMMENT 'Create Time',
    `updated_at` datetime(3) DEFAULT NULL COMMENT 'Update Time',
    PRIMARY KEY (`id`),
    KEY `idx_sort` (`sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='Node Groups';

-- ===== Create group_history table =====
DROP TABLE IF EXISTS `group_history`;
CREATE TABLE IF NOT EXISTS `group_history` (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `group_mode` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT 'Group Mode: average/subscribe/traffic',
    `trigger_type` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT 'Trigger Type: manual/auto/schedule',
    `state` varchar(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '' COMMENT 'State: pending/running/completed/failed',
    `total_users` int NOT NULL DEFAULT '0' COMMENT 'Total Users',
    `success_count` int NOT NULL DEFAULT '0' COMMENT 'Success Count',
    `failed_count` int NOT NULL DEFAULT '0' COMMENT 'Failed Count',
    `start_time` datetime(3) DEFAULT NULL COMMENT 'Start Time',
    `end_time` datetime(3) DEFAULT NULL COMMENT 'End Time',
    `operator` varchar(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci DEFAULT NULL COMMENT 'Operator',
    `error_message` text CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci COMMENT 'Error Message',
    `created_at` datetime(3) DEFAULT NULL COMMENT 'Create Time',
    PRIMARY KEY (`id`),
    KEY `idx_group_mode` (`group_mode`),
    KEY `idx_trigger_type` (`trigger_type`),
    KEY `idx_state` (`state`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='Group Calculation History';

-- ===== Create group_history_detail table =====
-- Note: user_group_id column removed, using user_data JSON field instead
DROP TABLE IF EXISTS `group_history_detail`;
CREATE TABLE IF NOT EXISTS `group_history_detail` (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `history_id` bigint NOT NULL COMMENT 'History ID',
    `node_group_id` bigint NOT NULL COMMENT 'Node Group ID',
    `user_count` int NOT NULL DEFAULT '0' COMMENT 'User Count',
    `node_count` int NOT NULL DEFAULT '0' COMMENT 'Node Count',
    `user_data` TEXT COMMENT 'User data JSON (id and email/phone)',
    `created_at` datetime(3) DEFAULT NULL COMMENT 'Create Time',
    PRIMARY KEY (`id`),
    KEY `idx_history_id` (`history_id`),
    KEY `idx_node_group_id` (`node_group_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='Group History Details';

-- ===== Add columns to nodes table =====
SET @column_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'nodes' AND COLUMN_NAME = 'node_group_ids');
SET @sql = IF(@column_exists = 0,
    'ALTER TABLE `nodes` ADD COLUMN `node_group_ids` JSON COMMENT ''Node Group IDs (JSON array, multiple groups)''',
    'SELECT ''Column node_group_ids already exists''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ===== Add node_group_id column to user_subscribe table =====
SET @column_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'node_group_id');
SET @sql = IF(@column_exists = 0,
    'ALTER TABLE `user_subscribe` ADD COLUMN `node_group_id` bigint NOT NULL DEFAULT 0 COMMENT ''Node Group ID (single ID)''',
    'SELECT ''Column node_group_id already exists''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ===== Add index for user_subscribe.node_group_id =====
SET @index_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND INDEX_NAME = 'idx_node_group_id');
SET @sql = IF(@index_exists = 0,
    'ALTER TABLE `user_subscribe` ADD INDEX `idx_node_group_id` (`node_group_id`)',
    'SELECT ''Index idx_node_group_id already exists''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ===== Add group_locked column to user_subscribe table =====
SET @column_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_subscribe' AND COLUMN_NAME = 'group_locked');
SET @sql = IF(@column_exists = 0,
    'ALTER TABLE `user_subscribe` ADD COLUMN `group_locked` tinyint(1) NOT NULL DEFAULT 0 COMMENT ''Group Locked''',
    'SELECT ''Column group_locked already exists in user_subscribe table''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ===== Add columns to subscribe table =====
SET @column_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'node_group_ids');
SET @sql = IF(@column_exists = 0,
    'ALTER TABLE `subscribe` ADD COLUMN `node_group_ids` JSON COMMENT ''Node Group IDs (JSON array, multiple groups)''',
    'SELECT ''Column node_group_ids already exists''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ===== Add default node_group_id column to subscribe table =====
SET @column_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND COLUMN_NAME = 'node_group_id');
SET @sql = IF(@column_exists = 0,
    'ALTER TABLE `subscribe` ADD COLUMN `node_group_id` bigint NOT NULL DEFAULT 0 COMMENT ''Default Node Group ID (single ID)''',
    'SELECT ''Column node_group_id already exists in subscribe table''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ===== Add index for subscribe.node_group_id =====
SET @index_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'subscribe' AND INDEX_NAME = 'idx_node_group_id');
SET @sql = IF(@index_exists = 0,
    'ALTER TABLE `subscribe` ADD INDEX `idx_node_group_id` (`node_group_id`)',
    'SELECT ''Index idx_node_group_id already exists in subscribe table''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ===== Insert system configuration entries =====
INSERT INTO `system` (`category`, `key`, `value`, `desc`) VALUES
    ('group', 'enabled', 'false', 'Group Management Enabled'),
    ('group', 'mode', 'average', 'Group Mode: average/subscribe/traffic'),
    ('group', 'auto_create_group', 'false', 'Auto-create user group when creating subscribe product')
ON DUPLICATE KEY UPDATE
    `value` = VALUES(`value`),
    `desc` = VALUES(`desc`);
