// 僅供隔離測試環境：需以 review-admin / ReviewInitialPassword123 建立全新資料庫。
// 執行：playwright-cli -s=wurzburg-review run-code --filename=test/browser/review-smoke.js
// 不使用網路攔截、測試身分標頭或偽造 API 回應；測試密碼不可用於真實部署。
async (page) => {
  const origin = await page.evaluate(() => location.origin);
  const returnUrl = () => page.evaluate(() => new URL(location.href).searchParams.get('returnUrl'));
  const pendingPath = '/?owner_me=true&pending_confirmation=true';
  const calls = [];
  const capture = (response) => {
    const url = response.url();
    if (url.startsWith(origin + '/api/v1/ip-records?'))
      calls.push({ query: url.split('?')[1], status: response.status() });
  };
  page.on('response', capture);
  const require = (value, message) => {
    if (!value) throw new Error(message);
  };
  const login = async (password) => {
    await page.getByRole('tab', { name: '系統管理員（本機帳號）' }).click();
    await page.getByLabel('使用者名稱').fill('review-admin');
    await page.getByLabel('密碼', { exact: true }).fill(password);
    await page.getByRole('button', { name: '以系統管理員登入' }).click();
  };
  try {
    await page.context().clearCookies();
    await page.goto(origin + pendingPath);
    await page.waitForURL((url) => url.pathname === '/login');
    require((await returnUrl()) === pendingPath, '登入頁遺失郵件篩選目的地');
    await login('ReviewInitialPassword123');
    await page.waitForURL((url) => url.pathname === '/change-password');
    require((await returnUrl()) === pendingPath, '改密碼頁遺失郵件目的地');
    await page.getByLabel('新密碼（至少 12 個字元）').fill('ReviewChangedPassword123');
    await page.getByLabel('確認新密碼', { exact: true }).fill('ReviewChangedPassword123');
    await page.getByRole('button', { name: '更新密碼並繼續' }).click();
    await page.getByRole('heading', { name: '步驟 1：園區基本資訊' }).waitFor();
    await page.getByRole('button', { name: '儲存並繼續' }).click();
    for (const step of [2, 3, 4]) {
      await page.getByRole('heading', { name: new RegExp('^步驟 ' + step + '：') }).waitFor();
      await page.getByRole('button', { name: '稍後再設定（跳過此步）' }).click();
    }
    await page.getByRole('heading', { name: '步驟 5：檢視並設定納管網段' }).waitFor();
    await page.getByRole('button', { name: '完成精靈，進入系統' }).click();
    await page.waitForURL(origin + pendingPath);
    await page.getByRole('heading', { name: 'IP 總覽', exact: true }).waitFor();
    await page.reload();
    await page.getByRole('heading', { name: 'IP 總覽', exact: true }).waitFor();
    require(page.url() === origin + pendingPath, '重新整理未還原身分或遺失原篩選');
    require(calls.some(
      (call) =>
        call.status === 200 &&
        call.query.split('&').includes('owner_me=true') &&
        call.query.split('&').includes('pending_confirmation=true'),
    ), '真實 API 未收到自己的待確認篩選');

    await page.context().clearCookies();
    await page.goto(origin + '/login?returnUrl=' + encodeURIComponent('//evil.example.test/'));
    await login('ReviewChangedPassword123');
    await page.waitForURL(origin + '/');
    require(page.url().startsWith(origin + '/'), '不可信 returnUrl 導向站外');
    return {
      status: '通過',
      verified: [
        '未登入郵件連結',
        '首次強制改密碼保留目的地',
        '五步精靈可跳過流程',
        '登入後還原篩選',
        '重新整理還原工作階段',
        '拒絕站外 returnUrl',
      ],
      limitation: '未測試交換器／SMTP 連線與含登記資料的破壞性確認流程',
    };
  } finally {
    page.off('response', capture);
  }
};
