-- +goose Up
ALTER TABLE switches
    ADD COLUMN v3_username VARCHAR(100) NOT NULL DEFAULT '' AFTER snmp_version,
    ADD COLUMN v3_auth_protocol VARCHAR(20) NOT NULL DEFAULT 'SHA256' AFTER v3_username,
    ADD COLUMN v3_auth_pass_encrypted TEXT AFTER v3_auth_protocol,
    ADD COLUMN v3_priv_protocol VARCHAR(20) NOT NULL DEFAULT 'AES' AFTER v3_auth_pass_encrypted,
    ADD COLUMN v3_priv_pass_encrypted TEXT AFTER v3_priv_protocol;

-- +goose Down
ALTER TABLE switches
    DROP COLUMN v3_priv_pass_encrypted,
    DROP COLUMN v3_priv_protocol,
    DROP COLUMN v3_auth_pass_encrypted,
    DROP COLUMN v3_auth_protocol,
    DROP COLUMN v3_username;
