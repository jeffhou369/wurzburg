import { buildLoginReturnQuery, sanitizeReturnUrl } from './safe-return-url';

describe('sanitizeReturnUrl', () => {
  const safeUrls = [
    '/',
    '/subnets',
    '/subnets?search=a%20b&page=2',
    '/?owner_me=true&pending_confirmation=true',
    '/?presence=online&registration=unregistered',
    '/conflicts#top',
  ];

  safeUrls.forEach((url) => {
    it(`應保留站內相對路徑 ${url}`, () => {
      expect(sanitizeReturnUrl(url)).toBe(url);
    });
  });

  const unsafeUrls: [string, unknown][] = [
    ['絕對 https 網址', 'https://evil.example/'],
    ['絕對 http 網址', 'http://evil.example/path'],
    ['protocol-relative 網址', '//evil.example'],
    ['三個斜線', '///evil.example'],
    ['反斜線開頭', '\\evil.example'],
    ['雙反斜線開頭', '\\\\evil.example'],
    ['斜線加反斜線', '/\\evil.example'],
    ['反斜線加斜線', '\\/evil.example'],
    ['路徑中夾帶反斜線', '/subnets\\..\\..'],
    ['javascript 協定', 'javascript:alert(1)'],
    ['data 協定', 'data:text/html,hi'],
    ['缺少開頭斜線之主機名稱', 'evil.example'],
    ['空字串', ''],
    ['僅空白', '   '],
    ['斜線後夾帶 Tab（瀏覽器會移除而變成 //）', '/\t/evil.example'],
    ['斜線後夾帶換行', '/\n/evil.example'],
    ['含控制字元', '/subnets\u0000'],
    ['編碼後的反斜線', '/%5Cevil.example'],
    ['編碼後的雙斜線', '/%2F%2Fevil.example'],
    ['小寫編碼後的雙斜線', '/%2f%2fevil.example'],
    ['null', null],
    ['undefined', undefined],
    ['數字', 123],
    ['陣列', ['/subnets']],
    ['物件', { url: '/subnets' }],
    ['過長字串', '/' + 'a'.repeat(3000)],
  ];

  unsafeUrls.forEach(([label, value]) => {
    it(`應拒絕${label}並回到預設首頁`, () => {
      expect(sanitizeReturnUrl(value)).toBe('/');
    });
  });

  it('不安全的值可自訂備援目的地', () => {
    expect(sanitizeReturnUrl('//evil.example', '/subnets')).toBe('/subnets');
  });

  it('不應導回登入或改密碼頁以免循環', () => {
    expect(sanitizeReturnUrl('/login')).toBe('/');
    expect(sanitizeReturnUrl('/login?returnUrl=%2Fsubnets')).toBe('/');
    expect(sanitizeReturnUrl('/change-password')).toBe('/');
    expect(sanitizeReturnUrl('/change-password?returnUrl=%2F')).toBe('/');
    expect(sanitizeReturnUrl('/loginx')).toBe('/loginx');
  });
});

describe('buildLoginReturnQuery', () => {
  it('應回傳以安全 returnUrl 組成的查詢參數', () => {
    expect(buildLoginReturnQuery('/subnets?x=1')).toEqual({ returnUrl: '/subnets?x=1' });
  });

  it('不安全之目的地應退回首頁而非原值', () => {
    expect(buildLoginReturnQuery('https://evil.example')).toEqual({ returnUrl: '/' });
  });
});
