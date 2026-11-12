import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { Router } from '@angular/router';
import { AUTH_TEXT } from './auth.i18n';

@Component({
  selector: 'app-change-password-page',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div class="login-backdrop">
      <main class="login-card card">
        <header class="card-header">
          <h1 class="title">{{ text.changePassword.title }}</h1>
          <p class="subtitle">{{ text.changePassword.subtitle }}</p>
        </header>

        <form class="form-stack" (ngSubmit)="submit()">
          @if (errorMsg()) {
            <div class="error-banner" role="alert">{{ errorMsg() }}</div>
          }

          <div class="field-group">
            <label for="newPw">{{ text.fields.newPassword }}</label>
            <input
              id="newPw"
              name="newPw"
              type="password"
              class="form-input"
              [(ngModel)]="newPassword"
              required
              minlength="12"
            />
          </div>

          <div class="field-group">
            <label for="confirmPw">{{ text.fields.confirmPassword }}</label>
            <input
              id="confirmPw"
              name="confirmPw"
              type="password"
              class="form-input"
              [(ngModel)]="confirmPassword"
              required
            />
          </div>

          <button type="submit" class="btn btn-primary" [disabled]="submitting()">
            {{ text.buttons.changePassword }}
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
      .card-header {
        margin-bottom: 20px;
      }
      .title {
        margin: 0 0 6px 0;
        font-size: var(--font-size-dialog-title);
        font-weight: 600;
      }
      .subtitle {
        margin: 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
      .form-stack {
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
    `,
  ],
})
export class ChangePasswordPage {
  private readonly http = inject(HttpClient);
  private readonly router = inject(Router);
  protected readonly text = AUTH_TEXT;
  protected readonly submitting = signal(false);
  protected readonly errorMsg = signal<string | null>(null);

  protected newPassword = '';
  protected confirmPassword = '';

  submit(): void {
    this.errorMsg.set(null);
    if (this.newPassword.length < 12) {
      this.errorMsg.set(this.text.changePassword.lengthError);
      return;
    }
    if (this.newPassword !== this.confirmPassword) {
      this.errorMsg.set(this.text.changePassword.mismatchError);
      return;
    }

    this.submitting.set(true);
    this.http
      .post(
        '/api/v1/auth/change-password',
        { new_password: this.newPassword },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: () => {
          this.submitting.set(false);
          void this.router.navigateByUrl('/?wizard=true');
        },
        error: (err) => {
          this.submitting.set(false);
          this.errorMsg.set(err?.error?.message ?? '密碼更新失敗');
        },
      });
  }
}
