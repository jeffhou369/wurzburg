import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { AuthService } from './auth.service';

describe('AuthService', () => {
  let auth: AuthService;
  let httpTesting: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    auth = TestBed.inject(AuthService);
    httpTesting = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpTesting.verify();
  });

  it('初始狀態不應有任何使用者，也不是系統管理員', () => {
    expect(auth.currentUser()).toBeNull();
    expect(auth.isAdmin()).toBeFalse();
    expect(auth.isAuthenticated()).toBeFalse();
  });

  it('未登入時 setRole 不應憑空建立使用者', () => {
    auth.setRole('admin');
    expect(auth.currentUser()).toBeNull();
    expect(auth.isAdmin()).toBeFalse();
  });

  it('restoreSession 應呼叫 GET /api/v1/auth/me 並還原管理員身分', () => {
    let result: unknown = 'pending';
    auth.restoreSession().subscribe((u) => (result = u));

    const req = httpTesting.expectOne('/api/v1/auth/me');
    expect(req.request.method).toBe('GET');
    req.flush({
      id: 7,
      username: 'admin',
      display_name: '系統管理員',
      mail: 'admin@example.test',
      role: 'admin',
      auth_source: 'local',
      requires_password_change: false,
    });

    expect(result).toEqual({
      id: '7',
      username: 'admin',
      displayName: '系統管理員',
      mail: 'admin@example.test',
      role: 'admin',
      authSource: 'local',
      requiresPasswordChange: false,
    });
    expect(auth.currentUser()?.role).toBe('admin');
    expect(auth.isAdmin()).toBeTrue();
    expect(auth.isAuthenticated()).toBeTrue();
  });

  it('restoreSession 還原一般使用者時 isAdmin 應為 false', () => {
    auth.restoreSession().subscribe();
    httpTesting.expectOne('/api/v1/auth/me').flush({
      id: 12,
      username: 'alice',
      display_name: 'Alice',
      role: 'user',
      auth_source: 'ad',
    });
    expect(auth.currentUser()?.displayName).toBe('Alice');
    expect(auth.currentUser()?.requiresPasswordChange).toBeFalse();
    expect(auth.isAdmin()).toBeFalse();
  });

  it('restoreSession 遇到 401 應回傳 null 並保持未登入', () => {
    let result: unknown = 'pending';
    auth.restoreSession().subscribe((u) => (result = u));
    httpTesting
      .expectOne('/api/v1/auth/me')
      .flush({ code: 'unauthorized' }, { status: 401, statusText: 'Unauthorized' });

    expect(result).toBeNull();
    expect(auth.currentUser()).toBeNull();
  });

  it('restoreSession 遇到伺服器錯誤時以未登入處理，不可誤判為已登入', () => {
    let result: unknown = 'pending';
    auth.restoreSession().subscribe((u) => (result = u));
    httpTesting
      .expectOne('/api/v1/auth/me')
      .flush('boom', { status: 500, statusText: 'Server Error' });

    expect(result).toBeNull();
    expect(auth.isAuthenticated()).toBeFalse();
  });

  it('restoreSession 遇到 401 應清除先前殘留的使用者', () => {
    auth.currentUser.set({
      id: '1',
      username: 'old',
      displayName: 'Old',
      role: 'admin',
      authSource: 'local',
      requiresPasswordChange: false,
    });
    auth.restoreSession().subscribe();
    httpTesting
      .expectOne('/api/v1/auth/me')
      .flush(null, { status: 401, statusText: 'Unauthorized' });
    expect(auth.currentUser()).toBeNull();
  });

  it('ensureSession 已有使用者時不應再發出請求', () => {
    auth.currentUser.set({
      id: '1',
      username: 'alice',
      displayName: 'Alice',
      role: 'user',
      authSource: 'ad',
      requiresPasswordChange: false,
    });
    let result: unknown = 'pending';
    auth.ensureSession().subscribe((u) => (result = u));
    httpTesting.expectNone('/api/v1/auth/me');
    expect((result as { username: string }).username).toBe('alice');
  });

  it('ensureSession 尚無使用者時應向 /auth/me 還原', () => {
    auth.ensureSession().subscribe();
    httpTesting.expectOne('/api/v1/auth/me').flush({
      id: 3,
      username: 'bob',
      display_name: 'Bob',
      role: 'user',
      auth_source: 'radius',
    });
    expect(auth.currentUser()?.username).toBe('bob');
  });

  it('setRole 在已登入時應只更新角色', () => {
    auth.currentUser.set({
      id: '1',
      username: 'alice',
      displayName: 'Alice',
      role: 'user',
      authSource: 'ad',
      requiresPasswordChange: false,
    });
    auth.setRole('admin');
    expect(auth.isAdmin()).toBeTrue();
    expect(auth.currentUser()?.username).toBe('alice');
  });
});
