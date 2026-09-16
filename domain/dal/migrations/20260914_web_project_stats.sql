-- Apply manually to the application's database before enabling analytics.
-- Additive and repeatable on MySQL/MariaDB. No automatic application startup DDL.
CREATE TABLE IF NOT EXISTS web_project_stat_total (
 project_id BIGINT NOT NULL,
 pv BIGINT UNSIGNED NOT NULL DEFAULT 0,
 uv BIGINT UNSIGNED NOT NULL DEFAULT 0,
 uv_hll BLOB NOT NULL,
 last_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
 tracking_started_at DATETIME(6) NOT NULL,
 persisted_at DATETIME(6) NOT NULL,
 quality VARCHAR(16) NOT NULL,
 quality_reason VARCHAR(64) NOT NULL DEFAULT '',
 timezone VARCHAR(64) NOT NULL,
 format_version SMALLINT UNSIGNED NOT NULL,
 PRIMARY KEY (project_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS web_project_stat_daily (
 project_id BIGINT NOT NULL,
 stat_date DATE NOT NULL,
 pv BIGINT UNSIGNED NOT NULL DEFAULT 0,
 uv BIGINT UNSIGNED NOT NULL DEFAULT 0,
 uv_hll BLOB NOT NULL,
 snapshot_seq BIGINT UNSIGNED NOT NULL DEFAULT 0,
 persisted_at DATETIME(6) NOT NULL,
 quality VARCHAR(16) NOT NULL,
 quality_reason VARCHAR(64) NOT NULL DEFAULT '',
 PRIMARY KEY (project_id, stat_date),
 KEY idx_stat_date_project (stat_date, project_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
