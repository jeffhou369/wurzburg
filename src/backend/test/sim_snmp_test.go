package test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SNMP 模擬器：以 Testcontainers 啟動 snmpsim，讓真正的採集模組（gosnmp）經由 UDP 讀取。
//
// snmpsim 以資料檔名稱決定回應哪一份資料：
//   - SNMPv2c：community 即檔名，例如 community 為 public 時回應 public.snmprec；
//     非預設 VRF 沿用採集模組的 community@context 慣例，檔名為 public@CORP.snmprec。
//   - SNMPv3：contextName 即檔名；context 空白（預設 VRF）時是 snmpSimV3DefaultContext。
//
// 資料檔可在執行中更換或移除，用來模擬交換器回報內容改變，或某筆 VRF 讀取設定讀不到資料。
// 但資料檔的名稱必須在啟動時就存在：snmpsim 只在啟動時把資料檔登記成可回應的 community 與 context，
// 執行中才新增的名稱不會有回應。之後要用到的名稱，啟動時先放好內容即可。

// snmpSimImage 鎖定映像的 digest，避免上游更新造成測試結果改變。
// 對應 ghcr.io/lextudio/docker-snmpsim:master（2025-11-20 建置）。升級時更新 digest 並重跑整套測試。
const snmpSimImage = "ghcr.io/lextudio/docker-snmpsim@sha256:ff7878dc0f5651b96be88c0bf88b348b72a52f1bde2ead244454a9095d73731e"

const snmpSimDataDir = "/data"

// snmpSimProbeCommunity 是啟動時用來確認模擬器已能回應的 community，情境不使用它。
const snmpSimProbeCommunity = "readiness-probe"

// snmpSimV3DefaultContext 是 SNMPv3 以空白 context 讀取時，snmpsim 回應的資料檔名稱。
// snmpsim 把資料目錄最上層名為 self 的資料檔登記為空白的 context（見 snmpsim/datafile.py 的 process_file_extension）。
const snmpSimV3DefaultContext = "self"

// SNMPv3 使用者；模擬器與交換器設定使用同一組。
const (
	simV3User     = "wurzburg-ro"
	simV3AuthPass = "SimV3AuthPass#41"
	simV3PrivPass = "SimV3PrivPass#41"
)

type snmpSim struct {
	container testcontainers.Container
	host      string // 可連到模擬器的 IPv4 位址
	port      int    // 對應到容器 161/udp 的主機埠

	mu    sync.Mutex
	mtime int64 // 最近一次寫入資料檔時使用的修改時間
}

