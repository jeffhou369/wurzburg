/**
 * 登入後導向目的地（returnUrl）的安全檢查。
 *
 * 只接受站內相對路徑（以單一斜線開頭），避免開放式重新導向（open redirect）：
 * 拒絕絕對網址、protocol-relative（//host）、反斜線、控制字元與其編碼變體。
 */

const MAX_RETURN_URL_LENGTH = 2048;

/** 這些頁面本身不應成為登入後的目的地，否則會造成循環。 */
const AUTH_PAGES = ['/login', '/change-password'];

function isAuthPage(url: string): boolean {
  const path = url.split(/[?#]/, 1)[0];
  return AUTH_PAGES.some((page) => path === page || path.startsWith(`${page}/`));
}

function hasControlCharacter(value: string): boolean {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code <= 0x1f || code === 0x7f) {
      return true;
    }
  }
  return false;
}

/**
 * 回傳可安全導向的站內網址；不安全或無效時回傳 fallback。
 */
export function sanitizeReturnUrl(raw: unknown, fallback = '/'): string {
  if (typeof raw !== 'string') {
    return fallback;
  }
  const value = raw;
  if (value.length === 0 || value.length > MAX_RETURN_URL_LENGTH) {
    return fallback;
  }
  if (value.trim() !== value || hasControlCharacter(value)) {
    return fallback;
  }
  if (value.includes('\\')) {
    return fallback;
  }
  if (!value.startsWith('/') || value.startsWith('//')) {
    return fallback;
  }

  // 編碼後的變體（%5C、%2F%2F）同樣視為可疑，一律拒絕。
  let decoded: string;
  try {
    decoded = decodeURIComponent(value);
  } catch {
    return fallback;
  }
  if (decoded.includes('\\') || decoded.startsWith('//') || hasControlCharacter(decoded)) {
    return fallback;
  }

  // 最後以 URL 解析確認不會跳到其他來源。
  try {
    const base = 'http://return-url.invalid';
    if (new URL(value, base).origin !== base) {
      return fallback;
    }
  } catch {
    return fallback;
  }

  if (isAuthPage(value)) {
    return fallback;
  }
  return value;
}

/** 組成導向登入頁用的查詢參數；目的地一律先經過安全檢查。 */
export function buildLoginReturnQuery(destination: string): { returnUrl: string } {
  return { returnUrl: sanitizeReturnUrl(destination) };
}
