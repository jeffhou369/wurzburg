package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jeffhou369/wurzburg/backend/internal/clock"
	"github.com/jeffhou369/wurzburg/backend/internal/collector"
	"github.com/jeffhou369/wurzburg/backend/internal/interfaces"
)

// snmp-capture 為實機驗證專用命令列工具，用來對指定 Cisco 交換器讀取並將原始回應轉存為 .walk 測試資料檔。
func main() {
	target := flag.String("target", "", "交換器管理 IPv4")
	port := flag.Int("port", 161, "SNMP UDP 埠號")
	platform := flag.String("platform", "ios-xe", "交換器平台 (ios-xe | nx-os)")
	version := flag.String("version", "v3", "SNMP 版本 (v2c | v3)")
	community := flag.String("community", "", "SNMPv2c 唯讀 Community")
	vrfName := flag.String("vrf", "default", "VRF 名稱")
	ctxName := flag.String("context", "", "SNMPv3 Context 名稱")
	v3User := flag.String("v3-user", "", "SNMPv3 使用者名稱")
	v3AuthProto := flag.String("v3-auth-proto", "SHA256", "SNMPv3 Auth 演算法")
	v3AuthPass := flag.String("v3-auth-pass", "", "SNMPv3 Auth 密語")
	v3PrivProto := flag.String("v3-priv-proto", "AES", "SNMPv3 Priv 演算法")
	v3PrivPass := flag.String("v3-priv-pass", "", "SNMPv3 Priv 密語")
	output := flag.String("output", "", "輸出檔案路徑（留空則輸出至標準輸出）")
	flag.Parse()

	if *target == "" {
		fmt.Fprintln(os.Stderr, "必須指定 -target 交換器管理 IP")
		os.Exit(1)
	}

	input := interfaces.VRFReadInput{
		MgmtIP:        *target,
		Platform:      *platform,
		SNMPVersion:   *version,
		SNMPPort:      *port,
		TimeoutMs:     5000,
		VRFName:       *vrfName,
		SNMPContext:   *ctxName,
		SNMPCommunity: *community,
		V3Username:    *v3User,
		V3AuthProto:   *v3AuthProto,
		V3AuthPass:    *v3AuthPass,
		V3PrivProto:   *v3PrivProto,
		V3PrivPass:    *v3PrivPass,
	}

	outWriter := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "無法建立輸出檔: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = f.Close() }()
		outWriter = f
	}

	col := collector.NewSNMPCollector(clock.NewRealClock())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := col.CaptureWalk(ctx, input, outWriter); err != nil {
		fmt.Fprintf(os.Stderr, "擷取 SNMP 回應失敗: %v\n", err)
		os.Exit(1)
	}
}
