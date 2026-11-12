# 資料擺設工具擴充指引（`testutil.Seeder`）

本套件提供給後續各功能工作項目（07 至 32）在主要接縫測試環境中迅速布置資料庫前置狀態，讓功能開發不需等待上游實作完成。

## 使用範例

```go
s := testutil.NewSeeder(t, db, testClock)
adminID := s.SeedAdmin("admin")
userID := s.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")
swID, vrfID := s.SeedSwitch("core-sw-01", "10.0.0.1", "ios-xe", "v3")
subnetID := s.SeedSubnet("10.10.10.0/24", "managed", nil)

ipID := s.SeedIPRecord("10.10.10.50", "b8:27:eb:11:22:33", subnetID, &testutil.IPRecordSeedOpts{
    Presence:     "online",
    Registration: "registered",
    OwnerUserID:  &userID,
    OwnerMail:    "chen@example.com",
})
s.SeedSighting(ipID, vrfID, swID, "b8:27:eb:11:22:33", "Vlan10", 0)
```

## 如何在自己的工作項目中擴充

**請勿直接修改共用的 `seed.go` 檔案**，以避免平行開發時發生 Git 合併衝突。
若您的功能需要專屬的前置資料建立函式，請直接在您自己功能的套件或測試檔中以輔助函式包裝 `s.DB` 與 `s.Clock` 即可（例如 `func seedAuditEntry(t *testing.T, s *testutil.Seeder, ...)`）。
