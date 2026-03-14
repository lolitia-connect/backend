-- 为 node_group 表添加过期节点组相关字段
ALTER TABLE `node_group`
ADD COLUMN `is_expired_group` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Is Expired Group: 0=normal, 1=expired group' AFTER `for_calculation`,
ADD COLUMN `expired_days_limit` int NOT NULL DEFAULT 7 COMMENT 'Expired days limit (days)' AFTER `is_expired_group`,
ADD COLUMN `max_traffic_gb_expired` bigint DEFAULT 0 COMMENT 'Max traffic for expired users (GB)' AFTER `expired_days_limit`,
ADD COLUMN `speed_limit` int NOT NULL DEFAULT 0 COMMENT 'Speed limit (KB/s)' AFTER `max_traffic_gb_expired`;

-- 添加索引
ALTER TABLE `node_group` ADD INDEX `idx_is_expired_group` (`is_expired_group`);

-- 为 user_subscribe 表添加过期流量统计字段
ALTER TABLE `user_subscribe`
ADD COLUMN `expired_download` bigint NOT NULL DEFAULT 0 COMMENT 'Expired period download traffic (bytes)' AFTER `upload`,
ADD COLUMN `expired_upload` bigint NOT NULL DEFAULT 0 COMMENT 'Expired period upload traffic (bytes)' AFTER `expired_download`;
