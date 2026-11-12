import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { provideHttpClient } from '@angular/common/http';
import { routes } from './app.routes';
import { App } from './app';
import { AuthService } from './core/auth.service';
import { ThemeService } from './core/theme.service';
import { SidebarBadgesService } from './core/sidebar-badges.service';

describe('App Shell and Design System', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [App],
      providers: [provideRouter(routes), provideHttpClient()],
    }).compileComponents();
  });

  it('應該建立應用程式外殼與側欄全部導覽項目', async () => {
    const fixture = TestBed.createComponent(App);
    const router = TestBed.inject(Router);
    await router.navigateByUrl('/');
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('Würzburg');
    expect(compiled.textContent).toContain('首頁工作臺');
    expect(compiled.textContent).toContain('網段');
    expect(compiled.textContent).toContain('系統設定');
  });

  it('一般使用者身分應隱藏系統管理員專用導覽入口', async () => {
    const fixture = TestBed.createComponent(App);
    const router = TestBed.inject(Router);
    const auth = TestBed.inject(AuthService);
    auth.setRole('user');
    await router.navigateByUrl('/');
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    const navText = compiled.querySelector('.sidebar-nav')?.textContent ?? '';
    expect(navText).toContain('首頁工作臺');
    expect(navText).toContain('網段');
    expect(navText).toContain('衝突');
    expect(navText).not.toContain('系統設定');
    expect(navText).not.toContain('交換器');
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
    badges.pendingSubnetsCount.set(5);
    badges.openConflictsCount.set(3);
    await router.navigateByUrl('/');
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.textContent).toContain('5');
    expect(compiled.textContent).toContain('3');
  });
});
