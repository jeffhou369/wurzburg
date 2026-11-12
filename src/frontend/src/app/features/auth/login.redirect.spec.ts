import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ActivatedRoute, convertToParamMap, provideRouter, Router } from '@angular/router';
import { LoginPage } from './login.page';
import { ChangePasswordPage } from './change-password.page';
import { AuthService } from '../../core/auth.service';

function configure(returnUrl: string | null): {
  httpTesting: HttpTestingController;
  router: Router;
  auth: AuthService;
} {
  TestBed.configureTestingModule({
    imports: [LoginPage, ChangePasswordPage],
    providers: [
      provideHttpClient(),
      provideHttpClientTesting(),
      provideRouter([]),
      {
        provide: ActivatedRoute,
        useValue: {
          snapshot: {
            queryParamMap: convertToParamMap(returnUrl === null ? {} : { returnUrl }),
          },
        },
      },
    ],
  });
  const router = TestBed.inject(Router);
  spyOn(router, 'navigateByUrl').and.returnValue(Promise.resolve(true));
  return {
    httpTesting: TestBed.inject(HttpTestingController),
    router,
    auth: TestBed.inject(AuthService),
  };
}

function loginAs(
  role: 'admin' | 'user',
  extra: { requires_password_change?: boolean } = {},
): { id: number; username: string; display_name: string; role: string; auth_source: string } & {
  requires_password_change?: boolean;
} {
  return {
    id: role === 'admin' ? 1 : 2,
    username: role === 'admin' ? 'admin' : 'alice',
    display_name: role === 'admin' ? '系統管理員' : 'Alice',
    role,
    auth_source: role === 'admin' ? 'local' : 'ad',
    ...extra,
  };
}

function submitLogin(
  httpTesting: HttpTestingController,
  user: ReturnType<typeof loginAs>,
  mode: 'local' | 'org' = 'org',
): void {
  const fixture = TestBed.createComponent(LoginPage);
  fixture.detectChanges();
  const component = fixture.componentInstance as any;
  component.switchMode(mode);
  component.username = user.username;
  component.password = 'SomePassword#123';
  component.submit();
  httpTesting
    .expectOne(mode === 'local' ? '/api/v1/auth/login/local' : '/api/v1/auth/login/org')
    .flush({ user });
}

describe('LoginPage 登入後導向與 returnUrl', () => {
  afterEach(() => {
    TestBed.inject(HttpTestingController).verify();
  });

  it('一般使用者登入後應導向 returnUrl 並正確顯示一般使用者身分', () => {
    const { httpTesting, router, auth } = configure('/subnets?search=a');
    submitLogin(httpTesting, loginAs('user'));

    expect(router.navigateByUrl).toHaveBeenCalledWith('/subnets?search=a');
    expect(auth.isAdmin()).toBeFalse();
    expect(auth.currentUser()?.displayName).toBe('Alice');
    expect(auth.currentUser()?.role).toBe('user');
  });

  it('沒有 returnUrl 的一般使用者登入後導向首頁', () => {
    const { httpTesting, router } = configure(null);
    submitLogin(httpTesting, loginAs('user'));
    expect(router.navigateByUrl).toHaveBeenCalledWith('/');
  });

  it('郵件連結 /?owner_me=true&pending_confirmation=true 登入後應完整保留', () => {
    const { httpTesting, router } = configure('/?owner_me=true&pending_confirmation=true');
    submitLogin(httpTesting, loginAs('user'));
    expect(router.navigateByUrl).toHaveBeenCalledWith('/?owner_me=true&pending_confirmation=true');
  });

  it('管理員已完成精靈時登入後直接導向 returnUrl', () => {
    const { httpTesting, router, auth } = configure('/?presence=online&registration=unregistered');
    submitLogin(httpTesting, loginAs('admin'), 'local');
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: true, current_step: 5, unfinished_items: [] });

    expect(router.navigateByUrl).toHaveBeenCalledWith(
      '/?presence=online&registration=unregistered',
    );
    expect(auth.isAdmin()).toBeTrue();
  });

  it('管理員精靈未完成時應先進精靈並攜帶 returnUrl 以便完成後回到目的地', () => {
    const { httpTesting, router } = configure('/subnets');
    submitLogin(httpTesting, loginAs('admin'), 'local');
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 1, unfinished_items: [] });

    expect(router.navigateByUrl).toHaveBeenCalledWith('/?wizard=true&returnUrl=%2Fsubnets');
  });

  it('管理員精靈未完成且沒有 returnUrl 時維持 /?wizard=true', () => {
    const { httpTesting, router } = configure(null);
    submitLogin(httpTesting, loginAs('admin'), 'local');
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 1, unfinished_items: [] });
    expect(router.navigateByUrl).toHaveBeenCalledWith('/?wizard=true');
  });

  it('查詢精靈狀態失敗時仍應導向 returnUrl', () => {
    const { httpTesting, router } = configure('/conflicts');
    submitLogin(httpTesting, loginAs('admin'), 'local');
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush('x', { status: 500, statusText: 'Server Error' });
    expect(router.navigateByUrl).toHaveBeenCalledWith('/conflicts');
  });

  it('需要強制改密碼時應導向 change-password 並攜帶 returnUrl', () => {
    const { httpTesting, router } = configure('/subnets');
    submitLogin(httpTesting, loginAs('admin', { requires_password_change: true }), 'local');
    expect(router.navigateByUrl).toHaveBeenCalledWith('/change-password?returnUrl=%2Fsubnets');
  });

  const hostileUrls = [
    'https://evil.example/phish',
    '//evil.example',
    '/\\evil.example',
    '\\\\evil.example',
    'javascript:alert(1)',
  ];

  hostileUrls.forEach((hostile) => {
    it(`惡意 returnUrl（${hostile}）應被忽略並導向首頁`, () => {
      const { httpTesting, router } = configure(hostile);
      submitLogin(httpTesting, loginAs('user'));
      expect(router.navigateByUrl).toHaveBeenCalledOnceWith('/');
    });
  });
});

