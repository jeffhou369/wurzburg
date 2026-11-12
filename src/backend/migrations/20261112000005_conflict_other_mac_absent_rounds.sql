-- +goose Up
-- 處理中（in_progress）衝突：記錄「其他 MAC」連續未被成功回報的輪數，
-- 達離線判定輪數才自動解消（開發規格 3.6），不因來源改回報採信 MAC 而在第 1 輪就解消。
ALTER TABLE ip_conflicts
    ADD COLUMN other_mac_absent_rounds INT NOT NULL DEFAULT 0 AFTER last_detected_at;

-- +goose Down
ALTER TABLE ip_conflicts
    DROP COLUMN other_mac_absent_rounds;
