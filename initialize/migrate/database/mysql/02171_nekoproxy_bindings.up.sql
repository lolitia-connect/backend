CREATE TABLE `nekoproxy_bindings` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `node_id` BIGINT NOT NULL,
    `api_url` VARCHAR(512) COLLATE utf8mb4_bin NOT NULL,
    `forward_id` VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    `session_id` VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `nekoproxy_bindings_node_id` (`node_id`),
    UNIQUE KEY `nekoproxy_bindings_api_url_forward_id` (`api_url`, `forward_id`),
    CONSTRAINT `nekoproxy_bindings_node_fk` FOREIGN KEY (`node_id`) REFERENCES `nodes` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
