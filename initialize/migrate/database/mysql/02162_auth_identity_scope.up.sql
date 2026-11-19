ALTER TABLE `user_auth_methods`
    DROP INDEX `idx_auth_identifier`,
    ADD UNIQUE KEY `idx_auth_type_identifier` (`auth_type`, `auth_identifier`);
UPDATE `user_auth_methods` SET `verified` = 1
WHERE `auth_type` NOT IN ('email', 'mobile', 'device') AND `verified` = 0;