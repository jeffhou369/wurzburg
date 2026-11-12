import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { RETENTION_TEXT } from './settings.i18n';

type RetentionPolicy = components['schemas']['RetentionPolicySettings'];

@Component({
  selector: 'app-retention-settings',
  standalone: true,
  imports: [FormsModule],
  template: `
    <div class="retention-section">
      <h2 class="section-title">{{ text.title }}</h2>
      <p class="section-desc">
        {{ text.description }}
      </p>

      <form class="form-grid" (ngSubmit)="save()">
        <div class="field-group">
          <label for="autoPurgeDays">{{ text.autoPurgeDays }}</label>
          <input
            id="autoPurgeDays"
            name="autoPurgeDays"
            type="number"
            min="0"
            class="form-input"
            [(ngModel)]="form.auto_purge_days"
          />
          <span class="field-help">{{ text.autoPurgeDaysHelp }}</span>
        </div>

        <div class="field-group">
          <label for="graceRounds">{{ text.graceRounds }}</label>
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
          <label for="validDays">{{ text.validDays }}</label>
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
          <label for="advanceDays">{{ text.advanceDays }}</label>
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
          <label for="intervalDays">{{ text.intervalDays }}</label>
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
          <label for="maxReminders">{{ text.maxReminders }}</label>
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
            {{ saving() ? actions.saving : text.save }}
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
  protected readonly text = RETENTION_TEXT;
  protected readonly actions = SHELL_TEXT.actions;

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
}