// startSNMPSim 啟動 snmpsim 容器。initial 是啟動時就存在的資料檔（檔名不含副檔名 → snmprec 內容），
// 必須涵蓋測試會用到的所有 community 與 context。
func startSNMPSim(t *testing.T, initial map[string][]byte) *snmpSim {
	t.Helper()
	ctx := context.Background()

	files := []testcontainers.ContainerFile{{
		Reader:            strings.NewReader("1.3.6.1.2.1.1.1.0|4|snmpsim readiness probe\n"),
		ContainerFilePath: snmpSimDataDir + "/" + snmpSimProbeCommunity + ".snmprec",
		FileMode:          0o644,
	}}
	for name, content := range initial {
		files = append(files, testcontainers.ContainerFile{
			Reader:            bytes.NewReader(content),
			ContainerFilePath: snmpSimDataDir + "/" + name + ".snmprec",
			FileMode:          0o644,
		})
	}
	flags := []string{
		"--data-dir=" + snmpSimDataDir,
		"--v3-user=" + simV3User,
		"--v3-auth-proto=SHA", "--v3-auth-key=" + simV3AuthPass,
		"--v3-priv-proto=AES", "--v3-priv-key=" + simV3PrivPass,
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        snmpSimImage,
			ExposedPorts: []string{"161/udp"},
			Env: map[string]string{
				"SNMPTRAPD_ENABLED": "0",
				"EXTRA_FLAGS":       strings.Join(flags, " "),
			},
			Files:      files,
			WaitingFor: wait.ForLog("Listening at UDP/IPv4 endpoint").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("啟動 SNMP 模擬器失敗: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	port, err := c.MappedPort(ctx, "161/udp")
	if err != nil {
		t.Fatalf("取得 SNMP 模擬器的埠失敗: %v", err)
	}
	sim := &snmpSim{container: c, host: "127.0.0.1", port: int(port.Num())}
	sim.waitUntilAnswering(t)
	return sim
}

// waitUntilAnswering 等到模擬器真的回應 SNMP 請求。
// snmpsim 印出開始聆聽的訊息後，還要一小段時間才會處理請求；太早送出的第一個請求會沒有回應。
func (s *snmpSim) waitUntilAnswering(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		g := &gosnmp.GoSNMP{
			Target: s.host, Port: uint16(s.port), Transport: "udp",
			Version: gosnmp.Version2c, Community: snmpSimProbeCommunity,
			Timeout: time.Second, Retries: 0,
		}
		if lastErr = g.Connect(); lastErr == nil {
			var result *gosnmp.SnmpPacket
			result, lastErr = g.Get([]string{"1.3.6.1.2.1.1.1.0"})
			_ = g.Conn.Close()
			if lastErr == nil && len(result.Variables) == 1 && result.Variables[0].Type == gosnmp.OctetString {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("SNMP 模擬器在期限內沒有回應: %v", lastErr)
}

// setData 寫入或取代一份資料檔。name 是 community（v2c）或 contextName（v3）。
func (s *snmpSim) setData(t *testing.T, name string, rec []byte) {
	t.Helper()
	// 先寫到暫存檔再改名，避免模擬器讀到寫了一半的檔案
	tmp := snmpSimDataDir + "/." + name + ".tmp"
	if err := s.container.CopyToContainer(context.Background(), rec, tmp, 0o644); err != nil {
		t.Fatalf("寫入 SNMP 模擬資料 %s 失敗: %v", name, err)
	}
	// snmpsim 以資料檔的修改時間（秒）判斷是否重建索引；同一秒內連續更換會被當成沒有變動。
	// 每次寫入都給一個嚴格遞增的修改時間，確保模擬器一定會重新讀取。
	s.mu.Lock()
	if s.mtime == 0 {
		s.mtime = time.Now().Unix() + 60
	}
	s.mtime++
	stamp := s.mtime
	s.mu.Unlock()
	s.exec(t, "touch", "-d", "@"+strconv.FormatInt(stamp, 10), tmp)
	s.exec(t, "mv", "-f", tmp, snmpSimDataDir+"/"+name+".snmprec")
}

// removeData 移除一份資料檔：之後以這個 community 或 context 讀取會得不到回應。
func (s *snmpSim) removeData(t *testing.T, name string) {
	t.Helper()
	s.exec(t, "rm", "-f", snmpSimDataDir+"/"+name+".snmprec")
}

func (s *snmpSim) exec(t *testing.T, cmd ...string) {
	t.Helper()
	code, _, err := s.container.Exec(context.Background(), cmd)
	if err != nil || code != 0 {
		t.Fatalf("在 SNMP 模擬器執行 %v 失敗: code=%d err=%v", cmd, code, err)
	}
}

// simInterface 是模擬交換器上的一個三層介面。
type simInterface struct {
	Index int64
	Name  string
	IP    string
	Mask  string
}

// simARP 是模擬交換器 ARP 表的一列。Type 依 ipNetToMediaType：2 invalid、3 dynamic、4 static；0 視為 dynamic。
type simARP struct {
	IP      string
	MAC     string
	IfIndex int64
	Type    int
}

// buildSNMPRec 把介面與 ARP 表轉成 snmpsim 的 snmprec 內容（OID|型別|值，依 OID 排序）。
func buildSNMPRec(ifaces []simInterface, arps []simARP) []byte {
	var rows []snmpRecRow
	for _, i := range ifaces {
		rows = append(rows,
			snmpRecRow{fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.1.%d", i.Index), "4", i.Name},
			snmpRecRow{"1.3.6.1.2.1.4.20.1.1." + i.IP, "64", i.IP},
			snmpRecRow{"1.3.6.1.2.1.4.20.1.2." + i.IP, "2", strconv.FormatInt(i.Index, 10)},
			snmpRecRow{"1.3.6.1.2.1.4.20.1.3." + i.IP, "64", i.Mask},
		)
	}
	for _, a := range arps {
		typ := a.Type
		if typ == 0 {
			typ = 3
		}
		suffix := fmt.Sprintf("%d.%s", a.IfIndex, a.IP)
		rows = append(rows,
			snmpRecRow{"1.3.6.1.2.1.4.22.1.1." + suffix, "2", strconv.FormatInt(a.IfIndex, 10)},
			snmpRecRow{"1.3.6.1.2.1.4.22.1.2." + suffix, "4x", strings.ReplaceAll(strings.ToLower(a.MAC), ":", "")},
			snmpRecRow{"1.3.6.1.2.1.4.22.1.3." + suffix, "64", a.IP},
			snmpRecRow{"1.3.6.1.2.1.4.22.1.4." + suffix, "2", strconv.Itoa(typ)},
		)
	}
	return encodeSNMPRec(rows)
}

type snmpRecRow struct{ oid, tag, value string }

func encodeSNMPRec(rows []snmpRecRow) []byte {
	sort.Slice(rows, func(i, j int) bool { return oidLess(rows[i].oid, rows[j].oid) })
	var b bytes.Buffer
	for _, r := range rows {
		fmt.Fprintf(&b, "%s|%s|%s\n", r.oid, r.tag, r.value)
	}
	return b.Bytes()
}

func oidLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}

// walkFileToSNMPRec 把 test/testdata/snmp/ 的 walk 檔（snmpwalk 文字輸出）轉成 snmprec，
// 讓模擬器逐列回放檔案中的每一筆資料，包含無效的 ARP 列與次要位址。
func walkFileToSNMPRec(t *testing.T, relPath string) []byte {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "test", "testdata", "snmp", relPath)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("開啟 walk 檔失敗: %v", err)
	}
	defer func() { _ = f.Close() }()

	var rows []snmpRecRow
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		oid, rest, ok := strings.Cut(line, " = ")
		if !ok {
			t.Fatalf("%s：無法解析的 walk 列 %q", relPath, line)
		}
		typ, value, ok := strings.Cut(rest, ": ")
		if !ok {
			t.Fatalf("%s：無法解析的 walk 值 %q", relPath, line)
		}
		row := snmpRecRow{oid: strings.TrimPrefix(oid, ".")}
		switch typ {
		case "STRING":
			row.tag, row.value = "4", strings.Trim(value, `"`)
		case "INTEGER":
			row.tag, row.value = "2", value
		case "IpAddress":
			row.tag, row.value = "64", value
		case "Hex-STRING":
			row.tag, row.value = "4x", strings.ToLower(strings.ReplaceAll(value, " ", ""))
		default:
			t.Fatalf("%s：walk 檔出現未處理的型別 %q", relPath, typ)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("讀取 walk 檔失敗: %v", err)
	}
	return encodeSNMPRec(rows)
}

// secondLocalIPv4 回傳本機除了 127.0.0.1 以外、可連到已發佈容器埠的 IPv4 位址。
// 交換器的管理 IP 不可重複，兩台模擬交換器需要兩個不同的位址。
func secondLocalIPv4(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("列舉本機位址失敗: %v", err)
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	// CI 上一定要有第二個位址，否則這個情境會被悄悄略過而沒有人發現
	if os.Getenv("CI") != "" {
		t.Fatal("找不到 127.0.0.1 以外的 IPv4 位址，無法模擬第二台交換器")
	}
	t.Skip("本機沒有 127.0.0.1 以外的 IPv4 位址，無法模擬第二台交換器")
	return ""
}
