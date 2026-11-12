import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { NotificationsPage } from './notifications.page';

describe('NotificationsPage', () => {
  let httpTesting: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [NotificationsPage],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();

    httpTesting = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpTesting.verify();
  });

  it('應該成功載入通知寄送紀錄並顯示狀態標籤', () => {
    const fixture = TestBed.createComponent(NotificationsPage);
    fixture.detectChanges();

    const req = httpTesting.expectOne((r) => r.url === '/api/v1/notifications/logs');
    expect(req.request.method).toBe('GET');
    req.flush({
      items: [
        {
          id: 1,
          notif_type: 'unregistered_digest',
          dedup_key: 'unreg|10.20.1.10|aa:bb:cc',
          recipient: 'admin@example.com',
          subject: '[Würzburg] 未登記在線位址彙整',
          status: 'sent',
          attempt_count: 1,
          created_at: '2026-11-12T02:00:00Z',
          sent_at: '2026-11-12T02:00:00Z',
        },
        {
          id: 2,
          notif_type: 'expiry_reminder',
          dedup_key: 'expiry|10.20.1.20|2026-11-12|1',
          recipient: 'user@example.com',
          subject: '[Würzburg] 請重新確認您的 IP 登記',
          status: 'failed',
          attempt_count: 3,
          last_error: 'SMTP connection timeout',
          created_at: '2026-11-12T03:00:00Z',
        },
      ],
      pagination: { page: 1, page_size: 50, total_items: 2, total_pages: 1 },
    });

    fixture.detectChanges();
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('未登記彙整');
    expect(compiled.textContent).toContain('到期催認');
    expect(compiled.textContent).toContain('SMTP connection timeout');
  });

  it('切換至去重狀態分頁應載入狀態並可呼叫重設', () => {
    const fixture = TestBed.createComponent(NotificationsPage);
    fixture.detectChanges();

    // 初始載入 logs
    httpTesting
      .expectOne((r) => r.url === '/api/v1/notifications/logs')
      .flush({
        items: [],
        pagination: { page: 1, page_size: 50, total_items: 0, total_pages: 0 },
      });
    fixture.detectChanges();

    // 切換至 states tab
    fixture.componentInstance.switchTab('states');
    fixture.detectChanges();

    const stateReq = httpTesting.expectOne((r) => r.url === '/api/v1/notifications/states');
    expect(stateReq.request.method).toBe('GET');
    stateReq.flush({
      items: [
        {
          id: 10,
          dedup_key: 'unreg|10.20.1.10|aa:bb:cc',
          notif_type: 'unregistered_digest',
          is_notified: true,
          notified_at: '2026-11-12T02:00:00Z',
          updated_at: '2026-11-12T02:00:00Z',
        },
      ],
      pagination: { page: 1, page_size: 50, total_items: 1, total_pages: 1 },
    });

    fixture.detectChanges();
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('unreg|10.20.1.10|aa:bb:cc');

    // 觸發重設
    fixture.componentInstance.resetState('unreg|10.20.1.10|aa:bb:cc');
    const resetReq = httpTesting.expectOne(
      '/api/v1/notifications/states/unreg%7C10.20.1.10%7Caa%3Abb%3Acc/reset',
    );
    expect(resetReq.request.method).toBe('POST');
    resetReq.flush(null, { status: 204, statusText: 'No Content' });

    // 重設後自動重新載入清單
    const reloadReq = httpTesting.expectOne((r) => r.url === '/api/v1/notifications/states');
    reloadReq.flush({
      items: [],
      pagination: { page: 1, page_size: 50, total_items: 0, total_pages: 0 },
    });
  });
});
