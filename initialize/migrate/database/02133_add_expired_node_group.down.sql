-- 回滚 user_subscribe 表的过期流量字段
ALTER TABLE `user_subscribe`
DROP COLUMN `expired_upload`,
DROP COLUMN `expired_download`;

-- 回滚 node_group 表的过期节点组字段
ALTER TABLE `node_group`
DROP INDEX `idx_is_expired_group`,
DROP COLUMN `speed_limit`,
DROP COLUMN `max_traffic_gb_expired`,
DROP COLUMN `expired_days_limit`,
DROP COLUMN `is_expired_group`;
