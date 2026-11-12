import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import {
  ActivatedRouteSnapshot,
  provideRouter,
  Router,
  RouterStateSnapshot,
  UrlTree,
} from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { authGuard } from './auth.guard';
import { AuthService } from './auth.service';
import { routes } from '../app.routes';
import { ShellLayoutComponent } from '../shared/components/shell-layout.component';

@Component({ standalone: true, template: 'stub-page' })
class StubPage {}

const ME_ADMIN = {
  id: 1,
  username: 'admin',
  display_name: '系統管理員',
  role: 'admin',
  auth_source: 'local',
  requires_password_change: false,
};

describe('authGuard', () => {
  let httpTesting: HttpTestingController;
  let router: Router;

  const runGuard = (url: string): Promise<boolean | UrlTree> =>
    new Promise((resolve, reject) => {
      const result = TestBed.runInInjectionContext(() =>
        authGuard({} as ActivatedRouteSnapshot, { url } as RouterStateSnapshot),
      );
      (result as import('rxjs').Observable<boolean | UrlTree>).subscribe({
        next: resolve,
        error: reject,
      });
    });

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), provideRouter([])],
    });
    httpTesting = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
  });

  afterEach(() => {
    httpTesting.verify();
  });

  it('未登入（401）時應導向 login 並保留原站內完整路徑與查詢字串', async () => {
    const pending = runGuard('/subnets?search=a%20b&page=2');
    httpTesting
      .expectOne('/api/v1/auth/me')
      .flush({ code: 'unauthorized' }, { status: 401, statusText: 'Unauthorized' });

    const result = await pending;
    expect(result instanceof UrlTree).toBeTrue();
    expect(router.serializeUrl(result as UrlTree)).toBe(
      '/login?returnUrl=%2Fsubnets%3Fsearch%3Da%2520b%26page%3D2',
    );
  });

  it('導向郵件連結目的地時 returnUrl 應完整保留 owner_me 與 pending_confirmation', async () => {
    const pending = runGuard('/?owner_me=true&pending_confirmation=true');
    httpTesting.expectOne('/api/v1/auth/me').flush(null, { status: 401, statusText: 'x' });

    const tree = (await pending) as UrlTree;
    expect(tree.queryParams['returnUrl']).toBe('/?owner_me=true&pending_confirmation=true');
  });

  it('已登入且不需改密碼時應放行', async () => {
    const pending = runGuard('/subnets');
    httpTesting.expectOne('/api/v1/auth/me').flush(ME_ADMIN);
    expect(await pending).toBeTrue();
    expect(TestBed.inject(AuthService).isAdmin()).toBeTrue();
  });

  it('已有還原過的使用者時不應重複呼叫 /auth/me', async () => {
    TestBed.inject(AuthService).currentUser.set({
      id: '2',
      username: 'alice',
      displayName: 'Alice',
      role: 'user',
      authSource: 'ad',
      requiresPasswordChange: false,
    });
    expect(await runGuard('/subnets')).toBeTrue();
    httpTesting.expectNone('/api/v1/auth/me');
  });

  it('需要強制改密碼時應導向 change-password 並保留目的地', async () => {
    const pending = runGuard('/subnets');
    httpTesting.expectOne('/api/v1/auth/me').flush({ ...ME_ADMIN, requires_password_change: true });

    const tree = (await pending) as UrlTree;
    expect(router.serializeUrl(tree)).toBe('/change-password?returnUrl=%2Fsubnets');
  });

  it('後端 5xx 時應以未登入處理（失敗即拒絕），不可放行', async () => {
    const pending = runGuard('/settings');
    httpTesting.expectOne('/api/v1/auth/me').flush('boom', { status: 500, statusText: 'x' });
    const tree = (await pending) as UrlTree;
    expect(tree.queryParams['returnUrl']).toBe('/settings');
    expect(router.serializeUrl(tree).startsWith('/login')).toBeTrue();
  });
});

describe('app.routes 登入保護設定', () => {
  it('Shell 路由應掛載 authGuard，登入與改密碼頁不得掛載', () => {
    const shell = routes.find((r) => r.component === ShellLayoutComponent);
    expect(shell).toBeDefined();
    expect(shell?.canActivate).toContain(authGuard);
    expect(shell?.canActivateChild).toContain(authGuard);

    const login = routes.find((r) => r.path === 'login');
    const change = routes.find((r) => r.path === 'change-password');
    expect(login?.canActivate).toBeUndefined();
    expect(change?.canActivate).toBeUndefined();
  });
});

describe('authGuard 於路由器的整合行為（TestBed 元件層級，非瀏覽器 E2E）', () => {
  let httpTesting: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([
          { path: 'login', component: StubPage },
          { path: 'change-password', component: StubPage },
          {
            path: '',
            canActivate: [authGuard],
            children: [{ path: 'subnets', component: StubPage }],
          },
        ]),
      ],
    });
    httpTesting = TestBed.inject(HttpTestingController);
  });

  const flushMe = async (
    respond: (req: import('@angular/common/http/testing').TestRequest) => void,
  ) => {
    for (let i = 0; i < 50; i++) {
      const matches = httpTesting.match('/api/v1/auth/me');
      if (matches.length) {
        respond(matches[0]);
        return;
      }
      await new Promise((r) => setTimeout(r, 0));
    }
    throw new Error('未收到 /api/v1/auth/me 請求');
  };

  it('未登入直接開啟深層連結時，網址應被導向 /login?returnUrl=原路徑', async () => {
    const harness = await RouterTestingHarness.create();
    const nav = harness.navigateByUrl('/subnets?search=x');
    await flushMe((req) => req.flush(null, { status: 401, statusText: 'Unauthorized' }));
    await nav;

    const router = TestBed.inject(Router);
    expect(router.url).toBe('/login?returnUrl=%2Fsubnets%3Fsearch%3Dx');
  });

  it('已登入時應停留在原網址', async () => {
    const harness = await RouterTestingHarness.create();
    const nav = harness.navigateByUrl('/subnets');
    await flushMe((req) => req.flush(ME_ADMIN));
    await nav;

    expect(TestBed.inject(Router).url).toBe('/subnets');
  });
});
