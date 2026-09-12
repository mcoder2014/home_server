CREATE TABLE application
(
    id              BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT 'application id',
    owner_user_id   BIGINT       NOT NULL COMMENT 'credential owner user id',
    name            VARCHAR(128) NOT NULL COMMENT 'display name',
    description     TEXT         NOT NULL COMMENT 'plain text description',
    access_key      CHAR(28) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'public application access key',
    secret_digest   BINARY(32)   NOT NULL COMMENT 'SHA-256 digest of the secret key',
    scopes          TEXT         NOT NULL COMMENT 'canonical JSON array of granted scopes',
    status          TINYINT      NOT NULL COMMENT '1 enabled, 2 disabled, 3 revoked',
    active_slot     TINYINT      NULL COMMENT 'bounded non-revoked credential slot per owner',
    revision        BIGINT       NOT NULL DEFAULT 1 COMMENT 'optimistic lock and authorization revision',
    secret_version  BIGINT       NOT NULL DEFAULT 1 COMMENT 'secret rotation version',
    expires_at      DATETIME(6)  NOT NULL COMMENT 'credential expiry',
    last_issued_at  DATETIME(6)  NULL COMMENT 'last successful token issue time',
    create_time     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    update_time     DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uk_application_access_key (access_key),
    UNIQUE KEY uk_application_owner_slot (owner_user_id, active_slot),
    KEY idx_application_owner_id (owner_user_id, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT 'user-owned application credentials';

CREATE TABLE application_access_token
(
    id                   BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY COMMENT 'access token record id',
    application_id       BIGINT      NOT NULL COMMENT 'issuing application id',
    token_digest         BINARY(32)  NOT NULL COMMENT 'SHA-256 digest of opaque bearer token',
    secret_version       BIGINT      NOT NULL COMMENT 'application secret version at issue time',
    application_revision BIGINT      NOT NULL COMMENT 'application authorization revision at issue time',
    scope_snapshot       TEXT        NOT NULL COMMENT 'canonical JSON array of issued scopes',
    expired_at           DATETIME(6) NOT NULL COMMENT 'token expiry',
    create_time          DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uk_application_token_digest (token_digest),
    KEY idx_application_token_expiry_id (expired_at, id),
    KEY idx_application_token_app_id (application_id, id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COMMENT 'opaque short-lived application access tokens';
