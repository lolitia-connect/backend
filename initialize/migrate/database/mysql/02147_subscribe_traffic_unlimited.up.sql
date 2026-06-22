ALTER TABLE `subscribe`
    ADD COLUMN `traffic_unlimited` TINYINT(1) NOT NULL DEFAULT 0 COMMENT 'Traffic Unlimited' AFTER `traffic`;
