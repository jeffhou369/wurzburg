package collector

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

const (
	oidIfNamePrefix         = "1.3.6.1.2.1.31.1.1.1.1."
	oidIfDescrPrefix        = "1.3.6.1.2.1.2.2.1.2."
	oidIpAdEntAddrPrefix    = "1.3.6.1.2.1.4.20.1.1."
	oidIpAdEntIfIdxPrefix   = "1.3.6.1.2.1.4.20.1.2."
	oidIpAdEntNetMaskPrefix = "1.3.6.1.2.1.4.20.1.3."
	oidArpIfIndexPrefix     = "1.3.6.1.2.1.4.22.1.1."
	oidArpPhysAddrPrefix    = "1.3.6.1.2.1.4.22.1.2."
	oidArpNetAddrPrefix     = "1.3.6.1.2.1.4.22.1.3."
	oidArpTypePrefix        = "1.3.6.1.2.1.4.22.1.4."
)

// ParseWalkFile 讀取標準 snmpwalk 格式檔案（如 testdata 內之 walk 檔），轉為系統統一的 PollOutput。
func ParseWalkFile(filePath string) (*interfaces.PollOutput, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("開啟 walk 檔案失敗: %w", err)
	}
	defer func() { _ = f.Close() }()
	return ParseWalk(f)
}

type arpEntry struct {
	ifIndex int64
	mac     string
	ip      string
	arpType string
}

// ParseWalk 解析文字串流之 OID 與值。
func ParseWalk(r io.Reader) (*interfaces.PollOutput, error) {
	scanner := bufio.NewScanner(r)

	ifNames := make(map[int64]string)
	ifDescrs := make(map[int64]string)

	ipAddrs := make(map[string]string) // ip -> ip
	ipIfIndexes := make(map[string]int64)
	ipMasks := make(map[string]string)

	arpEntries := make(map[string]*arpEntry) // key: ifIndex.ip

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		oid := strings.TrimSpace(parts[0])
		oid = strings.TrimPrefix(oid, ".")

		valPart := strings.TrimSpace(parts[1])
		valType, valStr := parseValTypeAndString(valPart)

		// 1. IfName
		if strings.HasPrefix(oid, oidIfNamePrefix) {
			idxStr := strings.TrimPrefix(oid, oidIfNamePrefix)
			if idx, err := strconv.ParseInt(idxStr, 10, 64); err == nil {
				ifNames[idx] = valStr
			}
			continue
		}

		// 2. IfDescr
		if strings.HasPrefix(oid, oidIfDescrPrefix) {
			idxStr := strings.TrimPrefix(oid, oidIfDescrPrefix)
			if idx, err := strconv.ParseInt(idxStr, 10, 64); err == nil {
				ifDescrs[idx] = valStr
			}
			continue
		}

		// 3. ipAddrTable
		if strings.HasPrefix(oid, oidIpAdEntAddrPrefix) {
			ip := strings.TrimPrefix(oid, oidIpAdEntAddrPrefix)
			ipAddrs[ip] = valStr
			continue
		}
		if strings.HasPrefix(oid, oidIpAdEntIfIdxPrefix) {
			ip := strings.TrimPrefix(oid, oidIpAdEntIfIdxPrefix)
			if idx, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				ipIfIndexes[ip] = idx
			}
			continue
		}
		if strings.HasPrefix(oid, oidIpAdEntNetMaskPrefix) {
			ip := strings.TrimPrefix(oid, oidIpAdEntNetMaskPrefix)
			ipMasks[ip] = valStr
			continue
		}

		// 4. ipNetToMediaTable (ARP)
		if strings.HasPrefix(oid, oidArpIfIndexPrefix) {
			suffix := strings.TrimPrefix(oid, oidArpIfIndexPrefix)
			entry := getOrCreateArp(arpEntries, suffix)
			if idx, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				entry.ifIndex = idx
			}
			continue
		}
		if strings.HasPrefix(oid, oidArpPhysAddrPrefix) {
			suffix := strings.TrimPrefix(oid, oidArpPhysAddrPrefix)
			entry := getOrCreateArp(arpEntries, suffix)
			entry.mac = formatMAC(valStr)
			continue
		}
		if strings.HasPrefix(oid, oidArpNetAddrPrefix) {
			suffix := strings.TrimPrefix(oid, oidArpNetAddrPrefix)
			entry := getOrCreateArp(arpEntries, suffix)
			entry.ip = valStr
			continue
		}
		if strings.HasPrefix(oid, oidArpTypePrefix) {
			suffix := strings.TrimPrefix(oid, oidArpTypePrefix)
			entry := getOrCreateArp(arpEntries, suffix)
			entry.arpType = parseArpType(valStr, valType)
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// 整合介面名稱
	mergedIfNames := make(map[int64]string)
	for k, v := range ifDescrs {
		mergedIfNames[k] = v
	}
	for k, v := range ifNames {
		mergedIfNames[k] = v
	}

	// 整合介面位址
	var ifAddresses []interfaces.InterfaceAddress
	for ip, mask := range ipMasks {
		idx := ipIfIndexes[ip]
		ifAddresses = append(ifAddresses, interfaces.InterfaceAddress{
			IPAddress: ip,
			NetMask:   mask,
			IfIndex:   idx,
			IfName:    mergedIfNames[idx],
		})
	}

	// 整合 ARP 觀測（忽略 invalid）
	var obs []interfaces.ARPObservation
	for _, a := range arpEntries {
		if a.arpType == "invalid" {
			continue
		}
		if net.ParseIP(a.ip).To4() == nil {
			continue // 忽略非 IPv4
		}
		obs = append(obs, interfaces.ARPObservation{
			IPAddress:  a.ip,
			MACAddress: a.mac,
			IfIndex:    a.ifIndex,
			IfName:     mergedIfNames[a.ifIndex],
			ARPType:    a.arpType,
		})
	}

	return &interfaces.PollOutput{
		CollectedAt:        time.Now().UTC(),
		ARPObservations:    obs,
		InterfaceAddresses: ifAddresses,
		InterfaceNames:     mergedIfNames,
	}, nil
}

func getOrCreateArp(m map[string]*arpEntry, key string) *arpEntry {
	if e, ok := m[key]; ok {
		return e
	}
	e := &arpEntry{arpType: "dynamic"}
	m[key] = e
	return e
}

func parseValTypeAndString(s string) (string, string) {
	idx := strings.Index(s, ":")
	if idx == -1 {
		return "", strings.Trim(s, `"`)
	}
	valType := strings.TrimSpace(s[:idx])
	valStr := strings.TrimSpace(s[idx+1:])
	return valType, strings.Trim(valStr, `"`)
}

func formatMAC(val string) string {
	val = strings.ReplaceAll(val, " ", ":")
	val = strings.ReplaceAll(val, "-", ":")
	parts := strings.Split(val, ":")
	var hexParts []string
	for _, p := range parts {
		if len(p) == 1 {
			hexParts = append(hexParts, "0"+strings.ToLower(p))
		} else if len(p) == 2 {
			hexParts = append(hexParts, strings.ToLower(p))
		}
	}
	if len(hexParts) == 6 {
		return strings.Join(hexParts, ":")
	}
	return strings.ToLower(val)
}

func parseArpType(valStr, _ string) string {
	switch valStr {
	case "2", "invalid":
		return "invalid"
	case "3", "dynamic":
		return "dynamic"
	case "4", "static":
		return "static"
	default:
		return "other"
	}
}
