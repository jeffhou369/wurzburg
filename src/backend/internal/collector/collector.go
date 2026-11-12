package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"

	"github.com/gosnmp/gosnmp"
	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// SNMPCollector 封裝對 Cisco IOS-XE 與 NX-OS 的 SNMP 採集邏輯。
// 整個系統只有此套件接觸 SNMP 協定與平台 OID 細節。
type SNMPCollector struct {
	clock clock.Clock
}

func NewSNMPCollector(clk clock.Clock) *SNMPCollector {
	if clk == nil {
		clk = clock.NewRealClock()
	}
	return &SNMPCollector{clock: clk}
}

func (c *SNMPCollector) buildClient(ctx context.Context, input interfaces.VRFReadInput) (*gosnmp.GoSNMP, error) {
	port := uint16(161)
	if input.SNMPPort > 0 {
		port = uint16(input.SNMPPort)
	}
	timeout := 3000
	if input.TimeoutMs > 0 {
		timeout = input.TimeoutMs
	}

	g := &gosnmp.GoSNMP{
		Target:    input.MgmtIP,
		Port:      port,
		Transport: "udp",
		Context:   ctx,
		Timeout:   timeDurationMs(timeout),
		Retries:   0, // 嚴格停用程式庫自動重試，由輪詢排程器負責唯一的一次重試
	}

	if input.SNMPVersion == "v2c" {
		g.Version = gosnmp.Version2c
		g.Community = input.SNMPCommunity
		// 若為非全域 VRF 且未單獨設定專屬 community，以傳統 community@context 慣例處理
		if input.VRFName != "" && input.VRFName != "default" && input.SNMPContext != "" && !strings.Contains(g.Community, "@") {
			g.Community = g.Community + "@" + input.SNMPContext
		}
	} else {
		g.Version = gosnmp.Version3
		g.SecurityModel = gosnmp.UserSecurityModel
		g.ContextName = input.SNMPContext

		authProto := mapAuthProto(input.V3AuthProto)
		privProto := mapPrivProto(input.V3PrivProto)

		var flags gosnmp.SnmpV3MsgFlags
		if input.V3PrivPass != "" && privProto != gosnmp.NoPriv {
			flags = gosnmp.AuthPriv
		} else if input.V3AuthPass != "" && authProto != gosnmp.NoAuth {
			flags = gosnmp.AuthNoPriv
		} else {
			flags = gosnmp.NoAuthNoPriv
		}
		g.MsgFlags = flags
		g.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 input.V3Username,
			AuthenticationProtocol:   authProto,
			AuthenticationPassphrase: input.V3AuthPass,
			PrivacyProtocol:          privProto,
			PrivacyPassphrase:        input.V3PrivPass,
		}
	}

	return g, nil
}

