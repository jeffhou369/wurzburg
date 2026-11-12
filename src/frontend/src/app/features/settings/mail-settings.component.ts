import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';

type MailSettings = components['schemas']['MailSettings'];

@Component({
  selector: 'app-mail-settings',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div class="mail-settings">
      <h2 class="section-title">SMTP 與郵件通知設定</h2>
      <p class="section-desc">
        設定通知郵件發送伺服器、加密方式、系統管理員收件清單與每週未登記總表寄送排程。
      </p>

      <form class="form-grid" (ngSubmit)="save()">
        <div class="field-group full">
          <label for="smtpHost">SMTP 伺服器主機 *</label>
          <input
            id="smtpHost"
            name="smtpHost"
            type="text"
            required
            class="form-input"
            [(ngModel)]="form.smtp_host"
            placeholder="例如 smtp.example.com"
          />
        </div>

        <div class="field-group">
          <label for="smtpPort">埠號 (Port) *</label>
          <input
            id="smtpPort"
            name="smtpPort"
            type="number"
            min="1"
            max="65535"
            required
            class="form-input font-mono"
            [(ngModel)]="form.smtp_port"
          />
        </div>

        <div class="field-group">
          <label for="tlsMode">連線加密方式 *</label>
          <select id="tlsMode" name="tlsMode" class="form-input" [(ngModel)]="form.smtp_tls_mode">
            <option value="starttls">STARTTLS（預設，連接 587）</option>
            <option value="smtps">SMTPS（直連 SSL/TLS，連接 465）</option>
            <option value="none">無加密（僅限內部信任網路，連接 25）</option>
          </select>
        </div>

        <div class="field-group">
          <label for="smtpUser">認證帳號（若無需認證可留空）</label>
          <input
            id="smtpUser"
            name="smtpUser"
            type="text"
            class="form-input"
            [(ngModel)]="form.smtp_username"
            placeholder="例如 no-reply@example.com"
          />
        </div>

        <div class="field-group">
          <label for="smtpPass">認證密碼</label>
          <input
            id="smtpPass"
            name="smtpPass"
            type="password"
            class="form-input"
            [(ngModel)]="passwordInput"
            [placeholder]="form.has_password ? '已設定（保留原密碼請留空）' : '請輸入密碼'"
          />
        </div>

        <div class="field-group">
          <label for="senderAddress">寄件人信箱 (From) *</label>
          <input
            id="senderAddress"
            name="senderAddress"
            type="email"
            required
            class="form-input"
            [(ngModel)]="form.sender_address"
            placeholder="ipmgmt@example.com"
          />
        </div>

        <div class="field-group">
          <label for="weeklyCron">未登記總表寄送排程 (Cron) *</label>
          <input
            id="weeklyCron"
            name="weeklyCron"
            type="text"
            required
            class="form-input font-mono"
            [(ngModel)]="form.weekly_summary_cron"
            placeholder="0 9 * * 1"
          />
          <span class="field-help">預設每週一 09:00（園區時區），格式為 5 欄位標準 Cron</span>
        </div>

        <div class="field-group full">
          <label for="adminRecipients">系統管理員通知收件清單（每行一個信箱）*</label>
          <textarea
            id="adminRecipients"
            name="adminRecipients"
            rows="3"
            class="form-input font-mono"
            [(ngModel)]="recipientsText"
            placeholder="admin1@example.com&#10;admin2@example.com"
          ></textarea>
          <span class="field-help"
            >未登記彙整、未登記累積總表與到期催認最終提醒將寄送至此清單。</span
          >
        </div>

        <div class="actions">
          <button type="submit" class="btn btn-primary" [disabled]="saving()">
            {{ saving() ? '儲存中...' : '儲存郵件設定' }}
          </button>
        </div>
      </form>

      <div class="test-section">
        <h3 class="test-title">寄送測試信</h3>
        <p class="test-desc">
          寄送一封驗證信件至指定信箱，確認 SMTP 連線與 HTML/純文字排版格式正常。
        </p>

        <div class="test-form">
          <input
            type="email"
            class="form-input"
            placeholder="請輸入測試收件人信箱"
            [(ngModel)]="testRecipient"
            [disabled]="testingMail()"
          />
          <button
            type="button"
            class="btn btn-secondary"
            (click)="sendTestMail()"
            [disabled]="testingMail() || !testRecipient.trim()"
          >
            {{ testingMail() ? '寄送中...' : '寄出測試信' }}
          </button>
        </div>

        @if (testResult()) {
          <div
            class="test-result"
            [class.success]="testResult()!.success"
            [class.error]="!testResult()!.success"
          >
            <strong>{{ testResult()!.success ? '寄送成功' : '寄送失敗' }}：</strong>
            {{ testResult()!.message }}
          </div>
        }
      </div>
    </div>
  `,
  styles: [
    `
      .section-title {
        margin: 0 0 6px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
      }
      .section-desc {
        margin: 0 0 20px 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
      .form-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
        gap: 16px;
      }
      .field-group {
        display: flex;
        flex-direction: column;
        gap: 6px;
        &.full {
          grid-column: 1 / -1;
        }
        label {
          font-size: var(--font-size-meta);
          font-weight: 500;
        }
      }
      .field-help {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .actions {
        grid-column: 1 / -1;
        display: flex;
        justify-content: flex-end;
        padding-top: 8px;
      }
      .test-section {
        margin-top: 32px;
        padding-top: 24px;
        border-top: 1px solid var(--color-border-default);
      }
      .test-title {
        margin: 0 0 6px 0;
        font-size: var(--font-size-body);
        font-weight: 600;
      }
      .test-desc {
        margin: 0 0 16px 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
      .test-form {
        display: flex;
        gap: 12px;
        max-width: 520px;
        margin-bottom: 12px;
        input {
          flex: 1;
        }
      }
      .test-result {
        max-width: 520px;
        padding: 10px 14px;
        border-radius: var(--radius-md);
        font-size: var(--font-size-meta);
        line-height: 1.4;
        &.success {
          background-color: var(--color-online-bg);
          color: var(--color-online);
          border: 1px solid var(--color-online-border);
        }
        &.error {
          background-color: var(--color-conflict-bg);
          color: var(--color-conflict);
          border: 1px solid var(--color-conflict-border);
        }
      }
    `,
  ],
})
export class MailSettingsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);

  protected readonly saving = signal(false);
  protected readonly testingMail = signal(false);
  protected readonly testResult = signal<{ success: boolean; message: string } | null>(null);

  protected passwordInput = '';
  protected recipientsText = '';
  protected testRecipient = '';

  protected form: MailSettings = {
    smtp_host: '',
    smtp_port: 587,
    smtp_tls_mode: 'starttls',
    smtp_username: '',
    has_password: false,
    sender_address: '',
    admin_recipients: [],
    weekly_summary_cron: '0 9 * * 1',
  };

  ngOnInit(): void {
    this.http.get<MailSettings>('/api/v1/settings/mail').subscribe({
      next: (res) => {
        this.form = res;
        this.recipientsText = (res.admin_recipients ?? []).join('\n');
      },
      error: () => {},
    });
  }

  save(): void {
    this.saving.set(true);
    const rcpts = this.recipientsText
      .split('\n')
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

    const payload: Record<string, unknown> = {
      smtp_host: this.form.smtp_host,
      smtp_port: this.form.smtp_port,
      smtp_tls_mode: this.form.smtp_tls_mode,
      smtp_username: this.form.smtp_username,
      sender_address: this.form.sender_address,
      admin_recipients: rcpts,
      weekly_summary_cron: this.form.weekly_summary_cron,
    };
    if (this.passwordInput) {
      payload['smtp_password'] = this.passwordInput;
    }

    this.http
      .put<MailSettings>('/api/v1/settings/mail', payload, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (res) => {
          this.form = res;
          this.passwordInput = '';
          this.saving.set(false);
          this.toast.show('郵件設定已儲存', 'success');
        },
        error: (err) => {
          this.saving.set(false);
          this.toast.show('儲存失敗', 'danger', err?.error?.message ?? '請檢查輸入欄位');
        },
      });
  }

  sendTestMail(): void {
    if (!this.testRecipient.trim()) return;
    this.testingMail.set(true);
    this.testResult.set(null);

    this.http
      .post<{ success: boolean; message: string }>(
        '/api/v1/settings/mail/test',
        { recipient: this.testRecipient.trim() },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          this.testingMail.set(false);
          this.testResult.set(res);
          if (res.success) {
            this.toast.show('測試信已寄出', 'success');
          } else {
            this.toast.show('寄送失敗', 'danger', res.message);
          }
        },
        error: (err) => {
          this.testingMail.set(false);
          const msg = err?.error?.message ?? '測試信寄送失敗，請確認 SMTP 設定與網路連線';
          this.testResult.set({ success: false, message: msg });
          this.toast.show('寄送失敗', 'danger', msg);
        },
      });
  }
}
