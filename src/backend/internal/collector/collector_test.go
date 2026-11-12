package collector

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

func TestParseWalkFiles_IOSXE(t *testing.T) {
	defaultPath := filepath.Join("..", "..", "..", "..", "test", "testdata", "snmp", "ios-xe", "default-vrf.walk")
	out, err := ParseWalkFile(defaultPath)
	if err != nil {
		t.Fatalf("解析 ios-xe default-vrf.walk 失敗: %v", err)
	}

	// 1. 驗證介面名稱
	if out.InterfaceNames[11] != "Vlan10" || out.InterfaceNames[12] != "Vlan20" {
		t.Errorf("介面名稱不符: %+v", out.InterfaceNames)
	}

	// 2. 驗證次要位址（Secondary IP）出現在介面位址表中
	foundSec := false
	foundPrimary := false
	for _, addr := range out.InterfaceAddresses {
		if addr.IPAddress == "10.10.11.2" && addr.NetMask == "255.255.255.128" && addr.IfName == "Vlan10" {
			foundSec = true
		}
		if addr.IPAddress == "10.10.10.2" && addr.NetMask == "255.255.255.0" && addr.IfName == "Vlan10" {
			foundPrimary = true
		}
	}
	if !foundPrimary || !foundSec {
		t.Errorf("未在介面位址表找到主位址或次要位址: %+v", out.InterfaceAddresses)
	}

	// 3. 驗證 invalid(2) 條目已被忽略，動態與靜態 ARP 格式正確保留
	arpByIP := make(map[string]interfaces.ARPObservation)
	for _, obs := range out.ARPObservations {
		arpByIP[obs.IPAddress] = obs
	}

	if _, exists := arpByIP["10.10.10.51"]; exists {
		t.Error("ARP 類型為 invalid 的 10.10.10.51 必須被忽略，但出現在結果中")
	}

	normal, exists := arpByIP["10.10.10.50"]
	if !exists || normal.MACAddress != "b8:27:eb:11:22:33" || normal.ARPType != "dynamic" || normal.IfName != "Vlan10" {
		t.Errorf("正常動態 ARP 觀測不符: %+v", normal)
	}

	hsrp, exists := arpByIP["10.10.10.1"]
	if !exists || hsrp.MACAddress != "00:00:0c:07:ac:0a" || hsrp.ARPType != "static" {
		t.Errorf("HSRP 靜態 ARP 觀測不符: %+v", hsrp)
	}

	// 4. 驗證 VRF CORP walk 解析
	corpPath := filepath.Join("..", "..", "..", "..", "test", "testdata", "snmp", "ios-xe", "vrf-corp.walk")
	corpOut, err := ParseWalkFile(corpPath)
	if err != nil {
		t.Fatalf("解析 ios-xe vrf-corp.walk 失敗: %v", err)
	}
	if len(corpOut.ARPObservations) != 3 {
		t.Errorf("VRF CORP 預期 3 筆 ARP，實際 %d 筆", len(corpOut.ARPObservations))
	}
}

func TestParseWalkFiles_NXOS(t *testing.T) {
	defaultPath := filepath.Join("..", "..", "..", "..", "test", "testdata", "snmp", "nx-os", "default-vrf.walk")
	out, err := ParseWalkFile(defaultPath)
	if err != nil {
		t.Fatalf("解析 nx-os default-vrf.walk 失敗: %v", err)
	}

	// 1. 驗證 Nexus 大編號 ifIndex (151060490) 與介面名稱
	if out.InterfaceNames[151060490] != "Vlan10" {
		t.Errorf("Nexus SVI ifIndex 名稱對應不符: %v", out.InterfaceNames[151060490])
	}

	// 2. 驗證 VRRP 虛擬 MAC (00:00:5e:00:01:0a)
	foundVRRP := false
	for _, obs := range out.ARPObservations {
		if obs.IPAddress == "10.10.10.1" && obs.MACAddress == "00:00:5e:00:01:0a" && obs.IfIndex == 151060490 {
			foundVRRP = true
		}
	}
	if !foundVRRP {
		t.Errorf("未在 NX-OS ARP 中找到 VRRP 觀測紀錄: %+v", out.ARPObservations)
	}

	// 3. 驗證 VRF PROD walk 解析
	prodPath := filepath.Join("..", "..", "..", "..", "test", "testdata", "snmp", "nx-os", "vrf-prod.walk")
	prodOut, err := ParseWalkFile(prodPath)
	if err != nil {
		t.Fatalf("解析 nx-os vrf-prod.walk 失敗: %v", err)
	}
	if len(prodOut.ARPObservations) != 2 {
		t.Errorf("VRF PROD 預期 2 筆 ARP，實際 %d 筆", len(prodOut.ARPObservations))
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err       error
		wantClass interfaces.ErrorClass
	}{
		{
			err:       &net.DNSError{IsTimeout: true},
			wantClass: interfaces.ErrClassTimeout,
		},
		{
			err:       syscall.ECONNREFUSED,
			wantClass: interfaces.ErrClassConnectionRefused,
		},
		{
			err:       errors.New("snmpv3 usm authentication failure"),
			wantClass: interfaces.ErrClassAuthFailed,
		},
	}

	for _, tc := range tests {
		classified := classifyError(tc.err)
		var cErr *interfaces.CollectError
		if !errors.As(classified, &cErr) {
			t.Fatalf("預期 CollectError，實際為 %T", classified)
		}
		if cErr.Class != tc.wantClass {
			t.Errorf("error %v: got class %s, want %s", tc.err, cErr.Class, tc.wantClass)
		}
	}
}

func TestFormatMAC(t *testing.T) {
	if got := formatMAC("00 1B D4 AA 10 02"); got != "00:1b:d4:aa:10:02" {
		t.Errorf("formatMAC hex-string failed: %q", got)
	}
	if got := formatMAC("b8-27-eb-11-22-33"); got != "b8:27:eb:11:22:33" {
		t.Errorf("formatMAC dash failed: %q", got)
	}
	if got := formatMAC("D4:3D:7E:44:55:66"); got != "d4:3d:7e:44:55:66" {
		t.Errorf("formatMAC case failed: %q", got)
	}
}

func TestParseWalk_DiscardNonIPv4(t *testing.T) {
	raw := `
.1.3.6.1.2.1.4.22.1.1.10.192.168.1.1 = INTEGER: 10
.1.3.6.1.2.1.4.22.1.2.10.192.168.1.1 = Hex-STRING: AA BB CC DD EE FF
.1.3.6.1.2.1.4.22.1.3.10.192.168.1.1 = IpAddress: 192.168.1.1
.1.3.6.1.2.1.4.22.1.4.10.192.168.1.1 = INTEGER: 3
.1.3.6.1.2.1.4.22.1.3.10.2001.db8.1 = STRING: "2001:db8::1"
`
	out, err := ParseWalk(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseWalk failed: %v", err)
	}
	if len(out.ARPObservations) != 1 {
		t.Errorf("預期僅保留 1 筆 IPv4，實際 %d 筆", len(out.ARPObservations))
	}
}
