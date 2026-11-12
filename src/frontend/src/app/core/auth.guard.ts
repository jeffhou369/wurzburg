import { inject } from '@angular/core';
import { CanActivateFn, Router, UrlTree } from '@angular/router';
import { map } from 'rxjs';
import { AuthService } from './auth.service';
import { returnUrlTree } from './safe-return-url';

/**
 * Shell 路由守衛：確認有有效工作階段才放行。
 *
 * - 尚未登入（/auth/me 回 401 或失敗）：導向 /login?returnUrl=原站內完整路徑與查詢字串。
 * - 需要強制改密碼：導向 /change-password 並同樣保留目的地。
 * 授權仍由後端 API 逐筆判斷，這裡只負責導覽流程。
 */
export const authGuard: CanActivateFn = (_route, state) => {
  const auth = inject(AuthService);
  const router = inject(Router);

  return auth.ensureSession().pipe(
    map((user): boolean | UrlTree => {
      if (!user) {
        return returnUrlTree(router, '/login', state.url);
      }
      if (user.requiresPasswordChange) {
        return returnUrlTree(router, '/change-password', state.url);
      }
      return true;
    }),
  );
};
