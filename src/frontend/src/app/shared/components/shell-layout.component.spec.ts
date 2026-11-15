import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideRouter } from '@angular/router';
import { ShellLayoutComponent } from './shell-layout.component';
import { AuthService } from '../../core/auth.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';

const PREFERENCE_KEY = 'wurzburg_sidebar_collapsed';

describe('ShellLayoutComponent（外殼 DOM 輔助測試）', () => {
  let fixture: ComponentFixture<ShellLayoutComponent>;
  let http: HttpTestingController;
  const el = () => fixture.nativeElement as HTMLElement;
  const toggle = () => el().querySelector<HTMLButtonElement>('.desktop-sidebar-toggle')!;

  beforeEach(async () => {
    localStorage.removeItem(PREFERENCE_KEY);
    localStorage.removeItem('wurzburg_theme');
    spyOn(window, 'matchMedia').and.returnValue({ matches: false } as MediaQueryList);
    await TestBed.configureTestingModule({
      imports: [ShellLayoutComponent],
      providers: [provideRouter([]), provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    TestBed.inject(AuthService).currentUser.set({
      id: '1',
      username: 'admin',
      displayName: '系統管理員',
      role: 'admin',
      authSource: 'local',
      requiresPasswordChange: false,
    });
  });

  afterEach(() => {
    http.verify();
    localStorage.removeItem(PREFERENCE_KEY);
    localStorage.removeItem('wurzburg_theme');
  });

  function render() {
    fixture = TestBed.createComponent(ShellLayoutComponent);
    fixture.detectChanges();
    http.expectOne('/api/v1/settings/campus').flush({ campus_name: '測試園區' });
    fixture.detectChanges();
  }

  it('可由展開切換成圖示列並記住偏好，再切回展開', () => {
    render();
    expect(toggle().getAttribute('aria-label')).toBe('收合側欄');
    toggle().click();
    fixture.detectChanges();
    expect(toggle().getAttribute('aria-expanded')).toBe('false');
    expect(toggle().getAttribute('aria-label')).toBe('展開側欄');
    expect(localStorage.getItem(PREFERENCE_KEY)).toBe('true');
    expect(el().querySelector('.app-shell')!.classList.contains('sidebar-collapsed')).toBeTrue();
    toggle().click();
    fixture.detectChanges();
    expect(toggle().getAttribute('aria-expanded')).toBe('true');
    expect(localStorage.getItem(PREFERENCE_KEY)).toBe('false');
  });

  it('讀取先前折疊偏好後，仍有完整導覽名稱與使用者身分可供輔助工具辨識', () => {
    localStorage.setItem(PREFERENCE_KEY, 'true');
    render();
    expect(toggle().getAttribute('aria-expanded')).toBe('false');
    expect(el().querySelector('a[href="/"]')!.getAttribute('aria-label')).toBe('首頁工作臺');
    expect(el().querySelector('.user-card')!.getAttribute('aria-label')).toBe(
      '系統管理員，系統管理員',
    );
    expect(el().querySelector('.user-card')!.getAttribute('tabindex')).toBe('0');
    expect(el().querySelector('.user-card')!.getAttribute('role')).toBe('group');
  });

  it('折疊後仍顯示徽章，並在導覽名稱保留未縮寫的完整數量', () => {
    localStorage.setItem(PREFERENCE_KEY, 'true');
    const badges = TestBed.inject(SidebarBadgesService);
    badges.pendingSubnetsCount.set(128);
    badges.openConflictsCount.set(7);
    render();
    const subnets = el().querySelector('a[href="/subnets"]')!;
    const conflicts = el().querySelector('a[href="/conflicts"]')!;
    expect(subnets.querySelector('.nav-badge')!.textContent?.trim()).toBe('99+');
    expect(subnets.getAttribute('aria-label')).toBe('網段，待檢視 128 筆');
    expect(conflicts.querySelector('.nav-badge')!.textContent?.trim()).toBe('7');
    expect(conflicts.getAttribute('aria-label')).toBe('衝突，未處理 7 筆');
  });

  it('主題與登出依序位於內容區右上，側欄底部只保留使用者且沒有展示入口', () => {
    render();
    const actions = el().querySelector('.workspace-actions')!;
    const buttons = actions.querySelectorAll('button');
    expect(buttons[0].getAttribute('aria-label')).toBe('切換外觀主題');
    expect(buttons[1].textContent).toContain('登出');
    expect(el().querySelector('.sidebar-footer button')).toBeNull();
    expect(el().querySelector('.sidebar-footer')!.textContent).toContain('系統管理員');
    expect(el().querySelector('a[href="/showcase"]')).toBeNull();
  });

  it('瀏覽器拒絕儲存讀寫時仍可折疊側欄及切換主題', () => {
    spyOn(Storage.prototype, 'getItem').and.throwError('Storage blocked');
    spyOn(Storage.prototype, 'setItem').and.throwError('Storage blocked');
    expect(() => render()).not.toThrow();
    toggle().click();
    fixture.detectChanges();
    expect(toggle().getAttribute('aria-expanded')).toBe('false');
    const theme = el().querySelector<HTMLButtonElement>('.theme-btn')!;
    expect(() => {
      theme.click();
      fixture.detectChanges();
    }).not.toThrow();
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    theme.click();
    fixture.detectChanges();
    expect(document.documentElement.getAttribute('data-theme')).toBeNull();
  });
});
