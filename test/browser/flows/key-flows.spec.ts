import { expect, test, type Page } from '@playwright/test';
import { PageWatch, env, loginLocal, loginOrg, logout, receivedMails, setSwitchHosts } from './support';

// 六組關鍵流程共用同一套系統、依序執行；前面的流程失敗時，後面的沒有意義，直接略過。
test.describe.configure({ mode: 'serial' });

let page: Page;
let watch: PageWatch;

test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext();
  page = await context.newPage();
  watch = new PageWatch(page);
});

test.afterEach(() => {
  watch.assertClean();
});

test.afterAll(async () => {
  await page.context().close();
});

test('登入：未登入時導向登入頁，初次登入強制改密碼', async () => {
  // 尚未登入就開啟需要登入的頁面：還原工作階段得到 401，導向登入頁並記住原本的目的地
  watch.allow(401, /^\/api\/v1\/auth\/me$/);
  await page.goto('/subnets');
  await expect(page).toHaveURL(/\/login\?returnUrl=%2Fsubnets$/);
  await expect(page.getByRole('heading', { name: '登入 Würzburg' })).toBeVisible();

  // 登入卡片加寬 10%，桌面頁籤維持單行，窄螢幕仍不得水平溢出。
  expect((await page.locator('.login-card').boundingBox())!.width).toBe(484);
  const orgLabel = page.getByRole('tab', { name: '組織帳號（AD / RADIUS）' }).locator('span');
  expect(await orgLabel.evaluate((label) => {
    const range = document.createRange();
    range.selectNodeContents(label);
    return range.getClientRects().length;
  })).toBe(1);
  await page.screenshot({ path: test.info().outputPath('login-width-desktop.png') });
  const viewport = page.viewportSize()!;
  await page.setViewportSize({ width: 375, height: 812 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: test.info().outputPath('login-width-mobile.png') });
  await page.setViewportSize(viewport);

  // 錯誤的密碼：留在登入頁並顯示原因
  watch.allow(401, /^\/api\/v1\/auth\/login\/local$/);
  await loginLocal(page, env.admin.user, 'NotTheAdminPassword#1');
  await expect(page.getByText('帳號或密碼錯誤')).toBeVisible();

  // 初始帳密登入：強制先改密碼，目的地一路保留
  await loginLocal(page, env.admin.user, env.admin.initialPw);
  await expect(page).toHaveURL(/\/change-password\?returnUrl=%2Fsubnets$/);
  await expect(page.getByRole('heading', { name: '首次登入強制變更密碼' })).toBeVisible();

  await page.getByLabel('新密碼（至少 12 個字元）').fill(env.admin.pw);
  await page.getByLabel('確認新密碼', { exact: true }).fill('DoesNotMatch#2026');
  await page.getByRole('button', { name: '更新密碼並繼續' }).click();
  await expect(page.getByText('兩次輸入的新密碼不相符')).toBeVisible();

  await page.getByLabel('確認新密碼', { exact: true }).fill(env.admin.pw);
  await page.getByRole('button', { name: '更新密碼並繼續' }).click();

  // 全新安裝：改完密碼後開啟設定精靈
  await expect(page.getByRole('heading', { name: '初始設定精靈' })).toBeVisible();
});