// Collect 執行單筆 VRF 讀取設定的 SNMP 讀取。
func (c *SNMPCollector) Collect(ctx context.Context, input interfaces.VRFReadInput) (*interfaces.PollOutput, error) {
	g, err := c.buildClient(ctx, input)
	if err != nil {
		return nil, &interfaces.CollectError{Class: interfaces.ErrClassAuthFailed, Summary: err.Error(), Cause: err}
	}

	if err := g.Connect(); err != nil {
		return nil, classifyError(err)
	}
	defer func() { _ = g.Conn.Close() }()

	ifNames := make(map[int64]string)
	ifDescrs := make(map[int64]string)

	// 1. 讀取介面名稱 (IF-MIB::ifName) 與備援 (ifDescr)
	_ = g.BulkWalk(".1.3.6.1.2.1.31.1.1.1.1", func(pdu gosnmp.SnmpPDU) error {
		idx := extractLastIndex(pdu.Name)
		if idx > 0 {
			if b, ok := pdu.Value.([]byte); ok {
				ifNames[idx] = string(b)
			}
		}
		return nil
	})
	if len(ifNames) == 0 {
		_ = g.BulkWalk(".1.3.6.1.2.1.2.2.1.2", func(pdu gosnmp.SnmpPDU) error {
			idx := extractLastIndex(pdu.Name)
			if idx > 0 {
				if b, ok := pdu.Value.([]byte); ok {
					ifDescrs[idx] = string(b)
				}
			}
			return nil
		})
	}

	mergedIfNames := make(map[int64]string)
	for k, v := range ifDescrs {
		mergedIfNames[k] = v
	}
	for k, v := range ifNames {
		mergedIfNames[k] = v
	}

	// 2. 讀取介面位址表 (RFC1213-MIB::ipAddrTable)
	ipIfIdx := make(map[string]int64)
	ipMasks := make(map[string]string)

	err = g.BulkWalk(".1.3.6.1.2.1.4.20.1.2", func(pdu gosnmp.SnmpPDU) error {
		ip := extractIPSuffix(pdu.Name)
		if ip != "" {
			ipIfIdx[ip] = gosnmp.ToBigInt(pdu.Value).Int64()
		}
		return nil
	})
	if err != nil {
		return nil, classifyError(err)
	}

	err = g.BulkWalk(".1.3.6.1.2.1.4.20.1.3", func(pdu gosnmp.SnmpPDU) error {
		ip := extractIPSuffix(pdu.Name)
		if ip != "" {
			if maskStr, ok := pdu.Value.(string); ok {
				ipMasks[ip] = maskStr
			}
		}
		return nil
	})
	if err != nil {
		return nil, classifyError(err)
	}

	var ifAddresses []interfaces.InterfaceAddress
	for ip, mask := range ipMasks {
		idx := ipIfIdx[ip]
		ifAddresses = append(ifAddresses, interfaces.InterfaceAddress{
			IPAddress: ip,
			NetMask:   mask,
			IfIndex:   idx,
			IfName:    mergedIfNames[idx],
		})
	}

	// 3. 讀取 ARP 表 (RFC1213-MIB::ipNetToMediaTable)
	type arpTemp struct {
		ifIndex int64
		mac     string
		ip      string
		arpType string
	}
	arpMap := make(map[string]*arpTemp)

	err = g.BulkWalk(".1.3.6.1.2.1.4.22.1", func(pdu gosnmp.SnmpPDU) error {
		oid := strings.TrimPrefix(pdu.Name, ".")
		var colPrefix string
		var field string
		switch {
		case strings.HasPrefix(oid, oidArpIfIndexPrefix):
			colPrefix = oidArpIfIndexPrefix
			field = "ifIndex"
		case strings.HasPrefix(oid, oidArpPhysAddrPrefix):
			colPrefix = oidArpPhysAddrPrefix
			field = "mac"
		case strings.HasPrefix(oid, oidArpNetAddrPrefix):
			colPrefix = oidArpNetAddrPrefix
			field = "ip"
		case strings.HasPrefix(oid, oidArpTypePrefix):
			colPrefix = oidArpTypePrefix
			field = "type"
		default:
			return nil
		}

		suffix := strings.TrimPrefix(oid, colPrefix)
		entry, ok := arpMap[suffix]
		if !ok {
			entry = &arpTemp{arpType: "dynamic"}
			arpMap[suffix] = entry
		}

		switch field {
		case "ifIndex":
			entry.ifIndex = gosnmp.ToBigInt(pdu.Value).Int64()
		case "mac":
			if b, ok := pdu.Value.([]byte); ok && len(b) == 6 {
				entry.mac = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
			}
		case "ip":
			if ipStr, ok := pdu.Value.(string); ok {
				entry.ip = ipStr
			}
		case "type":
			entry.arpType = parseArpType(fmt.Sprintf("%d", gosnmp.ToBigInt(pdu.Value).Int64()), "INTEGER")
		}
		return nil
	})
	if err != nil {
		return nil, classifyError(err)
	}

	var observations []interfaces.ARPObservation
	for _, a := range arpMap {
		if a.arpType == "invalid" {
			continue
		}
		if net.ParseIP(a.ip).To4() == nil {
			continue
		}
		observations = append(observations, interfaces.ARPObservation{
			IPAddress:  a.ip,
			MACAddress: a.mac,
			IfIndex:    a.ifIndex,
			IfName:     mergedIfNames[a.ifIndex],
			ARPType:    a.arpType,
		})
	}

	if len(observations) == 0 && len(ifAddresses) == 0 {
		return nil, &interfaces.CollectError{
			Class:   interfaces.ErrClassNoData,
			Summary: "交換器未回傳任何介面位址或 ARP 資料",
		}
	}

	return &interfaces.PollOutput{
		VRFConfigID:        input.VRFConfigID,
		SwitchID:           input.SwitchID,
		VRFName:            input.VRFName,
		CollectedAt:        c.clock.Now(),
		ARPObservations:    observations,
		InterfaceAddresses: ifAddresses,
		InterfaceNames:     mergedIfNames,
	}, nil
}

