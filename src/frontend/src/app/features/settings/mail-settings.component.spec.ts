import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { MailSettingsComponent } from './mail-settings.component';
import { ToastService } from '../../shared/components/toast.service';

const SETTINGS_URL = '/api/v1/settings/mail';
const TEST_URL = '/api/v1/settings/mail/test';
const stored = {
  smtp_host: 'smtp.example.com',
  smtp_port: 587,
  smtp_tls_mode: 'starttls',
  smtp_username: 'mailer@example.com',
  has_password: true,
  sender_address: 'mailer@example.com',
  admin_recipients: ['admin@example.com'],
  weekly_summary_cron: '0 9 * * 1',
};

describe('MailSettingsComponent（元件層級輔助，非 SMTP 端到端驗證）', () => {
  let fixture: ComponentFixture<MailSettingsComponent>;
  let http: HttpTestingController;
  const el = () => fixture.nativeElement as HTMLElement;
  const password = () => el().querySelector<HTMLInputElement>('#smtpPass')!;
  const sendButton = () => el().querySelector<HTMLButtonElement>('.test-form button')!;
  const saveButton = () => el().querySelector<HTMLButtonElement>('button[type="submit"]')!;

  async function settle() {
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  async function type(selector: string, value: string) {
    const input = el().querySelector<HTMLInputElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    await settle();
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [MailSettingsComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: ToastService, useValue: { show: jasmine.createSpy('show') } },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    fixture = TestBed.createComponent(MailSettingsComponent);
    fixture.detectChanges();
    http.expectOne(SETTINGS_URL).flush({ ...stored });
    await settle();
    await type('.test-form input', 'recipient@example.com');
  });

  afterEach(() => http.verify());

  it('先儲存畫面上的新設定與原樣密碼，成功後才寄測試信，整段流程禁止重複操作', async () => {
    await type('#smtpHost', 'smtp.changed.example.com');
    await type('#smtpPass', ' SmtpPassword#2026 ');
    sendButton().click();
    // 在畫面尚未重新渲染前連點及提交表單，也不能建立第二條操作鏈。
    sendButton().click();
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    fixture.detectChanges();

    const save = http.expectOne(SETTINGS_URL);
    expect(save.request.method).toBe('PUT');
    expect(save.request.body.smtp_host).toBe('smtp.changed.example.com');
    expect(save.request.body.smtp_password).toBe(' SmtpPassword#2026 ');
    http.expectNone(TEST_URL);
    expect(password().matches(':disabled')).toBeTrue();
    expect(saveButton().disabled).toBeTrue();
    expect(sendButton().disabled).toBeTrue();

    save.flush({ ...stored, smtp_host: 'smtp.changed.example.com' });
    const send = http.expectOne(TEST_URL);
    expect(send.request.method).toBe('POST');
    expect(send.request.body).toEqual({ recipient: 'recipient@example.com' });
    await settle();
    expect(password().value).toBe('');
    expect(sendButton().disabled).toBeTrue();
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    http.expectNone(SETTINGS_URL);
    http.expectNone(TEST_URL);

    send.flush({ success: true, message: '測試信已成功寄出' });
    await settle();
    expect(el().querySelector('[role="status"]')!.textContent).toContain('測試信已成功寄出');
    expect(password().matches(':disabled')).toBeFalse();
    expect(saveButton().disabled).toBeFalse();
    expect(sendButton().disabled).toBeFalse();
  });

  it('密碼留空時不覆寫已儲存的密碼，收件人前後空白不送出', async () => {
    await type('.test-form input', '  recipient@example.com  ');
    sendButton().click();
    const save = http.expectOne(SETTINGS_URL);
    expect('smtp_password' in save.request.body).toBeFalse();
    save.flush({ ...stored });
    const send = http.expectOne(TEST_URL);
    expect(send.request.body).toEqual({ recipient: 'recipient@example.com' });
    send.flush({ success: true, message: '測試信已成功寄出' });
    await settle();
    expect(sendButton().disabled).toBeFalse();
  });

  it('設定儲存失敗時不寄信，保留新密碼並允許重試', async () => {
    await type('#smtpPass', 'RetrySmtpPassword#2026');
    sendButton().click();
    http
      .expectOne(SETTINGS_URL)
      .flush({ message: '設定儲存暫時失敗' }, { status: 500, statusText: 'Server Error' });
    http.expectNone(TEST_URL);
    await settle();
    expect(el().querySelector('[role="status"]')!.textContent).toContain(
      '郵件設定儲存失敗，尚未寄出測試信：設定儲存暫時失敗',
    );
    expect(password().value).toBe('RetrySmtpPassword#2026');
    expect(sendButton().disabled).toBeFalse();
    expect(saveButton().disabled).toBeFalse();

    sendButton().click();
    expect(el().querySelector('[role="status"]')?.textContent).not.toContain('寄送成功');
    const retry = http.expectOne(SETTINGS_URL);
    expect(retry.request.body.smtp_password).toBe('RetrySmtpPassword#2026');
    retry.flush({ ...stored });
    http.expectOne(TEST_URL).flush({ success: true, message: '測試信已成功寄出' });
    await settle();
    expect(password().value).toBe('');
  });

  for (const failure of ['response', 'http'] as const) {
    it(`設定已儲存但測試寄信失敗（${failure}）時說明儲存結果，重試沿用新密碼`, async () => {
      await type('#smtpPass', 'SavedSmtpPassword#2026');
      sendButton().click();
      http.expectOne(SETTINGS_URL).flush({ ...stored });
      const send = http.expectOne(TEST_URL);
      if (failure === 'response') {
        send.flush({ success: false, message: 'SMTP 認證失敗' });
      } else {
        send.flush({ message: 'SMTP 認證失敗' }, { status: 502, statusText: 'Bad Gateway' });
      }
      await settle();
      expect(el().querySelector('[role="status"]')!.textContent).toContain(
        '郵件設定已儲存，但測試信寄送失敗：SMTP 認證失敗',
      );
      expect(password().value).toBe('');
      expect(sendButton().disabled).toBeFalse();
      expect(saveButton().disabled).toBeFalse();

      sendButton().click();
      const retry = http.expectOne(SETTINGS_URL);
      expect('smtp_password' in retry.request.body).toBeFalse();
      retry.flush({ ...stored });
      http.expectOne(TEST_URL).flush({ success: true, message: '測試信已成功寄出' });
      await settle();
      expect(el().querySelector('[role="status"]')!.textContent).toContain('寄送成功');
    });
  }

  it('單獨儲存只送 PUT，儲存期間不能另寄信或重複儲存', async () => {
    await type('#smtpPass', 'SaveOnlySmtpPassword#2026');
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    sendButton().click();
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    fixture.detectChanges();
    const save = http.expectOne(SETTINGS_URL);
    expect(save.request.body.smtp_password).toBe('SaveOnlySmtpPassword#2026');
    http.expectNone(TEST_URL);
    expect(sendButton().disabled).toBeTrue();
    save.flush({ ...stored });
    await settle();
    expect(password().value).toBe('');
    expect(sendButton().disabled).toBeFalse();
    http.expectNone(TEST_URL);
  });

  it('單獨儲存失敗後保留輸入並解除操作鎖定', async () => {
    await type('#smtpPass', 'SaveRetrySmtpPassword#2026');
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    http
      .expectOne(SETTINGS_URL)
      .flush({ message: '設定暫時無法儲存' }, { status: 500, statusText: 'Server Error' });
    await settle();
    expect(password().value).toBe('SaveRetrySmtpPassword#2026');
    expect(saveButton().disabled).toBeFalse();
    expect(sendButton().disabled).toBeFalse();
    http.expectNone(TEST_URL);
  });

  it('收件人空白或必要設定不完整時，不儲存也不寄信', async () => {
    await type('.test-form input', '   ');
    expect(sendButton().disabled).toBeTrue();
    sendButton().click();
    http.expectNone(SETTINGS_URL);
    http.expectNone(TEST_URL);

    await type('.test-form input', 'recipient@example.com');
    await type('#smtpHost', '');
    expect(sendButton().disabled).toBeTrue();
    expect(saveButton().disabled).toBeTrue();
    sendButton().click();
    el().querySelector('form')!.dispatchEvent(new Event('submit'));
    http.expectNone(SETTINGS_URL);
    http.expectNone(TEST_URL);
  });
});
