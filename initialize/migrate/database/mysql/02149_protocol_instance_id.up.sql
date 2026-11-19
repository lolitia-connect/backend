SET @column_exists = (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'nodes' AND COLUMN_NAME = 'protocol_id');
SET @sql = IF(@column_exists = 0, 'ALTER TABLE `nodes` ADD COLUMN `protocol_id` VARCHAR(100) NOT NULL DEFAULT '''' COMMENT ''Protocol Instance ID'' AFTER `protocol`', 'SELECT 1');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

UPDATE `nodes` SET `protocol_id` = '1' WHERE `protocol_id` = '';

UPDATE `servers` s
JOIN (
    SELECT id,
           -- 使用 CONCAT 和 GROUP_CONCAT 手动拼接标准 JSON 数组，彻底规避 MariaDB 的自动转义
           CONCAT('[', GROUP_CONCAT(
               CASE
                   WHEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(d.protocol, '$.id')), '') = ''
                       THEN JSON_SET(d.protocol, '$.id', CAST(d.protocol_index AS CHAR))
                   ELSE d.protocol
               END
               ORDER BY d.ord SEPARATOR ','
           ), ']') AS protocols
    FROM (
        SELECT s.id,
               jt.ord,
               JSON_EXTRACT(s.`protocols`, CONCAT('$[', jt.ord - 1, ']')) AS protocol,
               ROW_NUMBER() OVER (
                   PARTITION BY s.id, JSON_UNQUOTE(JSON_EXTRACT(s.`protocols`, CONCAT('$[', jt.ord - 1, '].type')))
                   ORDER BY jt.ord
               ) AS protocol_index
        FROM `servers` s
        JOIN JSON_TABLE(
            CASE
                WHEN JSON_VALID(s.`protocols`) AND LEFT(TRIM(s.`protocols`), 1) = '['
                    THEN s.`protocols`
                ELSE '[]'
            END,
            '$[*]' COLUMNS (
                ord FOR ORDINALITY
            )
        ) jt
    ) d
    GROUP BY d.id
) migrated ON migrated.id = s.id
SET s.`protocols` = migrated.protocols;