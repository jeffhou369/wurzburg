import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { MAIL_TEXT } from './mail-settings.i18n';

type MailSettings = components['schemas']['MailSettings'];

@Component({
  selector: 'app-mail-settings',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div class="mail-settings">
      <h2 class="section-title">{{ text.title }}</h2>
      <p class="section-desc">
        {{ text.description }}
      </p>

      <form class="form-grid" (ngSubmit)="save()">
        <div class="field-group full">
          <label for="smtpHost">{{ text.smtpHost.label }}</label>
          <input
            id="smtpHost"
            name="smtpHost"
            type="text"
            required
            class="form-input"
            [(ngModel)]="form.smtp_host"
            [placeholder]="text.smtpHost.placeholder"
          />
        </div>

        <div class="field-group">
          <label for="smtpPort">{{ text.smtpPort }}</label>
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
          <label for="tlsMode">{{ text.tlsMode.label }}</label>
          <select id="tlsMode" name="tlsMode" class="form-input" [(ngModel)]="form.smtp_tls_mode">
            <option value="starttls">{{ text.tlsMode.starttls }}</option>
            <option value="smtps">{{ text.tlsMode.smtps }}</option>
            <option value="none">{{ text.tlsMode.none }}</option>
          </select>
        </div>

        <div class="field-group">
          <label for="smtpUser">{{ text.smtpUser.label }}</label>
          <input
            id="smtpUser"
            name="smtpUser"
            type="text"
            class="form-input"
            [(ngModel)]="form.smtp_username"
            [placeholder]="text.smtpUser.placeholder"
          />
        </div>

        <div class="field-group">
          <label for="smtpPass">{{ text.smtpPass.label }}</label>
          <input
            id="smtpPass"
            name="smtpPass"
            type="password"
            class="form-input"
            [(ngModel)]="passwordInput"
            [placeholder]="
              form.has_password ? text.smtpPass.placeholderKeep : text.smtpPass.placeholderNew
            "
          />
        </div>

        <div class="field-group">
          <label for="senderAddress">{{ text.senderAddress }}</label>
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
          <label for="weeklyCron">{{ text.weeklyCron.label }}</label>
          <input
            id="weeklyCron"
            name="weeklyCron"
            type="text"
            required
            class="form-input font-mono"
            [(ngModel)]="form.weekly_summary_cron"
            placeholder="0 9 * * 1"
          />
          <span class="field-help">{{ text.weeklyCron.help }}</span>
        </div>

        <div class="field-group full">
          <label for="adminRecipients">{{ text.adminRecipients.label }}</label>
          <textarea
            id="adminRecipients"
            name="adminRecipients"
            rows="3"
            class="form-input font-mono"
            [(ngModel)]="recipientsText"
            placeholder="admin1@example.com&#10;admin2@example.com"
          ></textarea>
          <span class="field-help">{{ text.adminRecipients.help }}</span>
        </div>

        <div class="actions">
          <button type="submit" class="btn btn-primary" [disabled]="saving()">
            {{ saving() ? actions.saving : text.save }}
          </button>
        </div>
      </form>

      <div class="test-section">
        <h3 class="test-title">{{ text.test.title }}</h3>
        <p class="test-desc">
          {{ text.test.description }}
        </p>

        <div class="test-form">
          <input
            type="email"
            class="form-input"
            [placeholder]="text.test.recipientPlaceholder"
            [(ngModel)]="testRecipient"
            [disabled]="testingMail()"
          />
          <button
            type="button"
            class="btn btn-secondary"
            (click)="sendTestMail()"
            [disabled]="testingMail() || !testRecipient.trim()"
          >
            {{ testingMail() ? text.test.sending : text.test.send }}
          </button>
        </div>

        @if (testResult()) {
          <div
            class="test-result"
            [class.success]="testResult()!.success"
            [class.error]="!testResult()!.success"
          >
            <strong>{{ text.test.resultLabel(testResult()!.success) }}</strong>
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
          background-color: var(--color-success-subtle);
          color: var(--color-success-text);
          border: 1px solid var(--color-success-border);
        }
        &.error {
          background-color: var(--color-danger-subtle);
          color: var(--color-danger-text);
          border: 1px solid var(--color-danger-border);
        }
      }
    `,
  ],
})
export class MailSettingsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);
  protected readonly text = MAIL_TEXT;
  protected readonly actions = SHELL_TEXT.actions;

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
          this.toast.show(this.text.saved, 'success');
        },
        error: (err) => {
          this.saving.set(false);
          this.toast.show(
            this.text.saveFailed,
            'danger',
            err?.error?.message ?? this.text.saveFailedHint,
          );
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
            this.toast.show(this.text.test.sent, 'success');
          } else {
            this.toast.show(this.text.test.failed, 'danger', res.message);
          }
        },
        error: (err) => {
          this.testingMail.set(false);
          const msg = err?.error?.message ?? this.text.test.failedHint;
          this.testResult.set({ success: false, message: msg });
          this.toast.show(this.text.test.failed, 'danger', msg);
        },
      });
  }
}
