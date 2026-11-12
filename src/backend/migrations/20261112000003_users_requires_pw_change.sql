-- +goose Up
ALTER TABLE users
    ADD COLUMN requires_password_change BOOLEAN NOT NULL DEFAULT FALSE AFTER password_hash;

-- +goose Down
ALTER TABLE users
    DROP COLUMN requires_password_change;
