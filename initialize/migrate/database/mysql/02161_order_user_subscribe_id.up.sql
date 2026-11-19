ALTER TABLE `order`
    ADD COLUMN `user_subscribe_id` BIGINT NOT NULL DEFAULT 0,
    ADD INDEX `idx_order_user_subscribe_id` (`user_subscribe_id`);