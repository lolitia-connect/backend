ALTER TABLE `node_group`
    ADD COLUMN `group_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'common' COMMENT 'Node group type: common, subscribe, app' AFTER `name`;

UPDATE `node_group`
SET `group_type` = 'common'
WHERE `group_type` = '';
