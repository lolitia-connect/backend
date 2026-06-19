ALTER TABLE `payment`
    ADD COLUMN `currency_unit` VARCHAR(10) NOT NULL DEFAULT '' COMMENT 'Payment Channel Currency Unit' AFTER `token`,
    ADD COLUMN `exchange_rate` DECIMAL(16, 8) NOT NULL DEFAULT 0 COMMENT 'Exchange Rate from System Currency to Channel Currency' AFTER `currency_unit`,
    ADD COLUMN `bill_desc` VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Bill Description Template, supports {order_no}, {item_name}, {amount}, {trade_no}' AFTER `exchange_rate`;
