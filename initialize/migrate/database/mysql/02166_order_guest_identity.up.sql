ALTER TABLE `order`
    ADD COLUMN `guest_auth_type` VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN `guest_identifier` VARCHAR(255) NOT NULL DEFAULT '';
CREATE INDEX `order_guest_auth_type_guest_identifier_coupon_status` ON `order` (`guest_auth_type`, `guest_identifier`, `coupon`, `status`);