// CaptureWalk 將實機的原始 SNMP 回應轉存成標準 walk 格式文字，供日後實機驗證留存測試資料。
func (c *SNMPCollector) CaptureWalk(ctx context.Context, input interfaces.VRFReadInput, w io.Writer) error {
	g, err := c.buildClient(ctx, input)
	if err != nil {
		return err
	}
	if err := g.Connect(); err != nil {
		return err
	}
	defer func() { _ = g.Conn.Close() }()

	_, _ = fmt.Fprintf(w, "# 實機擷取測試資料\n# 平台：%s\n# VRF：%s\n", input.Platform, input.VRFName)

	oids := []string{
		".1.3.6.1.2.1.31.1.1.1.1", // ifName
		".1.3.6.1.2.1.4.20.1",     // ipAddrTable
		".1.3.6.1.2.1.4.22.1",     // ipNetToMediaTable
	}

	for _, base := range oids {
		err := g.BulkWalk(base, func(pdu gosnmp.SnmpPDU) error {
			switch pdu.Type {
			case gosnmp.OctetString:
				b, _ := pdu.Value.([]byte)
				if strings.HasPrefix(strings.TrimPrefix(pdu.Name, "."), oidArpPhysAddrPrefix) {
					var hexes []string
					for _, bt := range b {
						hexes = append(hexes, fmt.Sprintf("%02X", bt))
					}
					_, _ = fmt.Fprintf(w, "%s = Hex-STRING: %s\n", pdu.Name, strings.Join(hexes, " "))
				} else {
					_, _ = fmt.Fprintf(w, "%s = STRING: %q\n", pdu.Name, string(b))
				}
			case gosnmp.IPAddress:
				_, _ = fmt.Fprintf(w, "%s = IpAddress: %v\n", pdu.Name, pdu.Value)
			default:
				_, _ = fmt.Fprintf(w, "%s = INTEGER: %v\n", pdu.Name, gosnmp.ToBigInt(pdu.Value))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func classifyError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() || strings.Contains(msg, "timeout") {
		return &interfaces.CollectError{Class: interfaces.ErrClassTimeout, Summary: "SNMP 請求逾時", Cause: err}
	}
	if errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(msg, "connection refused") {
		return &interfaces.CollectError{Class: interfaces.ErrClassConnectionRefused, Summary: "目標交換器拒絕 SNMP 連線", Cause: err}
	}
	if strings.Contains(msg, "usm") || strings.Contains(msg, "auth") || strings.Contains(msg, "decryption") || strings.Contains(msg, "unknown user") {
		return &interfaces.CollectError{Class: interfaces.ErrClassAuthFailed, Summary: "SNMP 認證或加密參數失敗", Cause: err}
	}
	return &interfaces.CollectError{Class: interfaces.ErrClassTimeout, Summary: err.Error(), Cause: err}
}

func extractLastIndex(oid string) int64 {
	parts := strings.Split(oid, ".")
	if len(parts) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	return v
}

func extractIPSuffix(oid string) string {
	parts := strings.Split(oid, ".")
	if len(parts) < 4 {
		return ""
	}
	return strings.Join(parts[len(parts)-4:], ".")
}

func mapAuthProto(p string) gosnmp.SnmpV3AuthProtocol {
	switch strings.ToUpper(p) {
	case "MD5":
		return gosnmp.MD5
	case "SHA256":
		return gosnmp.SHA256
	case "SHA":
		return gosnmp.SHA
	default:
		return gosnmp.SHA256
	}
}

func mapPrivProto(p string) gosnmp.SnmpV3PrivProtocol {
	switch strings.ToUpper(p) {
	case "DES":
		return gosnmp.DES
	case "AES128", "AES":
		return gosnmp.AES
	default:
		return gosnmp.AES
	}
}