test('設定精靈：走完五個步驟', async () => {
  const wizard = page.getByRole('dialog');

  // 步驟 1：園區資訊。公開網址預先帶入目前使用的網址
  await expect(wizard.getByRole('heading', { name: '步驟 1：園區基本資訊' })).toBeVisible();
  await expect(wizard.getByLabel('系統公開網址')).toHaveValue(env.baseUrl);
  await wizard.getByLabel('園區顯示名稱 *').fill('Würzburg 研發園區');
  await wizard.getByRole('button', { name: '儲存並繼續' }).click();

  // 步驟 2：驗證來源，以實際的組織帳號測試登入
  await expect(wizard.getByRole('heading', { name: '步驟 2：一般使用者驗證來源' })).toBeVisible();
  await wizard.getByLabel('AD 伺服器網址 *').fill(env.ldap.url);
  await wizard.getByLabel('啟用 TLS 加密連線（強烈建議生產環境保持勾選）').uncheck();
  await wizard.getByLabel('查詢帳號 DN').fill(env.ldap.bindDn);
  await wizard.getByLabel('查詢密碼').fill(env.ldap.bindPw);
  await wizard.getByLabel('搜尋 Base DN *').fill(env.ldap.baseDn);
  await wizard.getByPlaceholder('測試帳號').fill(env.user01.user);
  await wizard.getByPlaceholder('測試密碼').fill(env.user01.pw);
  await wizard.getByRole('button', { name: '儲存並測試登入' }).click();
  await expect(wizard.getByText('驗證測試成功')).toBeVisible();
  await wizard.getByRole('button', { name: '儲存並繼續' }).click();

  // 步驟 3：郵件通知，寄一封測試信並確認郵件伺服器真的收到
  await expect(wizard.getByRole('heading', { name: '步驟 3：郵件通知設定 (SMTP)' })).toBeVisible();
  await wizard.getByLabel('SMTP 伺服器主機 *').fill(env.smtp.host);
  await wizard.getByLabel('埠號').fill(env.smtp.port);
  await wizard.getByLabel('加密模式').selectOption({ label: '無加密' });
  await wizard.getByLabel('認證帳號').fill(env.smtp.user);
  await wizard.getByLabel('認證密碼').fill(env.smtp.pw);
  await wizard.getByLabel('寄件人信箱 (From) *').fill(env.senderMailbox);
  await wizard.getByLabel('系統管理員收件清單（多筆請用逗號隔開）*').fill(env.adminMailbox);
  await wizard.getByPlaceholder('收件信箱').fill(env.adminMailbox);
  await wizard.getByRole('button', { name: '儲存並寄出測試信' }).click();
  await expect(wizard.locator('.test-feedback.success')).toBeVisible();
  await expect
    .poll(async () => (await receivedMails()).filter((m) => m.To.some((t) => t.Address === env.adminMailbox)).length)
    .toBe(1);
  await wizard.getByRole('button', { name: '儲存並繼續' }).click();

  // 步驟 4：第一台交換器，以真正的 SNMP 測試讀取
  await expect(wizard.getByRole('heading', { name: '步驟 4：新增第一台 ARP 來源交換器' })).toBeVisible();
  await wizard.getByLabel('交換器名稱 *').fill('core-iosxe-01');
  await wizard.getByLabel('管理 IP 位址 *').fill(env.snmp.host);
  await wizard.getByLabel('Community 字串 *').fill(env.snmp.community);
  await wizard.getByRole('button', { name: '儲存並測試讀取' }).click();
  await expect(wizard.getByText('讀取成功，共讀到 5 筆 ARP、2 個介面位址')).toBeVisible();
  await wizard.getByRole('button', { name: '儲存並繼續' }).click();

  // 步驟 5：同步結束後列出從介面位址表發現的網段，把兩個都設為納管
  await expect(wizard.getByRole('heading', { name: '步驟 5：檢視並設定納管網段' })).toBeVisible();
  for (const cidr of ['10.10.10.0/24', '10.10.30.0/24']) {
    const row = wizard.locator('.subnet-row', { hasText: cidr });
    await expect(row).toBeVisible();
    await row.getByRole('combobox').selectOption({ label: '納管 (managed)' });
    await expect(row.getByRole('combobox')).toHaveValue('managed');
  }
  // 納管後再同步一次，IP 紀錄才會建立
  await wizard.getByRole('button', { name: '觸發同步探索網段' }).click();
  await expect(wizard.locator('.subnet-row', { hasText: '10.10.10.0/24' })).toContainText('IP 數：4，在線：4');
  await expect(wizard.locator('.subnet-row', { hasText: '10.10.30.0/24' })).toContainText('IP 數：1，在線：1');

  await wizard.getByRole('button', { name: '完成精靈，進入系統' }).click();
  await expect(wizard).toBeHidden();
  await expect(page.getByText('初始設定精靈已完成！')).toBeVisible();

  // 登入前的目的地是網段頁，改完密碼並走完精靈後要回到那裡
  await expect(page.getByRole('heading', { name: '網段管理' })).toBeVisible();
  await expect(page.getByText('10.10.10.0/24')).toBeVisible();
  await expect(page.getByText('已記錄 4 筆')).toBeVisible();
});

