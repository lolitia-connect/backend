-- Truncate first: the old width cannot hold what the up migration allowed.
UPDATE `user_device`
SET `user_agent` = SUBSTRING(`user_agent`, 1, 64)
WHERE CHAR_LENGTH(`user_agent`) > 64;

ALTER TABLE `user_device`
    MODIFY COLUMN `user_agent` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci DEFAULT NULL COMMENT 'Device User Agent.';
