import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideRouter } from '@angular/router';
import { WorkbenchPage } from './workbench.page';
import { MOCK_DASHBOARD_SUMMARY, MOCK_IP_RECORDS, MOCK_SUBNETS } from '../../core/mock-api';
import { AuthService } from '../../core/auth.service';

describe('WorkbenchPage', () => {
  let httpTesting: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [WorkbenchPage],
      providers: [provideHttpClient(), provideHttpClientTesting(), provideRouter([])],
    }).compileComponents();

    httpTesting = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpTesting.verify();
  });

  /** 以下情境檢視的是系統管理員畫面（含設定精靈未完成清單），因此明確以管理員登入。 */
  const signInAsAdmin = () =>
    TestBed.inject(AuthService).currentUser.set({
      id: '1',
      username: 'admin',
      displayName: '系統管理員',
      role: 'admin',
      authSource: 'local',
      requiresPasswordChange: false,
    });

  it('一般使用者與未登入狀態都不應查詢設定精靈狀態（僅系統管理員需要）', () => {
    const auth = TestBed.inject(AuthService);
    auth.currentUser.set({
      id: '2',
      username: 'alice',
      displayName: 'Alice',
      role: 'user',
      authSource: 'ad',
      requiresPasswordChange: false,
    });
    const fixture = TestBed.createComponent(WorkbenchPage);
    fixture.detectChanges();
    expect(httpTesting.match('/api/v1/settings/wizard').length).toBe(0);
    httpTesting.match(() => true);
  });

  it('應該載入儀表板統計與 IP 表格，並將離線列呈現為灰階', () => {
    signInAsAdmin();
    const fixture = TestBed.createComponent(WorkbenchPage);
    fixture.detectChanges();

    httpTesting.expectOne('/api/v1/settings/wizard').flush({
      completed: true,
      current_step: 5,
      unfinished_items: [],
    });
    httpTesting.expectOne('/api/v1/settings/campus').flush({
      campus_name: 'Würzburg',
      timezone: 'Asia/Taipei',
    });
    httpTesting.expectOne('/api/v1/dashboard/stats').flush(MOCK_DASHBOARD_SUMMARY);
    httpTesting.expectOne('/api/v1/subnets').flush({ pending_count: 1, items: MOCK_SUBNETS });
    httpTesting
      .expectOne((req) => req.url === '/api/v1/ip-records')
      .flush({
        items: MOCK_IP_RECORDS,
        pagination: { page: 1, page_size: 50, total_items: MOCK_IP_RECORDS.length, total_pages: 1 },
      });

    fixture.detectChanges();
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('1240');
    expect(compiled.textContent).toContain('10.10.10.50');
    expect(compiled.textContent).toContain('10.10.10.88');

    const offlineRows = compiled.querySelectorAll('tr.row-offline');
    expect(offlineRows.length).toBe(1);
  });

  it('勾選多筆後應顯示批次操作列並可執行批次重新確認', () => {
    signInAsAdmin();
    const fixture = TestBed.createComponent(WorkbenchPage);
    fixture.detectChanges();

    httpTesting.expectOne('/api/v1/settings/wizard').flush({
      completed: true,
      current_step: 5,
      unfinished_items: [],
    });
    httpTesting.expectOne('/api/v1/settings/campus').flush({
      campus_name: 'Würzburg',
      timezone: 'Asia/Taipei',
    });
    httpTesting.expectOne('/api/v1/dashboard/stats').flush(MOCK_DASHBOARD_SUMMARY);
    httpTesting.expectOne('/api/v1/subnets').flush({ pending_count: 1, items: MOCK_SUBNETS });
    httpTesting
      .expectOne((req) => req.url === '/api/v1/ip-records')
      .flush({
        items: MOCK_IP_RECORDS,
        pagination: { page: 1, page_size: 50, total_items: MOCK_IP_RECORDS.length, total_pages: 1 },
      });

    fixture.detectChanges();
    const component = fixture.componentInstance as any;
    component.toggleRow(101);
    component.toggleRow(103);
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('.batch-bar')?.textContent).toContain('已選取本頁 2 筆');

    component.executeBatch('confirm');
    const confirmReq = httpTesting.expectOne('/api/v1/ip-records/actions/confirm');
    expect(confirmReq.request.method).toBe('POST');
    expect(confirmReq.request.body.ip_record_ids).toEqual([101, 103]);
    confirmReq.flush({ succeeded_count: 2, skipped_count: 0, results: [] });

    // 刷新列表與統計
    httpTesting.expectOne('/api/v1/dashboard/stats').flush(MOCK_DASHBOARD_SUMMARY);
    httpTesting
      .expectOne((req) => req.url === '/api/v1/ip-records')
      .flush({
        items: MOCK_IP_RECORDS,
        pagination: { page: 1, page_size: 50, total_items: MOCK_IP_RECORDS.length, total_pages: 1 },
      });
  });
});
