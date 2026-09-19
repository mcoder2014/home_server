CREATE TABLE resource_passwords
(
    resource_type VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'web_project or manual',
    resource_id   BIGINT NOT NULL COMMENT 'id in the resource-specific table',
    password_hash VARCHAR(100) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '' COMMENT 'bcrypt hash, empty retains a cleared version',
    version       BIGINT UNSIGNED NOT NULL COMMENT 'monotonic password policy version starting at one',
    update_time   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (resource_type, resource_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT 'independent reading passwords for hosted resources';
