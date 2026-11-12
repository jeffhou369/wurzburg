-- +goose Up
CREATE TABLE IF NOT EXISTS schema_initial_check (
    id INT AUTO_INCREMENT PRIMARY KEY,
    initialized_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS schema_initial_check;
