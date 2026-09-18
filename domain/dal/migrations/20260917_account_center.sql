-- Additive account center upgrade. Never change the published 20260913 import.
ALTER TABLE user_account ADD COLUMN avatar_version BIGINT NOT NULL DEFAULT 0;

CREATE TABLE user_avatar (
    user_id BIGINT NOT NULL PRIMARY KEY,
    version BIGINT NOT NULL,
    content_type VARCHAR(32) NOT NULL,
    content_blob MEDIUMBLOB NOT NULL,
    byte_size INT UNSIGNED NOT NULL,
    update_time DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE login_token ADD COLUMN login_ip VARCHAR(45) NOT NULL DEFAULT '';
ALTER TABLE login_token ADD COLUMN user_agent VARCHAR(1024) NOT NULL DEFAULT '';
ALTER TABLE login_token ADD COLUMN client_name VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE login_token ADD COLUMN os_name VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE login_token ADD COLUMN device_type VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE login_token ADD COLUMN login_source VARCHAR(24) NOT NULL DEFAULT '';
-- Preserve idx_login_token_user for existing validity-range queries.
ALTER TABLE login_token ADD KEY idx_login_token_session_page (user_id,auth_version,is_expired,create_time,id);
ALTER TABLE login_token ADD KEY idx_login_token_metadata_expiry (expire_time,id);
-- Bound effective-session reads by account version and natural expiry.
ALTER TABLE login_token ADD KEY idx_login_token_effective_range (user_id,auth_version,is_expired,expire_time,create_time,id);
