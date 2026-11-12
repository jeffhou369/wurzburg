import { Injectable, signal } from '@angular/core';

export type UserRole = 'admin' | 'user';

export interface CurrentUser {
  id: string;
  username: string;
  displayName: string;
  role: UserRole;
  authSource: 'local' | 'ad' | 'radius';
}

@Injectable({
  providedIn: 'root',
})
export class AuthService {
  // 預設為系統管理員身分，以利展示完整功能；後續工作項目由後端 API 動態賦值
  readonly currentUser = signal<CurrentUser>({
    id: 'u_admin',
    username: 'admin',
    displayName: '系統管理員',
    role: 'admin',
    authSource: 'local',
  });

  isAdmin(): boolean {
    return this.currentUser()?.role === 'admin';
  }

  setRole(role: UserRole): void {
    this.currentUser.update((u) => ({ ...u, role }));
  }
}
