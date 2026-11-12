/**
 * 園區對外根網址（public_base_url）的前端驗證，規則對齊後端 NormalizePublicBaseURL：
 * 只接受 HTTP(S) 根網址，不得含帳密、子路徑、查詢參數或片段；空字串代表「未設定」。
 * 前端驗證只負責即時提示，最終仍以後端回應為準。
 */
export function isValidPublicBaseUrl(raw: string): boolean {
  const value = raw.trim();
  if (value === '') {
    return true;
  }
  if (/[\\\r\n\t#?\s]/.test(value)) {
    return false;
  }
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return false;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    return false;
  }
  if (url.hostname === '' || url.username !== '' || url.password !== '') {
    return false;
  }
  if (url.search !== '' || url.hash !== '') {
    return false;
  }
  // 後端要求埠號介於 1 到 65535；URL 解析器已擋掉超過 65535，這裡補上 0
  if (url.port !== '' && Number(url.port) < 1) {
    return false;
  }
  return url.pathname === '' || url.pathname === '/';
}
