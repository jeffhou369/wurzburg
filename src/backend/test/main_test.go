package test

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/testutil/apicontract"
	"github.com/jeffhou369/wurzburg/backend/internal/testutil/secretscan"
)

// knownUncoveredOperations 列出描述檔有定義、但目前沒有整合測試打到的操作，以及原因。
// 新增操作時應同時補上測試；不要為了讓檢查通過而把操作加進這份清單。
var knownUncoveredOperations = map[string]string{}

// TestMain 負責整套整合測試層級的兩項檢查：
//   - Secret 掃描：擷取測試期間的全部日誌，結束時連同寄出的信重新掃描，任何洩漏都讓測試失敗。
//   - API 契約涵蓋率：描述檔中的每個操作都要至少有一個回應被驗證過，否則描述檔與實作可能已經脫節而沒有測試發現。
func TestMain(m *testing.M) {
	scanner := secretscan.Shared()
	// 測試期間應用程式的全部日誌（slog，以及經由它輸出的標準 log）都經過 Secret 掃描，再照常印到 stderr。
	// 等級設為 Debug，讓正式環境預設不輸出的除錯訊息也在掃描範圍。
	slog.SetDefault(slog.New(slog.NewTextHandler(scanner.LogWriter(os.Stderr), &slog.HandlerOptions{Level: slog.LevelDebug})))
	// 不經由 API 送出、因此不會被自動登錄的 Secret：測試用的 WURZBURG_SECRET_KEY、預覽簽章金鑰與測試資料庫的密碼。
	// 64 字元的 WURZBURG_SECRET_KEY 是 32 字元那把重複兩次，登錄 32 字元的即可涵蓋。
	for label, value := range map[string]string{
		"WURZBURG_SECRET_KEY":      "0123456789abcdef0123456789abcdef",
		"WURZBURG_SECRET_KEY（全 a）": strings.Repeat("a", 64),
		"預覽簽章金鑰（管理範圍測試）":           "test_token_secret_32bytes_length!!",
		"預覽簽章金鑰（多實例測試）":            "sa-shared-secret-for-two-instances",
		"測試資料庫密碼":                  "wurzburg_test_pw",
	} {
		if err := scanner.Register(label, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	code := m.Run()

	// 日誌與信件可能在 Secret 第一次經由 API 送出（被登錄）之前就已產生，結束時用完整的清單再掃一次。
	scanner.Rescan()
	if findings := scanner.Findings(); len(findings) > 0 {
		fmt.Fprintf(os.Stderr, "Secret 掃描發現 %d 處洩漏：\n", len(findings))
		for _, f := range findings {
			fmt.Fprintln(os.Stderr, "  - "+f.String())
		}
		code = 1
	}
	if code == 0 && contractCoverageApplies() {
		if problems := contractCoverageProblems(); len(problems) > 0 {
			fmt.Fprintln(os.Stderr, "API 契約涵蓋率檢查失敗：")
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "  - "+p)
			}
			code = 1
		}
	}
	os.Exit(code)
}

// contractCoverageApplies 只在完整執行時檢查涵蓋率：
// -short 會略過需要資料庫的測試，-run／-skip 只跑部分測試，這些情況下沒打到的操作不代表缺測試。
func contractCoverageApplies() bool {
	if testing.Short() {
		return false
	}
	for _, name := range []string{"test.run", "test.skip", "test.list"} {
		if f := flag.Lookup(name); f != nil && f.Value.String() != "" {
			return false
		}
	}
	return true
}

func contractCoverageProblems() []string {
	checker, err := apicontract.Shared()
	if err != nil {
		return []string{"載入 OpenAPI 描述檔失敗: " + err.Error()}
	}
	var problems []string
	uncovered := map[string]bool{}
	for _, op := range checker.Uncovered() {
		uncovered[op] = true
		if _, known := knownUncoveredOperations[op]; !known {
			problems = append(problems, "沒有任何整合測試驗證過這個操作的回應："+op)
		}
	}
	for op := range knownUncoveredOperations {
		if !uncovered[op] {
			problems = append(problems, "這個操作已有測試涵蓋，請從 knownUncoveredOperations 移除："+op)
		}
	}
	if len(problems) > 0 {
		problems = append(problems, fmt.Sprintf("描述檔共 %d 個操作，%d 個未涵蓋（%s）",
			len(checker.Operations()), len(uncovered), strings.TrimSpace("其中已登記例外 "+fmt.Sprint(len(knownUncoveredOperations))+" 個")))
	}
	return problems
}
