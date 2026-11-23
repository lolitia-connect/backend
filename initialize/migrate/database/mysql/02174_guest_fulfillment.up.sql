ALTER TABLE `order`
    ADD COLUMN `guest_password_hash` VARCHAR(255) NULL,
    ADD COLUMN `guest_invite_code` TEXT NULL;
