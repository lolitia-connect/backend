ALTER TABLE `task`
    ADD COLUMN `daily_date` VARCHAR(10) NOT NULL DEFAULT '' COMMENT 'Email daily quota date' AFTER `current`,
    ADD COLUMN `daily_sent` BIGINT UNSIGNED NOT NULL DEFAULT '0' COMMENT 'Email daily quota sent count' AFTER `daily_date`;