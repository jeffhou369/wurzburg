import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Router } from '@angular/router';
import { CurrentUser } from './auth.service';
import { sanitizeReturnUrl } from './safe-return-url';

/**
 * 登入／改密碼成功後的導向規則，集中在這裡避免兩個頁面各自實作而不一致。
 *
 * 規則（目的地一律先經 sanitizeReturnUrl）：
 * 1. 需要強制改密碼：前往 /change-password，並攜帶目的地。
 * 2. 系統管理員且設定精靈尚未完成：前往 /?wizard=true，並攜帶目的地，精靈結束後再回到目的地。
 * 3. 其他情況：直接前往目的地（預設為首頁）。
 */
@Injectable({ providedIn: 'root' })
export class AuthRedirectService {
  private readonly http = inject(HttpClient);
  private readonly router = inject(Router);

  navigateAfterAuth(user: CurrentUser, rawReturnUrl: unknown): void {
    const destination = sanitizeReturnUrl(rawReturnUrl);

    if (user.requiresPasswordChange) {
      void this.router.navigateByUrl(this.withReturnUrl('/change-password', destination));
      return;
    }

    if (user.role === 'admin') {
      this.http.get<{ completed: boolean }>('/api/v1/settings/wizard').subscribe({
        next: (wizard) => {
          const target = wizard.completed
            ? destination
            : this.withReturnUrl('/?wizard=true', destination);
          void this.router.navigateByUrl(target);
        },
        error: () => void this.router.navigateByUrl(destination),
      });
      return;
    }

    void this.router.navigateByUrl(destination);
  }

  /** 目的地為首頁時不需攜帶 returnUrl。 */
  private withReturnUrl(base: string, destination: string): string {
    if (destination === '/') {
      return base;
    }
    const separator = base.includes('?') ? '&' : '?';
    return `${base}${separator}returnUrl=${encodeURIComponent(destination)}`;
  }
}
