import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';

type RetentionPolicy = components['schemas']['RetentionPolicySettings'];

@Component({
  selector: 'app-retention-settings',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div class="retention-section">
      <h2 class="section-title">自動清除與保存天數政策</h2>
      <p class="section-desc">
        設定離線判定輪數、未登記離線紀錄自動清除天數，以及登記有效期與到期催認規則。
      </p>

      <form class="form-grid" (ngSubmit)="save()">
        <div class="field-group">
          <label for="autoPurgeDays">未登記離線紀錄自動清除天數（0 代表停用）</label>
          <input
            id="autoPurgeDays"
            name="autoPurgeDays"
            type="number"
            min="0"
            class="form-input"
            [(ngModel)]="form.auto_purge_days"
          />
          <span class="field-help">已登記或已到期的 IP 紀錄不論離線多久皆不會被自動清除。</span>
        </div>

        <div class="field-group">
          <label for="graceRounds">離線判定寬限輪數</label>
          <input
            id="graceRounds"
            name="graceRounds"
            type="number"
            min="1"
            class="form-input"
            [(ngModel)]="form.offline_grace_rounds"
          />
        </div>

        <div class="field-group">
          <label for="validDays">登記有效天數</label>
          <input
            id="validDays"
            name="validDays"
            type="number"
            min="1"
            class="form-input"
            [(ngModel)]="form.registration_valid_days"
          />
        </div>

        <div class="field-group">
          <label for="advanceDays">到期前提前提醒天數</label>
          <input
            id="advanceDays"
            name="advanceDays"
            type="number"
            min="0"
            class="form-input"
            [(ngModel)]="form.expiry_advance_days"
          />
        </div>

        <div class="field-group">
          <label for="intervalDays">逾期後復催間隔天數</label>
          <input
            id="intervalDays"
            name="intervalDays"
            type="number"
            min="1"
            class="form-input"
            [(ngModel)]="form.expiry_reminder_interval_days"
          />
        </div>

        <div class="field-group">
          <label for="maxReminders">復催次數上限</label>
          <input
            id="maxReminders"
            name="maxReminders"
            type="number"
            min="0"
            class="form-input"
            [(ngModel)]="form.expiry_max_reminders"
          />
        </div>

        <div class="actions">
          <button type="submit" class="btn btn-primary" [disabled]="saving()">
            {{ saving() ? '儲存中...' : '儲存政策設定' }}
          </button>
        </div>
      </form>
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
        grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
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
    `,
  ],
})
export class RetentionSettingsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);

  protected readonly saving = signal(false);
  protected form: RetentionPolicy = {
    offline_grace_rounds: 2,
    auto_purge_days: 90,
    registration_valid_days: 365,
    expiry_advance_days: 14,
    expiry_reminder_interval_days: 7,
    expiry_max_reminders: 4,
  };

  ngOnInit(): void {
    this.http.get<RetentionPolicy>('/api/v1/settings/retention').subscribe({
      next: (res) => (this.form = res),
      error: () => {},
    });
  }

  save(): void {
    this.saving.set(true);
    this.http
      .put<RetentionPolicy>('/api/v1/settings/retention', this.form, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (res) => {
          this.form = res;
          this.saving.set(false);
          this.toast.show('政策已更新', 'success');
        },
        error: (err) => {
          this.saving.set(false);
          this.toast.show('儲存失敗', 'danger', err?.error?.message ?? '請檢查輸入數值');
        },
      });
  }
}
