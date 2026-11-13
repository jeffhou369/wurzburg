#!/usr/bin/env bash
# 執行瀏覽器關鍵流程（第 42 張工作項目）。
#
# 用法：
#   test/browser/run.sh           建置前端、內嵌進後端、啟動隔離環境並執行全部 Playwright 流程
#   test/browser/run.sh --hold    只啟動隔離環境並保持運作，供手動探索畫面；環境資訊寫在 test/browser/.env.json，
#                                 刪除該檔案或按 Ctrl+C 結束
#
# 需要：Docker、Go、Node.js。第一次執行前先在 test/browser 執行 npm ci 與 npx playwright install chromium。
# 環境是拋棄式的 Testcontainers 容器，不會動到既有部署的容器與 volume。
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

echo "==> 建置前端"
(cd "$root/src/frontend" && npm run build)

echo "==> 內嵌前端到後端"
dist="$root/src/backend/internal/static/dist"
find "$dist" -mindepth 1 ! -name .gitkeep -delete
cp -R "$root/src/frontend/dist/frontend/browser/." "$dist/"

export WURZBURG_BROWSER_TESTS=1
if [ "${1:-}" = "--hold" ]; then
  export WURZBURG_BROWSER_HOLD_FILE="$root/test/browser/.env.json"
  echo "==> 啟動隔離環境（保持運作）"
else
  echo "==> 啟動隔離環境並執行瀏覽器流程"
fi
cd "$root/src/backend"
exec go test -count=1 -timeout 70m -run '^TestBrowserFlows$' -v ./test/
