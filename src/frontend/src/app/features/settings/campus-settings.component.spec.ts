import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { CampusSettingsComponent } from './campus-settings.component';
import { ToastService } from '../../shared/components/toast.service';

const CAMPUS_URL = '/api/v1/settings/campus';

describe('CampusSettingsComponent（TestBed 元件層級，非瀏覽器 E2E）', () => {
  let fixture: ComponentFixture<CampusSettingsComponent>;
  let httpTesting: HttpTestingController;
  let toast: ToastService;

  const el = () => fixture.nativeElement as HTMLElement;
  const input = (id: string) => el().querySelector<HTMLInputElement>(`#${id}`)!;

  async function type(id: string, value: string) {
    const target = input(id);
    target.value = value;
    target.dispatchEvent(new Event('input'));
    fixture.detectChanges();
    await fixture.whenStable();
  }

  async function init(stored: Record<string, unknown> | 'error') {
    fixture = TestBed.createComponent(CampusSettingsComponent);
    fixture.detectChanges();
    const req = httpTesting.expectOne(CAMPUS_URL);
    if (stored === 'error') {
      req.flush('boom', { status: 500, statusText: 'Server Error' });
    } else {
      req.flush(stored);
    }
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  beforeEach(() => {
    TestBed.configureTestingModule({
      imports: [CampusSettingsComponent],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    httpTesting = TestBed.inject(HttpTestingController);
    toast = TestBed.inject(ToastService);
  });

  afterEach(() => httpTesting.verify());

  it('應顯示後端回傳的公開網址，並說明留空代表停止寄信', async () => {
    await init({
      campus_name: '新竹園區',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com',
    });
    expect(input('publicBaseUrl')).not.toBeNull();
    expect(input('publicBaseUrl').value).toBe('https://ip.example.com');
    expect(el().textContent).toContain('停止寄送');
  });

  it('後端未回傳公開網址欄位時應顯示為空白', async () => {
    await init({ campus_name: '新竹園區', timezone: 'Asia/Taipei' });
    expect(input('publicBaseUrl').value).toBe('');
  });

  it('儲存時應把前後空白去除後的公開網址隨園區資訊一起送出', async () => {
    await init({ campus_name: '新竹園區', timezone: 'Asia/Taipei', public_base_url: '' });
    await type('publicBaseUrl', '  https://ip.example.com/  ');
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    const req = httpTesting.expectOne(CAMPUS_URL);
    expect(req.request.method).toBe('PUT');
    expect(req.request.body).toEqual({
      campus_name: '新竹園區',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com/',
    });
    req.flush({
      campus_name: '新竹園區',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com',
    });
    fixture.detectChanges();
    await fixture.whenStable();
    expect(input('publicBaseUrl').value).toBe('https://ip.example.com');
  });

  it('清空欄位後儲存應送出空字串（明確解除設定）', async () => {
    await init({
      campus_name: '新竹園區',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com',
    });
    await type('publicBaseUrl', '');
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    const req = httpTesting.expectOne(CAMPUS_URL);
    expect(req.request.body.public_base_url).toBe('');
    req.flush({ campus_name: '新竹園區', timezone: 'Asia/Taipei', public_base_url: '' });
  });

  const rejected = [
    'ip.example.com',
    'ftp://ip.example.com',
    'https://ip.example.com/app',
    'https://ip.example.com?a=1',
    'https://ip.example.com#x',
    'https://user:pass@ip.example.com',
  ];
  for (const bad of rejected) {
    it(`不合法網址 ${bad} 不應送出請求，並顯示文字錯誤與 aria 標示`, async () => {
      await init({ campus_name: '新竹園區', timezone: 'Asia/Taipei', public_base_url: '' });
      await type('publicBaseUrl', bad);
      el().querySelector('form')!.dispatchEvent(new Event('submit'));
      httpTesting.expectNone(CAMPUS_URL);
      fixture.detectChanges();

      const alert = el().querySelector('#publicBaseUrlError');
      expect(alert).not.toBeNull();
      expect(alert!.getAttribute('role')).toBe('alert');
      expect(alert!.textContent).toContain('HTTP');
      expect(input('publicBaseUrl').getAttribute('aria-invalid')).toBe('true');
      expect(input('publicBaseUrl').getAttribute('aria-describedby')).toContain(
        'publicBaseUrlError',
      );
    });
  }

  it('改成合法網址後錯誤訊息應消失', async () => {
    await init({ campus_name: '新竹園區', timezone: 'Asia/Taipei', public_base_url: '' });
    await type('publicBaseUrl', 'ip.example.com');
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    fixture.detectChanges();
    expect(el().querySelector('#publicBaseUrlError')).not.toBeNull();

    await type('publicBaseUrl', 'https://ip.example.com');
    expect(el().querySelector('#publicBaseUrlError')).toBeNull();
    expect(input('publicBaseUrl').getAttribute('aria-invalid')).not.toBe('true');
  });

  it('後端拒絕時應以提示顯示後端訊息', async () => {
    const showSpy = spyOn(toast, 'show');
    await init({ campus_name: '新竹園區', timezone: 'Asia/Taipei', public_base_url: '' });
    await type('publicBaseUrl', 'https://ip.example.com');
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    httpTesting
      .expectOne(CAMPUS_URL)
      .flush({ message: '公開網址格式不符' }, { status: 400, statusText: 'Bad Request' });
    expect(showSpy).toHaveBeenCalled();
    expect(showSpy.calls.mostRecent().args[1]).toBe('danger');
    expect(showSpy.calls.mostRecent().args[2]).toBe('公開網址格式不符');
  });

  it('讀取設定失敗時，只要沒編輯公開網址就不可在儲存時送出該欄位（避免誤清空）', async () => {
    await init('error');
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    const req = httpTesting.expectOne(CAMPUS_URL);
    expect('public_base_url' in req.request.body).toBeFalse();
    req.flush({ campus_name: 'Würzburg', timezone: 'Asia/Taipei' });
  });
});
