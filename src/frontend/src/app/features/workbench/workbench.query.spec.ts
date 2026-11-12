import { TestBed, ComponentFixture } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ActivatedRoute, convertToParamMap, Params, provideRouter, Router } from '@angular/router';
import { Dialog } from '@angular/cdk/dialog';
import { of } from 'rxjs';
import { WorkbenchPage } from './workbench.page';
import { AuthService, CurrentUser } from '../../core/auth.service';
import { MOCK_IP_RECORDS } from '../../core/mock-api';

const ADMIN: CurrentUser = {
  id: '1',
  username: 'admin',
  displayName: '系統管理員',
  role: 'admin',
  authSource: 'local',
  mail: 'admin@example.test',
  requiresPasswordChange: false,
};

const NORMAL: CurrentUser = {
  id: '2',
  username: 'alice',
  displayName: 'Alice',
  role: 'user',
  authSource: 'ad',
  mail: 'alice@example.test',
  requiresPasswordChange: false,
};

interface Harness {
  fixture: ComponentFixture<WorkbenchPage>;
  component: any;
  httpTesting: HttpTestingController;
  router: Router;
  dialog: Dialog;
}

function setup(params: Params, user: CurrentUser | null = NORMAL): Harness {
  TestBed.configureTestingModule({
    imports: [WorkbenchPage],
    providers: [
      provideHttpClient(),
      provideHttpClientTesting(),
      provideRouter([]),
      {
        provide: ActivatedRoute,
        useValue: {
          queryParams: of(params),
          snapshot: { queryParams: params, queryParamMap: convertToParamMap(params) },
        },
      },
    ],
  });
  const router = TestBed.inject(Router);
  spyOn(router, 'navigate').and.returnValue(Promise.resolve(true));
  spyOn(router, 'navigateByUrl').and.returnValue(Promise.resolve(true));
  const dialog = TestBed.inject(Dialog);
  spyOn(dialog, 'open').and.returnValue({ closed: of(undefined) } as any);
  TestBed.inject(AuthService).currentUser.set(user);

  const fixture = TestBed.createComponent(WorkbenchPage);
  fixture.detectChanges();
  return {
    fixture,
    component: fixture.componentInstance as any,
    httpTesting: TestBed.inject(HttpTestingController),
    router,
    dialog,
  };
}

function recordRequests(httpTesting: HttpTestingController) {
  return httpTesting.match((req) => req.url === '/api/v1/ip-records');
}

describe('WorkbenchPage 待重新確認（pending_confirmation）與郵件連結', () => {
  it('郵件連結 /?owner_me=true&pending_confirmation=true 應以兩個參數查詢 IP 紀錄', () => {
    const { httpTesting, component, fixture } = setup({
      owner_me: 'true',
      pending_confirmation: 'true',
    });
    const [req] = recordRequests(httpTesting);
    expect(req.request.params.get('owner_me')).toBe('true');
    expect(req.request.params.get('pending_confirmation')).toBe('true');
    expect(component.filters.pending_confirmation).toBeTrue();
    expect(component.filters.owner_me).toBeTrue();

    req.flush({
      items: MOCK_IP_RECORDS,
      pagination: { page: 1, page_size: 50, total_items: MOCK_IP_RECORDS.length, total_pages: 1 },
    });
    fixture.detectChanges();
    const chips = Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll('label.filter-chip.active'),
    ).map((el) => el.textContent?.trim());
    expect(chips).toContain('待重新確認');
    expect(chips).toContain('我的登記');
  });

  it('未登記通知連結 /?presence=online&registration=unregistered 應原樣帶入查詢', () => {
    const { httpTesting, component } = setup({ presence: 'online', registration: 'unregistered' });
    const [req] = recordRequests(httpTesting);
    expect(req.request.params.get('presence')).toBe('online');
    expect(req.request.params.get('registration')).toBe('unregistered');
    expect(req.request.params.has('pending_confirmation')).toBeFalse();
    expect(component.filters.presence).toBe('online');
    expect(component.filters.registration).toBe('unregistered');
    expect(component.filters.pending_confirmation).toBeFalse();
  });

  it('沒有查詢參數時不應送出 pending_confirmation', () => {
    const { httpTesting } = setup({});
    const [req] = recordRequests(httpTesting);
    expect(req.request.params.has('pending_confirmation')).toBeFalse();
    expect(req.request.params.has('owner_me')).toBeFalse();
  });

  it('套用篩選時應把 pending_confirmation 寫回網址，取消勾選則移除', () => {
    const { component, router } = setup({});
    component.filters.pending_confirmation = true;
    component.filters.owner_me = true;
    component.applyFilters(1);

    const withFilter = (router.navigate as jasmine.Spy).calls.mostRecent().args[1];
    expect(withFilter.queryParams.pending_confirmation).toBe('true');
    expect(withFilter.queryParams.owner_me).toBe('true');

    component.filters.pending_confirmation = false;
    component.applyFilters(1);
    const withoutFilter = (router.navigate as jasmine.Spy).calls.mostRecent().args[1];
    expect(withoutFilter.queryParams.pending_confirmation).toBeNull();
  });

  it('匯出 Excel 應帶入 pending_confirmation 以匯出相同範圍', () => {
    const { component } = setup({ owner_me: 'true', pending_confirmation: 'true' });
    const openSpy = spyOn(window, 'open').and.returnValue(null);
    component.exportExcel();
    const url = openSpy.calls.mostRecent().args[0] as string;
    expect(url).toContain('pending_confirmation=true');
    expect(url).toContain('owner_me=true');
  });

  it('勾選「全部符合篩選」的批次操作應把 pending_confirmation 一併送出', () => {
    const { component, httpTesting } = setup({ owner_me: 'true', pending_confirmation: 'true' });
    component.selectAllFiltered.set(true);
    component.executeBatch('confirm');
    const req = httpTesting.expectOne('/api/v1/ip-records/actions/confirm');
    expect(req.request.body.select_all_filtered).toBeTrue();
    expect(req.request.body.filter.pending_confirmation).toBeTrue();
    expect(req.request.body.filter.owner_me).toBeTrue();
  });
});

