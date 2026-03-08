-- Purpose: Rollback node group management tables
-- Author: Tension
-- Date: 2025-02-23
-- Updated: 2025-03-06

-- ===== Remove system configuration entries =====
DELETE FROM `system` WHERE `category` = 'group' AND `key` IN ('enabled', 'mode', 'auto_create_group');

-- ===== Remove columns and indexes from subscribe table =====
ALTER TABLE `subscribe` DROP INDEX IF EXISTS `idx_node_group_id`;
ALTER TABLE `subscribe` DROP COLUMN IF EXISTS `node_group_id`;
ALTER TABLE `subscribe` DROP COLUMN IF EXISTS `node_group_ids`;

-- ===== Remove columns and indexes from user_subscribe table =====
ALTER TABLE `user_subscribe` DROP INDEX IF EXISTS `idx_node_group_id`;
ALTER TABLE `user_subscribe` DROP COLUMN IF EXISTS `node_group_id`;

-- ===== Remove columns and indexes from nodes table =====
ALTER TABLE `nodes` DROP COLUMN IF EXISTS `node_group_ids`;

-- ===== Drop group_history_detail table =====
DROP TABLE IF EXISTS `group_history_detail`;

-- ===== Drop group_history table =====
DROP TABLE IF EXISTS `group_history`;

-- ===== Drop node_group table =====
DROP TABLE IF EXISTS `node_group`;
