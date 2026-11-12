-- +goose Up

-- 1. 交換器 (switches)
CREATE TABLE IF NOT EXISTS switches (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    mgmt_ip VARCHAR(45) NOT NULL,
    platform ENUM('ios-xe', 'nx-os') NOT NULL DEFAULT 'ios-xe',
    snmp_version ENUM('v2c', 'v3') NOT NULL DEFAULT 'v3',
    snmp_port INT NOT NULL DEFAULT 161,
    timeout_ms INT NOT NULL DEFAULT 3000,
    retries INT NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    role_label VARCHAR(50) NOT NULL DEFAULT '核心',
    notes TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_switches_mgmt_ip (mgmt_ip)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 2. VRF 讀取設定 (vrf_read_configs)
CREATE TABLE IF NOT EXISTS vrf_read_configs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    switch_id BIGINT NOT NULL,
    vrf_name VARCHAR(100) NOT NULL DEFAULT 'default',
    snmp_context VARCHAR(100) NOT NULL DEFAULT '',
    snmp_community_encrypted TEXT,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_poll_at TIMESTAMP NULL DEFAULT NULL,
    last_success_at TIMESTAMP NULL DEFAULT NULL,
    last_status ENUM('success', 'failed', 'running', 'pending') NOT NULL DEFAULT 'pending',
    last_error_class VARCHAR(50) NOT NULL DEFAULT '',
    last_error_summary TEXT,
    last_learned_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_vrf_switch FOREIGN KEY (switch_id) REFERENCES switches (id) ON DELETE CASCADE,
    UNIQUE KEY uq_switch_vrf (switch_id, vrf_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 3. 輪詢讀取紀錄 (poll_runs)
CREATE TABLE IF NOT EXISTS poll_runs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    vrf_config_id BIGINT NOT NULL,
    switch_id BIGINT NOT NULL,
    started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP NULL DEFAULT NULL,
    is_success BOOLEAN NOT NULL DEFAULT FALSE,
    retried BOOLEAN NOT NULL DEFAULT FALSE,
    learned_count INT NOT NULL DEFAULT 0,
    error_class VARCHAR(50) NOT NULL DEFAULT '',
    error_summary TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_poll_runs_vrf FOREIGN KEY (vrf_config_id) REFERENCES vrf_read_configs (id) ON DELETE CASCADE,
    INDEX idx_poll_runs_started (started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 4. 網段 (subnets)
CREATE TABLE IF NOT EXISTS subnets (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cidr VARCHAR(50) NOT NULL,
    state ENUM('pending', 'managed', 'unmanaged') NOT NULL DEFAULT 'pending',
    exclude_reason ENUM('dhcp', 'wireless', 'other') NULL DEFAULT NULL,
    exclude_comment TEXT,
    is_discovered BOOLEAN NOT NULL DEFAULT TRUE,
    source_missing BOOLEAN NOT NULL DEFAULT FALSE,
    description VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT chk_subnets_exclude_reason CHECK (state != 'unmanaged' OR exclude_reason IS NOT NULL),
    UNIQUE KEY uq_subnets_cidr (cidr),
    INDEX idx_subnets_state (state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 5. 網段閘道位址 (subnet_gateways)
CREATE TABLE IF NOT EXISTS subnet_gateways (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    subnet_id BIGINT NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    if_index BIGINT NOT NULL DEFAULT 0,
    if_name VARCHAR(100) NOT NULL DEFAULT '',
    switch_id BIGINT NOT NULL,
    vrf_name VARCHAR(100) NOT NULL DEFAULT 'default',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_gateway_subnet FOREIGN KEY (subnet_id) REFERENCES subnets (id) ON DELETE CASCADE,
    UNIQUE KEY uq_subnet_gateway_ip (subnet_id, ip_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 6. 排除範圍 (exclusion_ranges)
CREATE TABLE IF NOT EXISTS exclusion_ranges (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    subnet_id BIGINT NOT NULL,
    start_ip VARCHAR(45) NOT NULL,
    end_ip VARCHAR(45) NOT NULL,
    reason ENUM('dhcp', 'wireless', 'other') NOT NULL DEFAULT 'dhcp',
    comment TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_exclusion_subnet FOREIGN KEY (subnet_id) REFERENCES subnets (id) ON DELETE CASCADE,
    INDEX idx_exclusion_subnet (subnet_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 7. 使用者帳號 (users)
CREATE TABLE IF NOT EXISTS users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(100) NOT NULL,
    display_name VARCHAR(100) NOT NULL DEFAULT '',
    mail VARCHAR(255) NOT NULL DEFAULT '',
    role ENUM('admin', 'user') NOT NULL DEFAULT 'user',
    auth_source ENUM('local', 'ad', 'radius') NOT NULL DEFAULT 'local',
    password_hash VARCHAR(255) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    failed_login_count INT NOT NULL DEFAULT 0,
    locked_until TIMESTAMP NULL DEFAULT NULL,
    last_login_at TIMESTAMP NULL DEFAULT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_users_source_username (auth_source, username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 8. IP 紀錄 (ip_records) - IPv4 全域唯一
CREATE TABLE IF NOT EXISTS ip_records (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    ip_address VARCHAR(45) NOT NULL,
    mac_address VARCHAR(20) NOT NULL DEFAULT '',
    subnet_id BIGINT NOT NULL,
    presence ENUM('online', 'offline') NOT NULL DEFAULT 'online',
    registration ENUM('unregistered', 'registered', 'expired') NOT NULL DEFAULT 'unregistered',
    expires_at TIMESTAMP NULL DEFAULT NULL,
    confirmed_at TIMESTAMP NULL DEFAULT NULL,
    registered_at TIMESTAMP NULL DEFAULT NULL,
    first_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    owner_user_id BIGINT NULL DEFAULT NULL,
    owner_mail VARCHAR(255) NOT NULL DEFAULT '',
    device_type VARCHAR(100) NOT NULL DEFAULT '',
    hostname VARCHAR(255) NOT NULL DEFAULT '',
    notes TEXT,
    vendor_name VARCHAR(150) NOT NULL DEFAULT '未知',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_ip_subnet FOREIGN KEY (subnet_id) REFERENCES subnets (id) ON DELETE RESTRICT,
    CONSTRAINT fk_ip_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE SET NULL,
    UNIQUE KEY uq_ip_records_ip (ip_address),
    INDEX idx_ip_presence (presence),
    INDEX idx_ip_registration (registration),
    INDEX idx_ip_mac (mac_address),
    INDEX idx_ip_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 9. ARP 觀測 (arp_sightings) - 每個 IP × VRF 讀取設定目前的一筆
CREATE TABLE IF NOT EXISTS arp_sightings (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    ip_record_id BIGINT NOT NULL,
    vrf_config_id BIGINT NOT NULL,
    switch_id BIGINT NOT NULL,
    vrf_name VARCHAR(100) NOT NULL DEFAULT 'default',
    mac_address VARCHAR(20) NOT NULL,
    if_index BIGINT NOT NULL DEFAULT 0,
    if_name VARCHAR(100) NOT NULL DEFAULT '',
    arp_type ENUM('other', 'invalid', 'dynamic', 'static') NOT NULL DEFAULT 'dynamic',
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    missed_rounds INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_sighting_ip FOREIGN KEY (ip_record_id) REFERENCES ip_records (id) ON DELETE CASCADE,
    CONSTRAINT fk_sighting_vrf FOREIGN KEY (vrf_config_id) REFERENCES vrf_read_configs (id) ON DELETE CASCADE,
    UNIQUE KEY uq_sighting_ip_vrf (ip_record_id, vrf_config_id),
    INDEX idx_sighting_mac (mac_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 10. IP 變更事件 (ip_events)
CREATE TABLE IF NOT EXISTS ip_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    ip_record_id BIGINT NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    event_type ENUM('first_seen', 'online', 'offline', 'mac_changed') NOT NULL,
    details TEXT,
    occurred_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_events_ip FOREIGN KEY (ip_record_id) REFERENCES ip_records (id) ON DELETE CASCADE,
    INDEX idx_events_ip_time (ip_record_id, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 11. MAC 衝突 (ip_conflicts)
CREATE TABLE IF NOT EXISTS ip_conflicts (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    ip_record_id BIGINT NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    adopted_mac VARCHAR(20) NOT NULL,
    conflict_macs JSON NOT NULL,
    status ENUM('open', 'in_progress', 'resolved') NOT NULL DEFAULT 'open',
    resolution ENUM('adopt_new_keep_reg', 'adopt_new_release_reg', 'keep_adopted', 'auto_vanished') NULL DEFAULT NULL,
    note TEXT,
    resolved_by VARCHAR(100) NOT NULL DEFAULT '',
    resolved_at TIMESTAMP NULL DEFAULT NULL,
    first_detected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_detected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    active_ip_record_id BIGINT AS (IF(status IN ('open', 'in_progress'), ip_record_id, NULL)) PERSISTENT,
    CONSTRAINT fk_conflict_ip FOREIGN KEY (ip_record_id) REFERENCES ip_records (id) ON DELETE CASCADE,
    UNIQUE KEY uq_active_conflict_per_ip (active_ip_record_id),
    INDEX idx_conflicts_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 11b. 未定前綴清單 (undetermined_prefixes)
CREATE TABLE IF NOT EXISTS undetermined_prefixes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    ip_address VARCHAR(45) NOT NULL,
    mac_address VARCHAR(20) NOT NULL,
    switch_id BIGINT NOT NULL,
    vrf_name VARCHAR(100) NOT NULL DEFAULT 'default',
    if_name VARCHAR(100) NOT NULL DEFAULT '',
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_undet_ip (ip_address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 12. 伺服端工作階段 (sessions)
CREATE TABLE IF NOT EXISTS sessions (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    idle_timeout_at TIMESTAMP NOT NULL,
    ip_address VARCHAR(45) NOT NULL DEFAULT '',
    user_agent VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_session_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    UNIQUE KEY uq_sessions_token (token_hash),
    INDEX idx_sessions_idle (idle_timeout_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 13. OUI 廠商條目 (oui_entries)
CREATE TABLE IF NOT EXISTS oui_entries (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    prefix VARCHAR(20) NOT NULL,
    vendor_name VARCHAR(200) NOT NULL,
    prefix_length INT NOT NULL DEFAULT 24,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_oui_prefix (prefix, prefix_length),
    INDEX idx_oui_lookup (prefix)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 14. OUI 匯入紀錄 (oui_imports)
CREATE TABLE IF NOT EXISTS oui_imports (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    version VARCHAR(50) NOT NULL,
    filename VARCHAR(255) NOT NULL,
    imported_by VARCHAR(100) NOT NULL,
    entry_count INT NOT NULL DEFAULT 0,
    imported_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 15. 通知紀錄 (notification_logs)
CREATE TABLE IF NOT EXISTS notification_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    notif_type ENUM('unregistered_digest', 'unregistered_weekly', 'expiry_reminder', 'test_mail') NOT NULL,
    dedup_key VARCHAR(100) NOT NULL DEFAULT '',
    recipient VARCHAR(255) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    status ENUM('pending', 'sent', 'failed') NOT NULL DEFAULT 'pending',
    attempt_count INT NOT NULL DEFAULT 1,
    last_error TEXT,
    payload_json JSON,
    sent_at TIMESTAMP NULL DEFAULT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_notif_created (created_at),
    INDEX idx_notif_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 16. 通知去重狀態 (notification_states)
CREATE TABLE IF NOT EXISTS notification_states (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    dedup_key VARCHAR(100) NOT NULL,
    notif_type ENUM('unregistered_digest', 'expiry_reminder') NOT NULL,
    is_notified BOOLEAN NOT NULL DEFAULT TRUE,
    notified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_notif_states_key (dedup_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 17. 稽核紀錄 (audit_logs)
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NULL DEFAULT NULL,
    username VARCHAR(100) NOT NULL,
    user_role VARCHAR(50) NOT NULL DEFAULT '',
    ip_address VARCHAR(45) NOT NULL DEFAULT '',
    action VARCHAR(100) NOT NULL,
    target_type VARCHAR(50) NOT NULL DEFAULT '',
    target_id VARCHAR(100) NOT NULL DEFAULT '',
    summary TEXT NOT NULL,
    details_json JSON,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_audit_created (created_at),
    INDEX idx_audit_user (username),
    INDEX idx_audit_action (action)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 18. 系統設定 (settings)
CREATE TABLE IF NOT EXISTS settings (
    setting_key VARCHAR(100) PRIMARY KEY,
    setting_value TEXT NOT NULL,
    is_secret BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS notification_states;
DROP TABLE IF EXISTS notification_logs;
DROP TABLE IF EXISTS oui_imports;
DROP TABLE IF EXISTS oui_entries;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS undetermined_prefixes;
DROP TABLE IF EXISTS ip_conflicts;
DROP TABLE IF EXISTS ip_events;
DROP TABLE IF EXISTS arp_sightings;
DROP TABLE IF EXISTS ip_records;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS exclusion_ranges;
DROP TABLE IF EXISTS subnet_gateways;
DROP TABLE IF EXISTS subnets;
DROP TABLE IF EXISTS poll_runs;
DROP TABLE IF EXISTS vrf_read_configs;
DROP TABLE IF EXISTS switches;
