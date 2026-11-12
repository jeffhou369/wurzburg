import { Component, inject, signal, OnInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { Router } from '@angular/router';
import { LucideShieldAlert, LucideBuilding2, LucideKeyRound } from '@lucide/angular';
import { AUTH_TEXT } from './auth.i18n';
import { AuthService } from '../../core/auth.service';

@Component({
  selector: 'app-login-page',
  standalone: true,
  imports: [FormsModule, LucideShieldAlert, LucideBuilding2, LucideKeyRound],
  template: `
    <div class="login-backdrop">
      <main class="login-card card">
        <header class="login-header">
          <div class="brand-mark" aria-hidden="true">W</div>
          <div>
            <h1 class="login-title">{{ text.title }}</h1>
            <p class="login-subtitle">{{ text.subtitle }}</p>
          </div>
        </header>

        @if (isPlaintextHttp()) {
          <div class="http-warning" role="alert">
            <svg lucideShieldAlert [size]="20" [strokeWidth]="2" class="warn-icon"></svg>
            <div>
              <div class="warn-title">{{ text.plaintextWarningTitle }}</div>
              <div class="warn-desc">{{ text.plaintextWarningDesc }}</div>
            </div>
          </div>
        }

        <div class="login-tabs" role="tablist">
          <button
            type="button"
            role="tab"
            class="login-tab"
            [class.active]="mode() === 'org'"
            [attr.aria-selected]="mode() === 'org'"
            (click)="switchMode('org')"
          >
            <svg lucideBuilding2 [size]="16" [strokeWidth]="2"></svg>
            <span>{{ text.tabs.org }}</span>
          </button>
          <button
            type="button"
            role="tab"
            class="login-tab"
            [class.active]="mode() === 'local'"
            [attr.aria-selected]="mode() === 'local'"
            (click)="switchMode('local')"
          >
            <svg lucideKeyRound [size]="16" [strokeWidth]="2"></svg>
            <span>{{ text.tabs.local }}</span>
          </button>
        </div>

        <form class="login-form" (ngSubmit)="submit()">
          @if (errorMsg()) {
            <div class="error-banner" role="alert">{{ errorMsg() }}</div>
          }

          <div class="field-group">
            <label for="username">{{ text.fields.username }}</label>
            <input
              id="username"
              name="username"
              type="text"
              class="form-input"
              [(ngModel)]="username"
              required
              autocomplete="username"
            />
          </div>

          <div class="field-group">
            <label for="password">{{ text.fields.password }}</label>
            <input
              id="password"
              name="password"
              type="password"
              class="form-input"
              [(ngModel)]="password"
              required
              autocomplete="current-password"
            />
          </div>

          <button type="submit" class="btn btn-primary submit-btn" [disabled]="submitting()">
            {{ mode() === 'org' ? text.buttons.loginOrg : text.buttons.loginLocal }}
          </button>
        </form>
      </main>
    </div>
  `,
  styles: [
    `
      .login-backdrop {
        min-height: 100vh;
        display: flex;
        align-items: center;
        justify-content: center;
        background-color: var(--sidebar-bg);
        padding: 20px;
      }

      .login-card {
        width: 100%;
        max-width: 440px;
        padding: 32px;
      }

      .login-header {
        display: flex;
        align-items: center;
        gap: 14px;
        margin-bottom: 20px;
      }

      .brand-mark {
        width: 40px;
        height: 40px;
        border-radius: var(--radius-md);
        background-color: var(--color-accent);
        color: var(--color-text-on-accent);
        display: flex;
        align-items: center;
        justify-content: center;
        font-weight: 700;
        font-size: 18px;
      }

      .login-title {
        margin: 0;
        font-size: var(--font-size-dialog-title);
        font-weight: 600;
      }

      .login-subtitle {
        margin: 2px 0 0 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }

      .http-warning {
        display: flex;
        align-items: flex-start;
        gap: 10px;
        background-color: var(--color-warning-subtle);
        border: 1px solid var(--color-warning-border);
        color: var(--color-warning-text);
        border-radius: var(--radius-md);
        padding: 12px;
        margin-bottom: 20px;
        font-size: var(--font-size-meta);
      }

      .warn-icon {
        flex-shrink: 0;
        margin-top: 2px;
      }

      .warn-title {
        font-weight: 600;
        margin-bottom: 2px;
      }

      .login-tabs {
        display: grid;
        grid-template-columns: 1fr 1fr;
        gap: 6px;
        background-color: var(--color-surface-subtle);
        padding: 4px;
        border-radius: var(--radius-md);
        margin-bottom: 20px;
      }

      .login-tab {
        display: flex;
        align-items: center;
        justify-content: center;
        gap: 6px;
        border: none;
        background: transparent;
        padding: 8px 10px;
        border-radius: 6px;
        font-size: var(--font-size-caption);
        font-weight: 500;
        color: var(--color-text-muted);
        cursor: pointer;

        &.active {
          background-color: var(--color-surface);
          color: var(--color-text-primary);
          font-weight: 600;
          box-shadow: var(--shadow-xs);
        }
      }

      .login-form {
        display: flex;
        flex-direction: column;
        gap: 16px;
      }

      .field-group {
        display: flex;
        flex-direction: column;
        gap: 6px;

        label {
          font-size: var(--font-size-meta);
          font-weight: 500;
          color: var(--color-text-secondary);
        }
      }

      .error-banner {
        background-color: var(--color-danger-subtle);
        border: 1px solid var(--color-danger-border);
        color: var(--color-danger-text);
        border-radius: var(--radius-md);
        padding: 10px 12px;
        font-size: var(--font-size-meta);
      }

      .submit-btn {
        width: 100%;
        margin-top: 4px;
      }
    `,
  ],
})
export class LoginPage implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly router = inject(Router);
  private readonly auth = inject(AuthService);

  protected readonly text = AUTH_TEXT;
  protected readonly mode = signal<'org' | 'local'>('org');
  protected readonly isPlaintextHttp = signal(false);
  protected readonly submitting = signal(false);
  protected readonly errorMsg = signal<string | null>(null);

  protected username = '';
  protected password = '';

  ngOnInit(): void {
    if (typeof window !== 'undefined') {
      this.isPlaintextHttp.set(window.location.protocol !== 'https:');
    }
  }

  switchMode(m: 'org' | 'local'): void {
    this.mode.set(m);
    this.errorMsg.set(null);
    this.password = '';
  }

  submit(): void {
    if (!this.username || !this.password) {
      return;
    }
    this.submitting.set(true);
    this.errorMsg.set(null);

    const endpoint =
      this.mode() === 'local' ? '/api/v1/auth/login/local' : '/api/v1/auth/login/org';

    this.http
      .post<{
        user: {
          id: number;
          username: string;
          display_name: string;
          role: 'admin' | 'user';
          auth_source: 'local' | 'ad' | 'radius';
          requires_password_change?: boolean;
        };
      }>(
        endpoint,
        { username: this.username, password: this.password },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          this.auth.currentUser.set({
            id: String(res.user.id),
            username: res.user.username,
            displayName: res.user.display_name,
            role: res.user.role,
            authSource: res.user.auth_source,
          });
          this.submitting.set(false);
          if (res.user.requires_password_change) {
            void this.router.navigateByUrl('/change-password');
          } else {
            void this.router.navigateByUrl('/');
          }
        },
        error: (err) => {
          this.submitting.set(false);
          this.errorMsg.set(err?.error?.message ?? '登入失敗，請檢查帳號密碼');
        },
      });
  }
}
