package test

import (
	"context"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/database"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil"
	"github.com/jeffhou369/wurzburg/backend/migrations"
	"github.com/testcontainers/testcontainers-go"
	mariadbmod "github.com/testcontainers/testcontainers-go/modules/mariadb"
)

func TestDatabaseSchemaConstraintsAndSeeder(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的資料庫結構測試")
	}

	ctx := context.Background()
	mariadbContainer, err := mariadbmod.Run(ctx,
		"mariadb:11.4",
		mariadbmod.WithDatabase("wurzburg"),
		mariadbmod.WithUsername("wurzburg"),
		mariadbmod.WithPassword("wurzburg_test_pw"),
	)
	if err != nil {
		t.Fatalf("啟動 MariaDB 容器失敗: %v", err)
	}
	t.Cleanup(func() {
		_ = testcontainers.TerminateContainer(mariadbContainer)
	})

	dsn, err := mariadbContainer.ConnectionString(ctx, "parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("取得連線字串失敗: %v", err)
	}

	db, err := connectTestDB(t, ctx, dsn)
	if err != nil {
		t.Fatalf("連線 MariaDB 失敗: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := database.RunMigrations(db, migrations.FS, "."); err != nil {
		t.Fatalf("套用遷移失敗: %v", err)
	}

	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	clk := clock.NewTestClock(now)
	seeder := testutil.NewSeeder(t, db, clk)

	// 1. 驗證系統管理員與一般使用者建立
	_ = seeder.SeedAdmin("admin")
	userID := seeder.SeedUser("chen", "user", "ad", "chen@example.com", "陳工程師")

	// 2. 驗證交換器、VRF 讀取設定與讀取紀錄
	swID, vrfDefaultID := seeder.SeedSwitch("core-sw-01", "10.0.0.1", "ios-xe", "v3")
	vrfCorpID := seeder.SeedVRFConfig(swID, "CORP", "CORP")
	seeder.SeedPollRun(vrfDefaultID, swID, true, 120, "")
	seeder.SeedPollRun(vrfCorpID, swID, false, 0, "timeout")

	// 3. 驗證三種狀態網段、排除範圍與閘道位址
	_ = seeder.SeedSubnet("10.10.99.0/24", "pending", nil)
	managedSubnetID := seeder.SeedSubnet("10.10.10.0/24", "managed", nil)
	dhcpReason := "dhcp"
	_ = seeder.SeedSubnet("10.10.20.0/24", "unmanaged", &dhcpReason)
	seeder.SeedExclusionRange(managedSubnetID, "10.10.10.200", "10.10.10.250", "dhcp")
	seeder.SeedGateway(managedSubnetID, "10.10.10.2", "Vlan10", swID)

	// 驗證資料庫限制：unmanaged 網段缺少 exclude_reason 必須被拒絕
	if _, err := db.Exec(`INSERT INTO subnets (cidr, state) VALUES ('10.99.0.0/24', 'unmanaged')`); err == nil {
		t.Fatal("預期未指定原因之不納管網段應遭 CHECK 限制拒絕，但寫入成功")
	}

	// 4. 驗證各種組合的 IP 紀錄
	ipUnregOnline := seeder.SeedIPRecord("10.10.10.50", "b8:27:eb:11:22:33", managedSubnetID, nil)
	expires := now.Add(300 * 24 * time.Hour)
	ipRegOffline := seeder.SeedIPRecord("10.10.10.60", "00:50:56:11:22:33", managedSubnetID, &testutil.IPRecordSeedOpts{
		Presence:     "offline",
		Registration: "registered",
		OwnerUserID:  &userID,
		OwnerMail:    "chen@example.com",
		ExpiresAt:    &expires,
	})
	expiredAt := now.Add(-10 * 24 * time.Hour)
	_ = seeder.SeedIPRecord("10.10.10.70", "d4:3d:7e:44:55:66", managedSubnetID, &testutil.IPRecordSeedOpts{
		Presence:     "online",
		Registration: "expired",
		OwnerUserID:  &userID,
		OwnerMail:    "chen@example.com",
		ExpiresAt:    &expiredAt,
	})

	// 驗證資料庫限制：重複 IPv4 應遭拒絕
	if _, err := db.Exec(
		`INSERT INTO ip_records (ip_address, subnet_id) VALUES ('10.10.10.50', ?)`,
		managedSubnetID,
	); err == nil {
		t.Fatal("預期重複的 ip_address 應遭 UNIQUE 限制拒絕，但寫入成功")
	}

	// 5. 驗證 ARP 觀測、變更事件、衝突與通知去重狀態
	seeder.SeedSighting(ipUnregOnline, vrfDefaultID, swID, "b8:27:eb:11:22:33", "Vlan10", 0)
	seeder.SeedIPEvent(ipUnregOnline, "10.10.10.50", "first_seen", "首次於 Vlan10 見到")
	seeder.SeedNotificationState("unreg|10.10.10.50|b8:27:eb:11:22:33", "unregistered_digest", true)

	// 驗證衝突唯一限制：同一個 IP 可有已解消的歷史衝突，但只能有一筆未解消衝突
	seeder.SeedConflict(ipRegOffline, "10.10.10.60", "00:50:56:11:22:33", []string{"00:50:56:11:22:33", "aa:bb:cc:11:22:33"}, "resolved")
	seeder.SeedConflict(ipRegOffline, "10.10.10.60", "00:50:56:11:22:33", []string{"00:50:56:11:22:33", "dd:ee:ff:11:22:33"}, "open")

	if _, err := db.Exec(
		`INSERT INTO ip_conflicts (ip_record_id, ip_address, adopted_mac, conflict_macs, status) VALUES (?, '10.10.10.60', '00:50:56:11:22:33', '["aa:bb:cc:00:00:01"]', 'in_progress')`,
		ipRegOffline,
	); err == nil {
		t.Fatal("預期同一筆 IP 紀錄不能同時擁有兩筆未解消衝突，但寫入成功")
	}
}
