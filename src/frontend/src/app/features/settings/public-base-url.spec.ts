import { isValidPublicBaseUrl } from './public-base-url';

describe('isValidPublicBaseUrl（園區對外根網址驗證，對齊後端 NormalizePublicBaseURL）', () => {
  const valid = [
    '',
    '   ',
    'https://ip.example.com',
    'https://ip.example.com/',
    'http://10.0.0.5:8080',
    '  https://ip.example.com  ',
    'HTTPS://IP.EXAMPLE.COM',
    'http://[2001:db8::1]:8443',
  ];
  for (const value of valid) {
    it(`接受 ${JSON.stringify(value)}`, () => {
      expect(isValidPublicBaseUrl(value)).toBeTrue();
    });
  }

  const invalid: Record<string, string> = {
    相對路徑: '/workbench',
    缺少通訊協定: 'ip.example.com',
    協定相對網址: '//ip.example.com',
    '非 HTTP(S) 協定 ftp': 'ftp://ip.example.com',
    'javascript 協定': 'javascript:alert(1)',
    含子路徑: 'https://ip.example.com/app',
    含查詢參數: 'https://ip.example.com?a=1',
    只有問號: 'https://ip.example.com?',
    含片段: 'https://ip.example.com#top',
    只有井字號: 'https://ip.example.com#',
    含帳密: 'https://user:pass@ip.example.com',
    只含使用者名稱: 'https://user@ip.example.com',
    含反斜線: 'https://ip.example.com\\app',
    主機為空: 'https://',
    埠號超出範圍: 'https://ip.example.com:70000',
    埠號為零: 'https://ip.example.com:0',
    內嵌換行: 'https://ip.example.com\n/evil',
    內嵌定位字元: 'https://ip.exa\tmple.com',
    內含空白: 'https://ip example.com',
  };
  for (const [label, value] of Object.entries(invalid)) {
    it(`拒絕${label}：${JSON.stringify(value)}`, () => {
      expect(isValidPublicBaseUrl(value)).toBeFalse();
    });
  }
});