describe('ChangePasswordPage 改密碼後導向與 returnUrl', () => {
  afterEach(() => {
    TestBed.inject(HttpTestingController).verify();
  });

  function submitChange(httpTesting: HttpTestingController): void {
    const fixture = TestBed.createComponent(ChangePasswordPage);
    fixture.detectChanges();
    const component = fixture.componentInstance as any;
    component.newPassword = 'AnotherLongPassword#1';
    component.confirmPassword = 'AnotherLongPassword#1';
    component.submit();
    const req = httpTesting.expectOne('/api/v1/auth/change-password');
    expect(req.request.method).toBe('POST');
    req.flush(null, { status: 204, statusText: 'No Content' });
  }

  function signIn(auth: AuthService, role: 'admin' | 'user'): void {
    auth.currentUser.set({
      id: '1',
      username: role === 'admin' ? 'admin' : 'alice',
      displayName: role === 'admin' ? '系統管理員' : 'Alice',
      role,
      authSource: role === 'admin' ? 'local' : 'ad',
      requiresPasswordChange: true,
    });
  }

  it('管理員改密碼後精靈未完成：進精靈並攜帶 returnUrl', () => {
    const { httpTesting, router, auth } = configure('/subnets');
    signIn(auth, 'admin');
    submitChange(httpTesting);
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 1, unfinished_items: [] });

    expect(router.navigateByUrl).toHaveBeenCalledWith('/?wizard=true&returnUrl=%2Fsubnets');
    expect(auth.currentUser()?.requiresPasswordChange).toBeFalse();
  });

  it('管理員改密碼後精靈已完成：直接回到 returnUrl', () => {
    const { httpTesting, router, auth } = configure('/audit');
    signIn(auth, 'admin');
    submitChange(httpTesting);
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: true, current_step: 5, unfinished_items: [] });
    expect(router.navigateByUrl).toHaveBeenCalledWith('/audit');
  });

  it('管理員改密碼且沒有 returnUrl、精靈未完成：維持 /?wizard=true', () => {
    const { httpTesting, router, auth } = configure(null);
    signIn(auth, 'admin');
    submitChange(httpTesting);
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 1, unfinished_items: [] });
    expect(router.navigateByUrl).toHaveBeenCalledWith('/?wizard=true');
  });

  it('一般使用者改密碼後應直接回到 returnUrl，不查詢精靈', () => {
    const { httpTesting, router, auth } = configure('/?owner_me=true&pending_confirmation=true');
    signIn(auth, 'user');
    submitChange(httpTesting);
    httpTesting.expectNone('/api/v1/settings/wizard');
    expect(router.navigateByUrl).toHaveBeenCalledWith('/?owner_me=true&pending_confirmation=true');
  });

  it('重新整理後尚未還原使用者時，改密碼成功應先以 /auth/me 還原再導向', () => {
    const { httpTesting, router, auth } = configure('/subnets');
    submitChange(httpTesting);
    httpTesting.expectOne('/api/v1/auth/me').flush({
      id: 2,
      username: 'alice',
      display_name: 'Alice',
      role: 'user',
      auth_source: 'ad',
      requires_password_change: false,
    });
    expect(router.navigateByUrl).toHaveBeenCalledWith('/subnets');
    expect(auth.currentUser()?.username).toBe('alice');
  });

  it('惡意 returnUrl 應被忽略', () => {
    const { httpTesting, router, auth } = configure('//evil.example');
    signIn(auth, 'user');
    submitChange(httpTesting);
    expect(router.navigateByUrl).toHaveBeenCalledOnceWith('/');
  });
});