describe('WorkbenchPage 精靈結束後保留目的地', () => {
  it('管理員帶 wizard=true 與 returnUrl 開啟精靈，結束後應回到 returnUrl', () => {
    const { dialog, router } = setup({ wizard: 'true', returnUrl: '/subnets' }, ADMIN);
    expect(dialog.open).toHaveBeenCalled();
    expect(router.navigateByUrl).toHaveBeenCalledWith('/subnets');
  });

  it('沒有 returnUrl 時精靈結束後應移除 wizard 參數，避免之後切換篩選又重開精靈', () => {
    const { router } = setup({ wizard: 'true' }, ADMIN);
    expect(router.navigateByUrl).not.toHaveBeenCalled();
    const call = (router.navigate as jasmine.Spy).calls
      .allArgs()
      .find((args) => args[1]?.queryParams?.wizard === null);
    expect(call).toBeDefined();
    expect(call?.[1].queryParamsHandling).toBe('merge');
  });

  it('惡意 returnUrl 應被忽略', () => {
    const { router } = setup({ wizard: 'true', returnUrl: 'https://evil.example/' }, ADMIN);
    expect(router.navigateByUrl).not.toHaveBeenCalled();
  });

  it('一般使用者帶 wizard=true 不應開啟精靈', () => {
    const { dialog } = setup({ wizard: 'true', returnUrl: '/subnets' }, NORMAL);
    expect(dialog.open).not.toHaveBeenCalled();
  });

  it('尚未取得使用者（空值）時帶 wizard=true 不應拋錯或開啟精靈', () => {
    const { dialog } = setup({ wizard: 'true' }, null);
    expect(dialog.open).not.toHaveBeenCalled();
  });
});

describe('WorkbenchPage 登記表單預設信箱', () => {
  function openDetail(h: Harness, ownerMail: string | undefined) {
    h.component.openDetail(101);
    h.httpTesting.expectOne('/api/v1/ip-records/101').flush({
      id: 101,
      ip_address: '10.10.10.50',
      registration: 'unregistered',
      owner_mail: ownerMail,
    });
  }

  it('尚未登記時應預設帶入目前帳號的信箱', () => {
    const h = setup({}, NORMAL);
    openDetail(h, undefined);
    expect(h.component.regForm.owner_mail).toBe('alice@example.test');
  });

  it('帳號沒有信箱時不可捏造位址，預設為空字串', () => {
    const h = setup({}, { ...NORMAL, mail: undefined });
    openDetail(h, undefined);
    expect(h.component.regForm.owner_mail).toBe('');
  });

  it('沒有登入使用者時預設為空字串且不拋錯', () => {
    const h = setup({}, null);
    openDetail(h, undefined);
    expect(h.component.regForm.owner_mail).toBe('');
  });

  it('既有登記信箱優先於帳號信箱', () => {
    const h = setup({}, NORMAL);
    openDetail(h, 'shared@example.test');
    expect(h.component.regForm.owner_mail).toBe('shared@example.test');
  });
});
