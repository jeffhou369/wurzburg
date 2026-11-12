import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, catchError, map, of, tap } from 'rxjs';
import type { components } from './api.types';

export type UserRole = 'admin' | 'user';

export type ApiCurrentUser = components['schemas']['CurrentUser'];

export interface CurrentUser {
  id: string;
  username: string;
  displayName: string;
  role: UserRole;
  authSource: 'local' | 'ad' | 'radius';
  mail?: string;
  requiresPasswordChange: boolean;
}

/** 把後端 CurrentUser（snake_case）轉為前端使用的結構。 */
export function toCurrentUser(api: ApiCurrentUser): CurrentUser {
  const user: CurrentUser = {
    id: String(api.id),
    username: api.username,
    displayName: api.display_name,
    role: api.role,
    authSource: api.auth_source,
    requiresPasswordChange: api.requires_password_change === true,
  };
  if (api.mail) {
    user.mail = api.mail;
  }
  return user;
}

@Injectable({
  providedIn: 'root',
})
export class AuthService {
  private readonly http = inject(HttpClient);

  /** 尚未登入或工作階段尚未還原時為 null；絕不預設為任何身分。 */
  readonly currentUser = signal<CurrentUser | null>(null);
  readonly isAuthenticated = computed(() => this.currentUser() !== null);

  isAdmin(): boolean {
    return this.currentUser()?.role === 'admin';
  }

  /**
   * 以既有的 GET /api/v1/auth/me 還原工作階段。
   * 401 或任何錯誤都視為未登入（失敗即拒絕），並清除殘留的使用者。
   */
  restoreSession(): Observable<CurrentUser | null> {
    return this.http.get<ApiCurrentUser>('/api/v1/auth/me').pipe(
      map((api) => toCurrentUser(api)),
      tap((user) => this.currentUser.set(user)),
      catchError(() => {
        this.currentUser.set(null);
        return of(null);
      }),
    );
  }

  /** 已有使用者就直接回傳，否則向後端還原。 */
  ensureSession(): Observable<CurrentUser | null> {
    const existing = this.currentUser();
    return existing ? of(existing) : this.restoreSession();
  }

  /** 僅在已有使用者時調整角色（展示頁用）；未登入時不會憑空建立使用者。 */
  setRole(role: UserRole): void {
    this.currentUser.update((u) => (u ? { ...u, role } : u));
  }

  /** 改密碼成功後標記不再需要強制變更。 */
  markPasswordChanged(): void {
    this.currentUser.update((u) => (u ? { ...u, requiresPasswordChange: false } : u));
  }
}