test('外殼：側欄可折疊並記住偏好，右上操作不再放在側欄底部', async () => {
  await page.goto('/');
  const sidebar = page.locator('.sidebar');
  await expect(sidebar).toBeVisible();
  expect((await sidebar.boundingBox())!.width).toBe(264);
  const workspaceBefore = (await page.locator('.workspace').boundingBox())!.width;
  await expect(page.getByRole('button', { name: '收合側欄', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '收合側欄', exact: true }).click();
  await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(64);
  expect((await page.locator('.workspace').boundingBox())!.width).toBeGreaterThan(workspaceBefore);
  await expect(sidebar.getByRole('link', { name: '元件展示' })).toHaveCount(0);
  await expect(sidebar.getByRole('button', { name: '登出', exact: true })).toHaveCount(0);
  await expect(sidebar.getByRole('button', { name: '切換外觀主題' })).toHaveCount(0);
  const theme = page.getByRole('button', { name: '切換外觀主題' });
  const logout = page.getByRole('button', { name: '登出', exact: true });
  expect((await theme.boundingBox())!.x).toBeLessThan((await logout.boundingBox())!.x);

  // 滑鼠與鍵盤都能取得完整名稱，目前頁面與側欄寬度仍可辨識。
  const homeLink = sidebar.getByRole('link', { name: '首頁工作臺', exact: true });
  await expect(homeLink).toHaveAttribute('aria-current', 'page');
  expect(await sidebar.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
  await homeLink.hover();
  await expect(page.getByRole('tooltip')).toHaveText('首頁工作臺');
  await page.mouse.move(700, 0);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  // 鍵盤焦點的提示也不可被側欄捲動區裁切。
  await homeLink.focus();
  await expect(page.getByRole('tooltip')).toHaveText('首頁工作臺');
  const hint = (await page.getByRole('tooltip').boundingBox())!;
  expect(hint.x).toBeGreaterThanOrEqual(64);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  await sidebar.locator('.user-card').hover();
  await expect(page.getByRole('tooltip')).toContainText('系統管理員');
  await page.mouse.move(700, 0);
  await expect(page.getByRole('tooltip')).toHaveCount(0);
  await sidebar.getByRole('group', { name: '系統管理員，系統管理員', exact: true }).focus();
  await expect(page.getByRole('tooltip')).toContainText('系統管理員');
  await page.keyboard.press('Escape');
  await page.screenshot({ animations: 'disabled', path: test.info().outputPath('shell-collapsed-light.png') });

  await theme.click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.screenshot({ animations: 'disabled', path: test.info().outputPath('shell-collapsed-dark.png') });
  await page.reload();
  await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(64);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

  // 手機抽屜使用完整寬度，不受桌面折疊偏好影響；關閉後焦點回到選單按鈕。
  const viewport = page.viewportSize()!;
  await page.setViewportSize({ width: 375, height: 812 });
  const menu = page.getByRole('button', { name: '切換導覽側欄', exact: true });
  await expect(sidebar).toHaveCount(0);
  await expect(menu).toBeVisible();
  expect((await theme.boundingBox())!.x).toBeGreaterThan((await menu.boundingBox())!.x + 40);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ animations: 'disabled', path: test.info().outputPath('shell-mobile-dark.png') });
  await menu.click();
  await expect(sidebar).toBeVisible();
  expect((await sidebar.boundingBox())!.width).toBe(264);
  await expect.poll(async () => (await sidebar.boundingBox())!.x).toBe(0);
  await expect(sidebar.locator('.nav-label').first()).toBeVisible();
  const closeMenu = sidebar.getByRole('button', { name: '關閉導覽側欄', exact: true });
  await expect(closeMenu).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  await expect(sidebar.getByRole('link', { name: '系統設定', exact: true })).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(closeMenu).toBeFocused();
  await page.screenshot({ animations: 'disabled', path: test.info().outputPath('shell-mobile-open.png') });
  await page.keyboard.press('Escape');
  await expect(sidebar).toHaveCount(0);
  await expect(menu).toBeFocused();
  await menu.click();
  await sidebar.getByRole('link', { name: /^網段/ }).click();
  await expect(sidebar).toHaveCount(0);
  await expect(menu).toBeFocused();
  await menu.click();
  await closeMenu.click();
  await expect(menu).toBeFocused();
  await menu.click();
  await page.locator('.mobile-backdrop').click({ position: { x: 350, y: 300 } });
  await expect(menu).toBeFocused();
  await page.setViewportSize({ width: 900, height: 812 });
  await expect(menu).toBeVisible();
  await expect(sidebar).toHaveCount(0);
  await page.setViewportSize({ width: 901, height: 812 });
  await expect(menu).toBeHidden();
  await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(64);
  await page.setViewportSize(viewport);
  await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(64);
  await page.getByRole('button', { name: '展開側欄', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(264);
  await page.screenshot({ animations: 'disabled', path: test.info().outputPath('shell-expanded-dark.png') });
  await theme.click();
  await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'dark');
  await page.screenshot({ animations: 'disabled', path: test.info().outputPath('shell-expanded-light.png') });
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.getByRole('button', { name: '收合側欄', exact: true }).click();
  await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(64);
  expect(await page.locator('.app-shell').evaluate((element) =>
    parseFloat(getComputedStyle(element).transitionDuration),
  )).toBeLessThanOrEqual(0.001);
  await page.getByRole('button', { name: '展開側欄', exact: true }).click();
  await expect.poll(async () => (await sidebar.boundingBox())!.width).toBe(264);
  await page.emulateMedia({ reducedMotion: 'no-preference' });
});

test('郵件設定：未另按儲存時，測試寄信仍採用新輸入的密碼', async () => {
  await page.goto('/settings?tab=mail');
  const settings = page.locator('app-mail-settings');
  const password = settings.getByLabel('認證密碼', { exact: true });
  await expect(settings.getByLabel('SMTP 伺服器主機 *')).toHaveValue(env.smtp.host);

  // 先保存錯誤的舊密碼，再於畫面改正；測試必須先儲存新值才有辦法通過 SMTP 驗證。
  await password.fill('BrowserWrongSmtp#2026');
  await settings.getByRole('button', { name: '儲存郵件設定', exact: true }).click();
  await expect(password).toHaveValue('');
  await password.fill(env.smtp.pw);
  await settings.getByPlaceholder('請輸入測試收件人信箱').fill(env.adminMailbox);

  const mailCount = async () =>
    (await receivedMails()).filter((m) => m.To.some((t) => t.Address === env.adminMailbox)).length;
  const before = await mailCount();
  await settings.getByRole('button', { name: /寄.*測試信/ }).click();
  await expect(settings.locator('.test-result.success')).toBeVisible({ timeout: 12000 });
  await expect.poll(mailCount).toBe(before + 1);
  await expect(password).toHaveValue('');

  // 重新載入後密碼欄保持空白，再測試應沿用剛才儲存的正確密碼。
  await page.reload();
  await expect(settings.getByLabel('SMTP 伺服器主機 *')).toHaveValue(env.smtp.host);
  await expect(password).toHaveValue('');
  await settings.getByPlaceholder('請輸入測試收件人信箱').fill(env.adminMailbox);
  await settings.getByRole('button', { name: /寄.*測試信/ }).click();
  await expect(settings.locator('.test-result.success')).toBeVisible();
  await expect.poll(mailCount).toBe(before + 2);
});

test('工作臺：未登記位址以文字標示狀態', async () => {
  await page.getByRole('link', { name: '首頁工作臺' }).click();
  await expect(page.getByRole('heading', { name: 'IP 總覽' })).toBeVisible();
  for (const ip of ['10.10.10.50', '10.10.10.51', '10.10.10.52', '10.10.10.53', '10.10.30.10']) {
    await expect(page.getByRole('row', { name: new RegExp(ip.replaceAll('.', '\\.')) })).toContainText('未登記');
  }
});

test('登入：登出後以組織帳號登入', async () => {
  await logout(page);
  await loginOrg(page, env.user01.user, env.user01.pw);
  await expect(page.getByRole('heading', { name: 'IP 總覽' })).toBeVisible();
  await expect(page.locator('.user-card')).toContainText('王工程師');
  await expect(page.locator('.user-card')).toContainText('一般使用者');
});

async function openIp(ip: string): Promise<void> {
  await page.getByRole('row', { name: new RegExp(ip.replaceAll('.', '\\.')) }).click();
  await expect(page.getByText('狀態：')).toBeVisible();
}

test('登記：單筆與批次', async () => {
  await openIp('10.10.10.50');
  await page.getByLabel('擁有者信箱（必填，到期催認寄送至此）').fill(env.user01.mail);
  await page.getByLabel('裝置類型（自填）').fill('虛擬伺服器');
  await page.getByRole('button', { name: '完成登記' }).click();
  await expect(page.getByText('登記成功')).toBeVisible();
  await page.keyboard.press('Escape');

  await page.getByRole('checkbox', { name: '選取 10.10.10.52' }).check();
  await page.getByRole('checkbox', { name: '選取 10.10.10.53' }).check();
  await page.getByRole('button', { name: '批次登記' }).click();
  const batch = page.getByRole('dialog', { name: '批次登記未登記之 IP 紀錄' });
  await batch.getByLabel('擁有者信箱（必填）').fill(env.user01.mail);
  await batch.getByRole('button', { name: '執行批次登記' }).click();
  await expect(page.getByText('批次登記完成')).toBeVisible();

  for (const ip of ['10.10.10.50', '10.10.10.52', '10.10.10.53']) {
    await expect(page.getByRole('row', { name: new RegExp(ip.replaceAll('.', '\\.')) })).toContainText('已登記');
  }
});

test('批次重新確認', async () => {
  await page.getByRole('checkbox', { name: '選取 10.10.10.50' }).check();
  await page.getByRole('checkbox', { name: '選取 10.10.10.52' }).check();
  await page.getByRole('button', { name: '批次重新確認' }).click();
  await expect(page.getByText('批次作業完成')).toBeVisible();
});

test('裁決衝突', async () => {
  await setSwitchHosts({ set: { '10.10.10.50': '00:0c:29:99:99:50' } });
  await logout(page);
  await loginLocal(page, env.admin.user, env.admin.pw);
  await page.getByRole('link', { name: '交換器' }).click();
  await page.getByRole('button', { name: '立即同步' }).click();
  await expect(page.getByText('已觸發立即同步')).toBeVisible();
  await expect(page.getByText('同步中')).toBeHidden({ timeout: 30_000 });

  await page.getByRole('link', { name: '衝突' }).click();
  await expect(page.getByRole('heading', { name: '衝突' })).toBeVisible();
  await page.getByRole('row', { name: /10\.10\.10\.50/ }).click();
  await page.getByText('採信新的 MAC，保留既有登記（擁有者更換設備）').click();
  await page.getByLabel('選擇採信之 MAC').selectOption('00:0c:29:99:99:50');
  await page.getByLabel('處置說明（選填）').fill('確認更換網卡');
  await page.getByRole('button', { name: '套用裁決' }).click();
  await expect(page.getByText('衝突裁決已套用')).toBeVisible();
});

test('把網段改為不納管', async () => {
  await page.getByRole('link', { name: '網段' }).click();
  await page.locator('article', { hasText: '10.10.30.0/24' }).click();
  await page.getByRole('button', { name: '設為不納管' }).click();
  const confirm = page.getByRole('alertdialog');
  await expect(confirm.getByRole('heading', { name: '確認將網段改為不納管' })).toBeVisible();
  await expect(confirm.getByText(/將直接刪除 1 筆 IP 紀錄/)).toBeVisible();
  await confirm.getByRole('button', { name: '確認刪除並設為不納管' }).click();
  await expect(page.locator('article', { hasText: '10.10.30.0/24' })).toContainText('不納管');
  await page.getByRole('button', { name: '關閉視窗' }).click();
  await page.getByRole('link', { name: '首頁工作臺' }).click();
  await expect(page.getByRole('cell', { name: '10.10.30.10' })).toHaveCount(0);
});
