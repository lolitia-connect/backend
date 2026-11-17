DROP INDEX `order_guest_auth_type_guest_identifier_coupon_status` ON `order`;
ALTER TABLE `order` DROP COLUMN `guest_auth_type`, DROP COLUMN `guest_identifier`;