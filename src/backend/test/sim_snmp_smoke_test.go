package test

import (
	"context"
	"testing"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/collector"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// TestSNMPSimulatorWithRealCollector 確認真正的採集模組能經由 UDP 讀到 SNMP 模擬器的資料：
// SNMPv2c、community@context、SNMPv3（驗證加加密）與 context，以及資料在執行中更換與移除。
// 端到端情境建立在這些行為之上，先在這裡單獨確認，失敗時比較容易判斷是模擬器還是產品的問題。
func TestSNMPSimulatorWithRealCollector(t *testing.T) {
	if testing.Short() {
		t.Skip("略過需啟動 Docker 容器的整合測試")
	}
	ctx := context.Background()
	base := buildSNMPRec(
		[]simInterface{{Index: 10, Name: "Vlan10", IP: "10.10.10.254", Mask: "255.255.255.0"}},
		[]simARP{
			{IP: "10.10.10.11", MAC: "00:11:22:33:44:11", IfIndex: 10},
			{IP: "10.10.10.12", MAC: "00:11:22:33:44:12", IfIndex: 10},
		},
	)
	sim := startSNMPSim(t, map[string][]byte{
		"public":                base,
		"public@CORP":           walkFileToSNMPRec(t, "ios-xe/vrf-corp.walk"),
		"CORP":                  walkFileToSNMPRec(t, "ios-xe/vrf-corp.walk"),
		snmpSimV3DefaultContext: walkFileToSNMPRec(t, "nx-os/default-vrf.walk"),
	})
	col := collector.NewSNMPCollector(clock.NewTestClock(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)))
	v2c := interfaces.VRFReadInput{
		MgmtIP: sim.host, SNMPPort: sim.port, TimeoutMs: 3000, Platform: "ios-xe",
		SNMPVersion: "v2c", SNMPCommunity: "public", VRFName: "default",
	}

	out, err := col.Collect(ctx, v2c)
	if err != nil {
		t.Fatalf("SNMPv2c 讀取失敗: %v", err)
	}
	if len(out.ARPObservations) != 2 || len(out.InterfaceAddresses) != 1 || out.InterfaceNames[10] != "Vlan10" {
		t.Fatalf("SNMPv2c 讀到的資料不符：ARP=%d 介面位址=%d 介面名稱=%v", len(out.ARPObservations), len(out.InterfaceAddresses), out.InterfaceNames)
	}
	if out.InterfaceAddresses[0].NetMask != "255.255.255.0" || out.InterfaceAddresses[0].IfName != "Vlan10" {
		t.Errorf("介面位址內容不符：%+v", out.InterfaceAddresses[0])
	}

	t.Run("資料在執行中更換後讀到新內容", func(t *testing.T) {
		sim.setData(t, "public", buildSNMPRec(
			[]simInterface{{Index: 10, Name: "Vlan10", IP: "10.10.10.254", Mask: "255.255.255.0"}},
			[]simARP{{IP: "10.10.10.11", MAC: "00:11:22:33:44:99", IfIndex: 10}},
		))
		out, err := col.Collect(ctx, v2c)
		if err != nil {
			t.Fatalf("讀取失敗: %v", err)
		}
		if len(out.ARPObservations) != 1 || out.ARPObservations[0].MACAddress != "00:11:22:33:44:99" {
			t.Fatalf("應讀到更換後的資料，實際：%+v", out.ARPObservations)
		}
	})

	t.Run("community@context 對應非預設 VRF", func(t *testing.T) {
		in := v2c
		in.VRFName, in.SNMPContext = "CORP", "CORP"
		out, err := col.Collect(ctx, in)
		if err != nil {
			t.Fatalf("讀取失敗: %v", err)
		}
		if len(out.ARPObservations) == 0 {
			t.Fatal("應讀到 VRF CORP 的 ARP 資料")
		}
	})

	t.Run("SNMPv3 驗證加加密與 context", func(t *testing.T) {
		in := interfaces.VRFReadInput{
			MgmtIP: sim.host, SNMPPort: sim.port, TimeoutMs: 3000, Platform: "ios-xe",
			SNMPVersion: "v3", VRFName: "CORP", SNMPContext: "CORP",
			V3Username: simV3User, V3AuthProto: "SHA", V3AuthPass: simV3AuthPass, V3PrivProto: "AES", V3PrivPass: simV3PrivPass,
		}
		out, err := col.Collect(ctx, in)
		if err != nil {
			t.Fatalf("SNMPv3 讀取失敗: %v", err)
		}
		if len(out.ARPObservations) == 0 {
			t.Fatal("應以 SNMPv3 讀到 VRF CORP 的 ARP 資料")
		}

		in.V3AuthPass = "WrongV3AuthPass#41"
		if _, err := col.Collect(ctx, in); err == nil {
			t.Error("驗證密碼錯誤時應讀取失敗")
		}
	})

	t.Run("SNMPv3 空白 context 對應預設 VRF", func(t *testing.T) {
		in := interfaces.VRFReadInput{
			MgmtIP: sim.host, SNMPPort: sim.port, TimeoutMs: 3000, Platform: "nx-os",
			SNMPVersion: "v3", VRFName: "default",
			V3Username: simV3User, V3AuthProto: "SHA", V3AuthPass: simV3AuthPass, V3PrivProto: "AES", V3PrivPass: simV3PrivPass,
		}
		out, err := col.Collect(ctx, in)
		if err != nil {
			t.Fatalf("SNMPv3 空白 context 讀取失敗: %v", err)
		}
		if out.InterfaceNames[151060490] != "Vlan10" {
			t.Errorf("應讀到 NX-OS 預設 VRF 的資料：%v", out.InterfaceNames)
		}
	})

	t.Run("經由本機的第二個位址也能讀取", func(t *testing.T) {
		in := v2c
		in.MgmtIP = secondLocalIPv4(t)
		if _, err := col.Collect(ctx, in); err != nil {
			t.Fatalf("經由 %s 讀取失敗: %v", in.MgmtIP, err)
		}
	})

	t.Run("資料移除後讀取失敗", func(t *testing.T) {
		sim.removeData(t, "public")
		in := v2c
		in.TimeoutMs = 800
		if _, err := col.Collect(ctx, in); err == nil {
			t.Error("資料移除後應讀取失敗")
		}
	})
}
