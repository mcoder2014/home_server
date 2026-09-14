CREATE TABLE user_account (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(64) NOT NULL,
    username_key VARCHAR(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    display_name VARCHAR(64) NOT NULL DEFAULT '',
    contact_email VARCHAR(254) NOT NULL DEFAULT '',
    contact_mobile VARCHAR(32) NOT NULL DEFAULT '',
    password_hash VARCHAR(255) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    role VARCHAR(16) NOT NULL DEFAULT 'user',
    library_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    webdav_permission VARCHAR(16) NOT NULL DEFAULT 'none',
    auth_version BIGINT NOT NULL DEFAULT 1,
    revision BIGINT NOT NULL DEFAULT 1,
    must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
    password_expires_at DATETIME(6) NULL,
    invite_eligible_at DATETIME(6) NOT NULL,
    source VARCHAR(16) NOT NULL,
    invited_by_user_id BIGINT NULL,
    created_by_user_id BIGINT NULL,
    imported_at DATETIME(6) NULL,
    last_login_at DATETIME(6) NULL,
    deleted_at DATETIME(6) NULL,
    create_time DATETIME(6) NOT NULL,
    update_time DATETIME(6) NOT NULL,
    UNIQUE KEY uk_account_username (username_key),
    KEY idx_account_status_id (status,id),
    KEY idx_account_role_status_id (role,status,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE user_login_alias (
    login_key VARCHAR(254) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    kind VARCHAR(16) NOT NULL,
    KEY idx_login_alias_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE user_invitation (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    inviter_user_id BIGINT NOT NULL,
    quota_month CHAR(7) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    slot TINYINT UNSIGNED NOT NULL,
    token_digest BINARY(32) NOT NULL,
    token_hint VARCHAR(16) NOT NULL,
    registration_epoch BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'unused',
    expires_at DATETIME(6) NOT NULL,
    used_by_user_id BIGINT NULL,
    used_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    revoke_reason VARCHAR(512) NOT NULL DEFAULT '',
    note VARCHAR(128) NOT NULL DEFAULT '',
    request_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    create_time DATETIME(6) NOT NULL,
    UNIQUE KEY uk_invitation_month_slot (inviter_user_id,quota_month,slot),
    UNIQUE KEY uk_invitation_digest (token_digest),
    UNIQUE KEY uk_invitation_request (inviter_user_id,request_id),
    KEY idx_invitation_owner_id (inviter_user_id,id),
    KEY idx_invitation_expiry (status,expires_at,id),
    CONSTRAINT ck_invitation_slot CHECK (slot BETWEEN 1 AND 3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE admin_audit_log (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    actor_user_id BIGINT NOT NULL,
    action VARCHAR(64) NOT NULL,
    target_type VARCHAR(32) NOT NULL,
    target_id BIGINT NOT NULL,
    before_summary TEXT NOT NULL,
    after_summary TEXT NOT NULL,
    reason VARCHAR(512) NOT NULL,
    result VARCHAR(16) NOT NULL DEFAULT 'success',
    request_id VARCHAR(64) NOT NULL DEFAULT '',
    create_time DATETIME(6) NOT NULL,
    KEY idx_admin_audit_target (target_type,target_id,id),
    KEY idx_admin_audit_actor (actor_user_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE login_token ADD COLUMN auth_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE login_token ADD COLUMN purpose VARCHAR(32) NOT NULL DEFAULT 'user';
ALTER TABLE login_token ADD COLUMN authenticated_at DATETIME(6) NULL;
ALTER TABLE login_token ADD COLUMN token_digest BINARY(32) NULL;
ALTER TABLE login_token ADD PRIMARY KEY (id);
ALTER TABLE login_token ADD UNIQUE KEY uk_login_token_digest (token_digest);
ALTER TABLE login_token ADD KEY idx_login_token_user (user_id,is_expired,expire_time);

ALTER TABLE web_project ADD COLUMN moderation_status VARCHAR(16) NOT NULL DEFAULT 'normal';
ALTER TABLE web_project ADD COLUMN moderation_reason VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE web_project ADD COLUMN moderated_by BIGINT NULL;
ALTER TABLE web_project ADD COLUMN moderated_at DATETIME(6) NULL;
ALTER TABLE web_project ADD COLUMN purge_after DATETIME(6) NULL;
ALTER TABLE web_project ADD KEY idx_web_project_moderation (moderation_status,id);
ALTER TABLE web_project ADD KEY idx_web_project_purge (status,purge_after,id);

ALTER TABLE web_project_release ADD KEY idx_web_release_owner_status_id (uploaded_by,status,id);
