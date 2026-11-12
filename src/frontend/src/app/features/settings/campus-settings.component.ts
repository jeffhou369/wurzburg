import { Component, OnInit, computed, inject, signal, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { LucideWand2 } from '@lucide/angular';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';
import { CAMPUS_TEXT } from './settings.i18n';
import { isValidPublicBaseUrl } from './public-base-url';

type CampusSettings = components['schemas']['CampusSettings'];

@Component({
  selector: 'app-campus-settings',
  standalone: true,
  imports: [FormsModule, LucideWand2],
  template: `
    <div class="campus-settings">
      <div class="header-row">
        <div>
          <h2 class="section-title">{{ text.title }}</h2>
          <p class="section-desc">{{ text.description }}</p>
        </div>
        <button type="button" class="btn btn-secondary" (click)="openWizard.emit()">
          <svg lucideWand2 [size]="16" [strokeWidth]="2"></svg>
          <span>{{ text.reopenWizard }}</span>
        </button>
      </div>

      <form class="form-grid" (ngSubmit)="save()" novalidate>
        <div class="field-group">
          <label for="campusName">{{ text.campusName.label }}</label>
          <input
            id="campusName"
            name="campusName"
            type="text"
            required
            class="form-input"
            [(ngModel)]="form.campus_name"
            [placeholder]="text.campusName.placeholder"
          />
          <span class="field-help">{{ text.campusName.help }}</span>
        </div>

        <div class="field-group">
          <label for="timezone">{{ text.timezone.label }}</label>
          <input
            id="timezone"
            name="timezone"
            type="text"
            required
            class="form-input font-mono"
            [(ngModel)]="form.timezone"
            [placeholder]="text.timezone.placeholder"
          />
          <span class="field-help">{{ text.timezone.help }}</span>
        </div>

        <div class="field-group field-wide">
          <label for="publicBaseUrl">{{ text.publicBaseUrl.label }}</label>
          <input
            id="publicBaseUrl"
            name="publicBaseUrl"
            type="text"
            inputmode="url"
            autocomplete="off"
            class="form-input font-mono"
            [class.invalid]="publicBaseUrlInvalid()"
            [attr.aria-invalid]="publicBaseUrlInvalid() ? 'true' : null"
            [attr.aria-describedby]="
              publicBaseUrlInvalid() ? 'publicBaseUrlHelp publicBaseUrlError' : 'publicBaseUrlHelp'
            "
            [ngModel]="publicBaseUrl()"
            (ngModelChange)="onPublicBaseUrlInput($event)"
            [placeholder]="text.publicBaseUrl.placeholder"
          />
          <span class="field-help" id="publicBaseUrlHelp">{{ text.publicBaseUrl.help }}</span>
          @if (publicBaseUrlInvalid()) {
            <span class="field-error" id="publicBaseUrlError" role="alert">{{
              text.publicBaseUrl.invalid
            }}</span>
          }
        </div>

        <div class="actions">
          <button type="submit" class="btn btn-primary" [disabled]="saving()">
            {{ saving() ? text.saving : text.save }}
          </button>
        </div>
      </form>
    </div>
  `,
  styles: [
    `
      .header-row {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        gap: 16px;
        margin-bottom: 20px;
        flex-wrap: wrap;
      }
      .section-title {
        margin: 0 0 6px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
      }
      .section-desc {
        margin: 0;
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
        label {
          font-size: var(--font-size-meta);
          font-weight: 500;
        }
      }
      .field-wide {
        grid-column: 1 / -1;
      }
      .field-help {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .field-error {
        font-size: var(--font-size-caption);
        color: var(--color-danger);
        font-weight: 500;
      }
      .form-input.invalid {
        border-color: var(--color-danger);
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
export class CampusSettingsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);

  protected readonly text = CAMPUS_TEXT;
  readonly openWizard = output<void>();

  protected readonly saving = signal(false);
  protected form: Pick<CampusSettings, 'campus_name' | 'timezone'> = {
    campus_name: 'Würzburg',
    timezone: 'Asia/Taipei',
  };

  protected readonly publicBaseUrl = signal('');
  /** 使用者按過儲存後才顯示錯誤，輸入途中不打擾；改動欄位後會即時重新判斷。 */
  private readonly submitted = signal(false);
  protected readonly publicBaseUrlInvalid = computed(
    () => this.submitted() && !isValidPublicBaseUrl(this.publicBaseUrl()),
  );
  /** 讀取成功或使用者親自編輯過，才允許在 PUT 帶上公開網址；否則省略（= 保留後端既有值）。 */
  private publicBaseUrlKnown = false;

  ngOnInit(): void {
    this.http.get<CampusSettings>('/api/v1/settings/campus').subscribe({
      next: (res) => {
        this.form = { campus_name: res.campus_name, timezone: res.timezone };
        this.publicBaseUrl.set(res.public_base_url ?? '');
        this.publicBaseUrlKnown = true;
      },
      error: () => {},
    });
  }

  protected onPublicBaseUrlInput(value: string): void {
    this.publicBaseUrl.set(value ?? '');
    this.publicBaseUrlKnown = true;
  }

  save(): void {
    this.submitted.set(true);
    if (!this.form.campus_name.trim() || !this.form.timezone.trim()) return;
    if (!isValidPublicBaseUrl(this.publicBaseUrl())) return;

    const body: CampusSettings = { ...this.form };
    if (this.publicBaseUrlKnown) {
      body.public_base_url = this.publicBaseUrl().trim();
    }
    this.saving.set(true);
    this.http
      .put<CampusSettings>('/api/v1/settings/campus', body, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (res) => {
          this.form = { campus_name: res.campus_name, timezone: res.timezone };
          this.publicBaseUrl.set(res.public_base_url ?? '');
          this.publicBaseUrlKnown = true;
          this.submitted.set(false);
          this.saving.set(false);
          this.toast.show(this.text.saved, 'success');
          // 更新 document title 與頁面顯示
          document.title = this.text.documentTitle(res.campus_name);
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
