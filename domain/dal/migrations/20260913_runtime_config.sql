CREATE TABLE site_config_current (
    namespace VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    schema_version INT UNSIGNED NOT NULL,
    values_json LONGTEXT NOT NULL CHECK (JSON_VALID(values_json)),
    values_sha256 BINARY(32) NOT NULL,
    updated_by BIGINT NOT NULL,
    update_time DATETIME(6) NOT NULL,
    PRIMARY KEY (namespace)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE site_config_history (
    namespace VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    schema_version INT UNSIGNED NOT NULL,
    values_json LONGTEXT NOT NULL CHECK (JSON_VALID(values_json)),
    values_sha256 BINARY(32) NOT NULL,
    request_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_hash BINARY(32) NOT NULL,
    actor_user_id BIGINT NOT NULL,
    reason VARCHAR(512) NOT NULL,
    rollback_from_revision BIGINT UNSIGNED NULL,
    create_time DATETIME(6) NOT NULL,
    PRIMARY KEY (namespace, revision),
    UNIQUE KEY uk_config_request (namespace, request_id),
    KEY idx_config_actor_time (actor_user_id, create_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE site_runtime_state (
    id TINYINT UNSIGNED NOT NULL,
    config_generation BIGINT UNSIGNED NOT NULL,
    registration_epoch BIGINT UNSIGNED NOT NULL,
    revision BIGINT UNSIGNED NOT NULL,
    update_time DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT ck_runtime_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
