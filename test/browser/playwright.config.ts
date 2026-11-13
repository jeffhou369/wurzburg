import { defineConfig, devices } from '@playwright/test';

// 環境由 src/backend/test/browser_env_test.go 啟動，位址與帳密經由 WZ_* 環境變數傳入。
// 六組流程共用同一套系統、依序執行，後面的流程建立在前面的狀態上，所以只用一個 worker 且不重試。
export default defineConfig({
  testDir: './flows',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: [['list']],
  outputDir: './test-results',
  use: {
    baseURL: process.env.WZ_BASE_URL,
    locale: 'zh-TW',
    timezoneId: 'Asia/Taipei',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
