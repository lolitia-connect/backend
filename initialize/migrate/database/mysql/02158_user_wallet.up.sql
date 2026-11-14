-- Billing-owned wallet table (ported from upstream v1.20.3, 02143_user_wallet):
-- the money columns move off the identity-owned user row. Upstream keys the
-- table on user_id directly; this fork keeps the local surrogate `id` primary
-- key convention and enforces one-wallet-per-user with a unique index instead.
CREATE TABLE IF NOT EXISTS `user_wallet` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `user_id` BIGINT NOT NULL COMMENT 'User Id (identity reference, no FK across domains)',
    `balance` BIGINT NOT NULL DEFAULT 0 COMMENT 'User Balance Amount',
    `gift_amount` BIGINT NOT NULL DEFAULT 0 COMMENT 'User Gift Amount',
    `commission` BIGINT NOT NULL DEFAULT 0 COMMENT 'Commission Amount',
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uni_user_wallet_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

SET @index_exists = (SELECT COUNT(1) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_wallet' AND INDEX_NAME = 'uni_user_wallet_user_id');
SET @sql = IF(@index_exists = 0, 'ALTER TABLE `user_wallet` ADD UNIQUE KEY `uni_user_wallet_user_id` (`user_id`)', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- Backfill one wallet per existing user. Users that already have a wallet row
-- are left untouched so the statement can be replayed safely.
INSERT INTO `user_wallet` (`user_id`, `balance`, `gift_amount`, `commission`, `created_at`, `updated_at`)
SELECT u.`id`, u.`balance`, u.`gift_amount`, u.`commission`, NOW(3), NOW(3)
FROM `user` u
LEFT JOIN `user_wallet` w ON w.`user_id` = u.`id`
WHERE w.`user_id` IS NULL;
