import { Component, OnInit, inject, signal, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { LucideWand2 } from '@lucide/angular';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';

type CampusSettings = components['schemas']['CampusSettings'];

@Component({
  selector: 'app-campus-settings',
  standalone: true,
  imports: [FormsModule, LucideWand2],
  template: `
    <div class="campus-settings">
      <div class="header-row">
        <div>
          <h2 class="section-title">園區基本資訊與設定精靈</h2>
          <p class="section-desc">
            設定園區識別名稱與顯示時區；名稱將用於側欄標頭與信件主旨，時區用於全系統時間顯示與排程。
          </p>
        </div>
        <button type="button" class="btn btn-secondary" (click)="openWizard.emit()">
          <svg lucideWand2 [size]="16" [strokeWidth]="2"></svg>
          <span>重新開啟設定精靈</span>
        </button>
      </div>

      <form class="form-grid" (ngSubmit)="save()">
        <div class="field-group">
          <label for="campusName">園區顯示名稱 *</label>
          <input
            id="campusName"
            name="campusName"
            type="text"
            required
            class="form-input"
            [(ngModel)]="form.campus_name"
            placeholder="例如 Würzburg 或 新竹二廠"
          />
          <span class="field-help">用於側欄標題、首頁工作臺與通知信件主旨之系統識別。</span>
        </div>

        <div class="field-group">
          <label for="timezone">園區顯示時區 (IANA Timezone) *</label>
          <input
            id="timezone"
            name="timezone"
            type="text"
            required
            class="form-input font-mono"
            [(ngModel)]="form.timezone"
            placeholder="Asia/Taipei"
          />
          <span class="field-help"
            >例如 Asia/Taipei、UTC 或 America/New_York。資料庫一律儲存 UTC。</span
          >
        </div>

        <div class="actions">
          <button type="submit" class="btn btn-primary" [disabled]="saving()">
            {{ saving() ? '儲存中...' : '儲存園區資訊' }}
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
export class CampusSettingsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);

  readonly openWizard = output<void>();

  protected readonly saving = signal(false);
  protected form: CampusSettings = {
    campus_name: 'Würzburg',
    timezone: 'Asia/Taipei',
  };

  ngOnInit(): void {
    this.http.get<CampusSettings>('/api/v1/settings/campus').subscribe({
      next: (res) => (this.form = res),
      error: () => {},
    });
  }

  save(): void {
    if (!this.form.campus_name.trim() || !this.form.timezone.trim()) return;
    this.saving.set(true);
    this.http
      .put<CampusSettings>('/api/v1/settings/campus', this.form, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (res) => {
          this.form = res;
          this.saving.set(false);
          this.toast.show('園區設定已儲存', 'success');
          // 更新 document title 與頁面顯示
          document.title = `[${res.campus_name}] IP 管理系統`;
        },
        error: (err) => {
          this.saving.set(false);
          this.toast.show('儲存失敗', 'danger', err?.error?.message ?? '請確認時區格式合法');
        },
      });
  }
}
