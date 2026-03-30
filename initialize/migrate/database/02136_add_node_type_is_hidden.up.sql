ALTER TABLE `nodes`
    ADD COLUMN `node_type` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'landing' COMMENT 'Node type: front, landing' AFTER `enabled`,
    ADD COLUMN `is_hidden` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Hidden - users cannot see hidden nodes' AFTER `node_type`;

UPDATE `nodes`
SET `node_type` = 'landing'
WHERE `node_type` = '';