package test

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jeffhou369/wurzburg/backend/internal/testutil/apicontract"
)

// knownUncoveredOperations 列出描述檔有定義、但目前沒有整合測試打到的操作，以及原因。
// 新增操作時應同時補上測試；不要為了讓檢查通過而把操作加進這份清單。
var knownUncoveredOperations = map[string]string{}

// TestMain 在整套整合測試跑完後檢查 API 契約的涵蓋率：
// 描述檔中的每個操作都要至少有一個回應被驗證過，否則描述檔與實作可能已經脫節而沒有測試發現。
func TestMain(m *testing.M) {
	code := m.Run()
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
