import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideRouter, Router } from '@angular/router';
import { LoginPage } from './login.page';
import { ChangePasswordPage } from './change-password.page';

describe('LoginPage and ChangePasswordPage', () => {
  let httpTesting: HttpTestingController;
  let router: Router;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [LoginPage, ChangePasswordPage],
      providers: [provideHttpClient(), provideHttpClientTesting(), provideRouter([])],
    }).compileComponents();

    httpTesting = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
    spyOn(router, 'navigateByUrl').and.returnValue(Promise.resolve(true));
  });

  afterEach(() => {
    httpTesting.verify();
  });

  it('系統管理員首次登入若需要修改密碼應導向至 /change-password', () => {
    const fixture = TestBed.createComponent(LoginPage);
    fixture.detectChanges();
    const component = fixture.componentInstance;

    component.switchMode('local');
    (component as any).username = 'admin';
    (component as any).password = 'InitialPass#123';
    component.submit();

    const req = httpTesting.expectOne('/api/v1/auth/login/local');
    expect(req.request.method).toBe('POST');
    req.flush({
      user: {
        id: 1,
        username: 'admin',
        display_name: '系統管理員',
        role: 'admin',
        auth_source: 'local',
        requires_password_change: true,
      },
    });

    expect(router.navigateByUrl).toHaveBeenCalledWith('/change-password');
  });

  it('變更密碼少於 12 字元時應在客戶端顯示錯誤', () => {
    const fixture = TestBed.createComponent(ChangePasswordPage);
    fixture.detectChanges();
    const component = fixture.componentInstance;

    (component as any).newPassword = 'short';
    (component as any).confirmPassword = 'short';
    component.submit();

    httpTesting.expectNone('/api/v1/auth/change-password');
    const compiled = fixture.nativeElement as HTMLElement;
    fixture.detectChanges();
    expect(compiled.textContent).toContain('至少需要 12 個字元');
  });
});
