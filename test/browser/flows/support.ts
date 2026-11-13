import { expect, type Page, type Response } from '@playwright/test';

/** 隔離環境的位址與帳密，由 src/backend/test/browser_env_test.go 以環境變數傳入。 */
function required(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`缺少環境變數 ${name}：請以 test/browser/run.sh 執行`);
  }
  return value;
}

export const env = {
  baseUrl: required('WZ_BASE_URL'),
  controlUrl: required('WZ_CONTROL_URL'),
  mailApiUrl: required('WZ_MAIL_API_URL'),
  admin: { user: required('WZ_ADMIN_USER'), initialPw: required('WZ_ADMIN_INITIAL_PW'), pw: required('WZ_ADMIN_PW') },
  adminMailbox: required('WZ_ADMIN_MAILBOX'),
  senderMailbox: required('WZ_SENDER_MAILBOX'),
  ldap: {
    url: required('WZ_LDAP_URL'),
    bindDn: required('WZ_LDAP_BIND_DN'),
    bindPw: required('WZ_LDAP_BIND_PW'),
    baseDn: required('WZ_LDAP_BASE_DN'),
    allowedGroup: required('WZ_LDAP_ALLOWED_GROUP'),
  },
  smtp: {
    host: required('WZ_SMTP_HOST'),
    port: required('WZ_SMTP_PORT'),
    user: required('WZ_SMTP_USER'),
    pw: required('WZ_SMTP_PW'),
  },
  snmp: { host: required('WZ_SNMP_HOST'), community: required('WZ_SNMP_COMMUNITY') },
  user01: { user: required('WZ_USER01'), pw: required('WZ_USER01_PW'), mail: required('WZ_USER01_MAIL') },
  user02: { user: required('WZ_USER02'), pw: required('WZ_USER02_PW'), mail: required('WZ_USER02_MAIL') },
};

/**
 * 監看頁面在流程中出現的問題：未預期的主控台錯誤、未捕捉的例外、失敗的 API 請求。
 * 流程刻意觸發的錯誤（例如尚未登入時的 401）要先以 allow 宣告，其餘一律讓流程失敗。
 */
export class PageWatch {
  private readonly problems: string[] = [];
  private readonly allowed: { status: number; path: RegExp }[] = [];

  constructor(page: Page) {
    page.on('pageerror', (err) => this.problems.push(`未捕捉的例外：${err.message}`));
    page.on('console', (msg) => {
      // 失敗的請求會同時在主控台留下「Failed to load resource」，那部分由 response 事件判斷
      if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource')) {
        this.problems.push(`主控台錯誤：${msg.text()}`);
      }
    });
    page.on('response', (res: Response) => {
      const url = new URL(res.url());
      if (res.status() < 400 || !url.pathname.startsWith('/api/')) {
        return;
      }
      if (this.allowed.some((a) => a.status === res.status() && a.path.test(url.pathname))) {
        return;
      }
      this.problems.push(`失敗的 API 請求：${res.request().method()} ${url.pathname} → ${res.status()}`);
    });
  }

  /** 宣告流程中預期會出現的失敗回應。 */
  allow(status: number, path: RegExp): void {
    this.allowed.push({ status, path });
  }

  assertClean(): void {
    expect(this.problems, '流程中不應有未預期的主控台錯誤或失敗的 API 請求').toEqual([]);
  }
}

export async function loginLocal(page: Page, username: string, password: string): Promise<void> {
  await page.getByRole('tab', { name: '系統管理員（本機帳號）' }).click();
  await page.getByLabel('使用者名稱').fill(username);
  await page.getByLabel('密碼', { exact: true }).fill(password);
  await page.getByRole('button', { name: '以系統管理員登入' }).click();
}

export async function loginOrg(page: Page, username: string, password: string): Promise<void> {
  await page.getByRole('tab', { name: '組織帳號（AD / RADIUS）' }).click();
  await page.getByLabel('使用者名稱').fill(username);
  await page.getByLabel('密碼', { exact: true }).fill(password);
  await page.getByRole('button', { name: '以組織帳號登入' }).click();
}

export async function logout(page: Page): Promise<void> {
  await page.getByRole('button', { name: '登出' }).click();
  await expect(page).toHaveURL(/\/login$/);
}

/** 讀取郵件模擬伺服器目前收到的信（由新到舊）。 */
export async function receivedMails(): Promise<{ Subject: string; To: { Address: string }[] }[]> {
  const res = await fetch(`${env.mailApiUrl}/api/v1/messages?limit=100`);
  const body = (await res.json()) as { messages: { Subject: string; To: { Address: string }[] }[] };
  return body.messages;
}

/** 改變模擬交換器回報的內容：set 是 IP → MAC（新增或更換），remove 是不再回報的 IP。 */
export async function setSwitchHosts(change: { set?: Record<string, string>; remove?: string[] }): Promise<void> {
  const res = await fetch(`${env.controlUrl}/snmp/hosts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(change),
  });
  if (res.status !== 204) {
    throw new Error(`更新模擬交換器失敗：${res.status} ${await res.text()}`);
  }
}
