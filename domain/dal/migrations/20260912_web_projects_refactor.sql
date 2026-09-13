-- Upgrade databases created by the PR's earlier 20260909 migration. The
-- temporary integer columns make every text-to-enum mapping explicit; unknown
-- values abort the migration before old columns are dropped.
DROP PROCEDURE IF EXISTS migrate_web_projects_refactor;

DELIMITER //

CREATE PROCEDURE migrate_web_projects_refactor()
BEGIN
	DECLARE invalid_project_id BIGINT DEFAULT NULL;
	DECLARE invalid_release_id BIGINT DEFAULT NULL;

	SELECT id INTO invalid_project_id
	FROM web_project
	WHERE access_mode NOT IN ('owner', 'members', 'authenticated', 'public')
	   OR status NOT IN ('draft', 'enabled', 'disabled', 'deleted')
	LIMIT 1;
	IF invalid_project_id IS NOT NULL THEN
		SIGNAL SQLSTATE '45000'
			SET MESSAGE_TEXT = 'web_project contains an unknown access_mode or status';
	END IF;

	SELECT id INTO invalid_release_id
	FROM web_project_release
	WHERE status NOT IN ('uploading', 'ready', 'failed', 'deleting')
	LIMIT 1;
	IF invalid_release_id IS NOT NULL THEN
		SIGNAL SQLSTATE '45000'
			SET MESSAGE_TEXT = 'web_project_release contains an unknown status';
	END IF;

    ALTER TABLE web_project
        ADD COLUMN access_mode_int TINYINT UNSIGNED NULL AFTER slug,
        ADD COLUMN status_int TINYINT UNSIGNED NULL AFTER access_mode,
        ADD COLUMN client_request_id_v2 VARCHAR(256) NULL AFTER revision;

    UPDATE web_project
    SET access_mode_int = CASE access_mode
            WHEN 'owner' THEN 1
            WHEN 'members' THEN 2
            WHEN 'authenticated' THEN 3
            WHEN 'public' THEN 4
            ELSE NULL
        END,
        status_int = CASE status
            WHEN 'draft' THEN 1
            WHEN 'enabled' THEN 2
            WHEN 'disabled' THEN 3
            WHEN 'deleted' THEN 4
            ELSE NULL
        END,
        client_request_id_v2 = client_request_id;

    ALTER TABLE web_project DROP INDEX uk_web_project_owner_request;
    ALTER TABLE web_project DROP INDEX idx_web_project_status_deleted_id;
    ALTER TABLE web_project
        DROP COLUMN access_mode,
        DROP COLUMN status,
        DROP COLUMN client_request_id,
        CHANGE COLUMN access_mode_int access_mode TINYINT UNSIGNED NOT NULL COMMENT '1 owner, 2 members, 3 authenticated, 4 public',
        CHANGE COLUMN status_int status TINYINT UNSIGNED NOT NULL COMMENT '1 draft, 2 enabled, 3 disabled, 4 deleted',
        CHANGE COLUMN client_request_id_v2 client_request_id VARCHAR(256) NULL COMMENT 'optional create idempotency key',
        MODIFY COLUMN name VARCHAR(256) NOT NULL COMMENT 'display name, at most 256 Unicode characters',
        MODIFY COLUMN slug VARCHAR(256) CHARACTER SET ascii COLLATE ascii_bin NOT NULL COMMENT 'public URL segment',
        ADD UNIQUE KEY uk_web_project_owner_request (owner_user_id, client_request_id),
        ADD KEY idx_web_project_status_deleted_id (status, deleted_at, id);

    ALTER TABLE web_project_release
        ADD COLUMN status_int TINYINT UNSIGNED NULL AFTER storage_key,
        ADD COLUMN idempotency_key_v2 VARCHAR(256) NULL AFTER total_bytes,
        ADD COLUMN extra TEXT NULL AFTER idempotency_key;

    UPDATE web_project_release
    SET status_int = CASE status
            WHEN 'uploading' THEN 1
            WHEN 'ready' THEN 2
            WHEN 'failed' THEN 3
            WHEN 'deleting' THEN 4
            ELSE NULL
        END,
        idempotency_key_v2 = idempotency_key,
        extra = JSON_OBJECT('error_message', error_message);

    ALTER TABLE web_project_release DROP INDEX uk_web_project_release_idempotency;
    ALTER TABLE web_project_release DROP INDEX idx_web_project_release_status_update_id;
    ALTER TABLE web_project_release
        DROP COLUMN status,
        DROP COLUMN idempotency_key,
        DROP COLUMN error_message,
        CHANGE COLUMN status_int status TINYINT UNSIGNED NOT NULL COMMENT '1 uploading, 2 ready, 3 failed, 4 deleting',
        CHANGE COLUMN idempotency_key_v2 idempotency_key VARCHAR(256) NULL COMMENT 'optional upload idempotency key',
        MODIFY COLUMN entry_file VARCHAR(2048) NOT NULL COMMENT 'validated HTML path relative to release content root',
        MODIFY COLUMN extra TEXT NOT NULL COMMENT 'JSON object for extensible release metadata, including error_message',
        ADD UNIQUE KEY uk_web_project_release_idempotency (project_id, idempotency_key),
        ADD KEY idx_web_project_release_status_update_id (status, update_time, id);
END//

CALL migrate_web_projects_refactor()//
DROP PROCEDURE migrate_web_projects_refactor//

DELIMITER ;
