import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { routes } from './app.routes';
import { App } from './app';
import { AuthService, CurrentUser } from './core/auth.service';
import { ThemeService } from './core/theme.service';
import { SidebarBadgesService } from './core/sidebar-badges.service';
import { ShellLayoutComponent } from './shared/components/shell-layout.component';

const ADMIN_USER: CurrentUser = {
  id: '1',
  username: 'admin',
  displayName: '系統管理員',
  role: 'admin',
  authSource: 'local',
  requiresPasswordChange: false,
};

const NORMAL_USER: CurrentUser = {
  id: '2',
  username: 'alice',
  displayName: 'Alice 王',
  role: 'user',
  authSource: 'ad',
  requiresPasswordChange: false,
};

describe('App Shell and Design System', () => {
  let httpTesting: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [App],
      providers: [provideRouter(routes), provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
    httpTesting = TestBed.inject(HttpTestingController);
  });

  it('系統管理員應看到應用程式外殼與側欄全部導覽項目', async () => {
    const fixture = TestBed.createComponent(App);
    const router = TestBed.inject(Router);
    TestBed.inject(AuthService).currentUser.set(ADMIN_USER);
    await router.navigateByUrl('/');
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('Würzburg');
    expect(compiled.textContent).toContain('首頁工作臺');
    expect(compiled.textContent).toContain('網段');
    expect(compiled.textContent).toContain('系統設定');
    expect(compiled.querySelector('.user-name')?.textContent).toContain('系統管理員');
    expect(compiled.querySelector('.user-role')?.textContent).toContain('系統管理員');
  });

  it('一般使用者身分應隱藏系統管理員專用導覽入口並顯示自己的名稱', async () => {
    const fixture = TestBed.createComponent(App);
    const router = TestBed.inject(Router);
    TestBed.inject(AuthService).currentUser.set(NORMAL_USER);
    await router.navigateByUrl('/');
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    const navText = compiled.querySelector('.sidebar-nav')?.textContent ?? '';
    expect(navText).toContain('首頁工作臺');
    expect(navText).toContain('網段');
    expect(navText).toContain('衝突');
    expect(navText).not.toContain('系統設定');
    expect(navText).not.toContain('交換器');
    expect(compiled.querySelector('.user-name')?.textContent).toContain('Alice 王');
    expect(compiled.querySelector('.user-role')?.textContent).toContain('一般使用者');
  });

  it('未登入（/auth/me 回 401）時開啟首頁應導向登入頁並保留原網址', async () => {
    const fixture = TestBed.createComponent(App);
    const router = TestBed.inject(Router);
    const navigation = router.navigateByUrl('/?owner_me=true&pending_confirmation=true');

    let answered = false;
    for (let i = 0; i < 50 && !answered; i++) {
      const pending = httpTesting.match('/api/v1/auth/me');
      if (pending.length > 0) {
        pending[0].flush({ code: 'unauthorized' }, { status: 401, statusText: 'Unauthorized' });
        answered = true;
      } else {
        await new Promise((resolve) => setTimeout(resolve, 0));
      }
    }
    await navigation;
    fixture.detectChanges();

    expect(answered).toBeTrue();
    expect(TestBed.inject(AuthService).currentUser()).toBeNull();
    expect(router.url).toBe('/login?returnUrl=%2F%3Fowner_me%3Dtrue%26pending_confirmation%3Dtrue');
    expect((fixture.nativeElement as HTMLElement).querySelector('.sidebar')).toBeNull();
  });

  it('應該支援切換淺色與深色主題', () => {
    const theme = TestBed.inject(ThemeService);
    const initial = theme.currentTheme();
    theme.toggleTheme();
    expect(theme.currentTheme()).not.toBe(initial);
  });

  it('側欄徽章服務應能更新待檢視網段與衝突計數', async () => {
    const fixture = TestBed.createComponent(App);
    const router = TestBed.inject(Router);
    const badges = TestBed.inject(SidebarBadgesService);
    TestBed.inject(AuthService).currentUser.set(ADMIN_USER);
    badges.pendingSubnetsCount.set(5);
    badges.openConflictsCount.set(3);
    await router.navigateByUrl('/');
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('5');
    expect(compiled.textContent).toContain('3');
  });
});

describe('ShellLayoutComponent 使用者為空時的安全性', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ShellLayoutComponent],
      providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
  });

  it('尚未取得使用者時不應拋錯，也不應顯示管理員專用導覽', () => {
    const auth = TestBed.inject(AuthService);
    expect(auth.currentUser()).toBeNull();

    const fixture = TestBed.createComponent(ShellLayoutComponent);
    expect(() => fixture.detectChanges()).not.toThrow();

    const compiled = fixture.nativeElement as HTMLElement;
    const navText = compiled.querySelector('.sidebar-nav')?.textContent ?? '';
    expect(navText).not.toContain('系統設定');
    expect(navText).not.toContain('交換器');
    expect(compiled.querySelector('.user-name')?.textContent?.trim()).toBe('');
  });
});
