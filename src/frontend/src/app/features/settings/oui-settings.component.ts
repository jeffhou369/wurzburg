import { Component, OnInit, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { LucideUpload } from '@lucide/angular';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';
import { OUI_TEXT } from './settings.i18n';

type OUIStatus = components['schemas']['OUIStatus'];

@Component({
  selector: 'app-oui-settings',
  standalone: true,
  imports: [LucideUpload],
  template: `
    <div class="oui-section">
      <h2 class="section-title">{{ text.title }}</h2>
      <p class="section-desc">{{ text.description }}</p>

      @if (status(); as st) {
        <dl class="info-grid">
          <div class="info-row">
            <dt>{{ text.status.version }}</dt>
            <dd class="font-mono">{{ st.version }}</dd>
          </div>
          <div class="info-row">
            <dt>{{ text.status.filename }}</dt>
            <dd class="font-mono">{{ st.filename }}</dd>
          </div>
          <div class="info-row">
            <dt>{{ text.status.entryCount }}</dt>
            <dd class="font-mono">{{ st.entry_count }}</dd>
          </div>
          <div class="info-row">
            <dt>{{ text.status.importedBy }}</dt>
            <dd>
              {{ st.imported_by }}{{ text.status.timeOpen
              }}<span class="font-mono">{{ st.imported_at }}</span
              >{{ text.status.timeClose }}
            </dd>
          </div>
        </dl>
      }

      <div class="upload-box">
        <label for="ouiFile" class="upload-label">{{ text.uploadLabel }}</label>
        <div class="upload-controls">
          <input id="ouiFile" type="file" accept=".csv,.txt" (change)="onFileSelected($event)" />
          <button
            type="button"
            class="btn btn-primary"
            [disabled]="!selectedFile() || uploading()"
            (click)="upload()"
          >
            <svg lucideUpload [size]="16" [strokeWidth]="2"></svg>
            <span>{{ uploading() ? text.uploading : text.upload }}</span>
          </button>
        </div>
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
      .info-grid {
        margin: 0 0 24px 0;
        border-top: 1px solid var(--color-border-subtle);
      }
      .info-row {
        display: flex;
        justify-content: space-between;
        padding: 10px 0;
        border-bottom: 1px solid var(--color-border-subtle);
        font-size: var(--font-size-body);
        dt {
          color: var(--color-text-muted);
        }
        dd {
          margin: 0;
          font-weight: 500;
        }
      }
      .upload-box {
        background-color: var(--color-surface-subtle);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
        padding: 16px;
      }
      .upload-label {
        display: block;
        margin-bottom: 10px;
        font-size: var(--font-size-meta);
        font-weight: 500;
      }
      .upload-controls {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 12px;
      }
    `,
  ],
})
export class OuiSettingsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);
  protected readonly text = OUI_TEXT;

  protected readonly status = signal<OUIStatus | null>(null);
  protected readonly selectedFile = signal<File | null>(null);
  protected readonly uploading = signal(false);

  ngOnInit(): void {
    this.http.get<OUIStatus>('/api/v1/settings/oui').subscribe({
      next: (res) => this.status.set(res),
      error: () => {},
    });
  }

  onFileSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    this.selectedFile.set(input.files?.[0] ?? null);
  }

  upload(): void {
    const file = this.selectedFile();
    if (!file) return;

    const form = new FormData();
    form.append('file', file);
    this.uploading.set(true);

    this.http
      .post<OUIStatus>('/api/v1/settings/oui', form, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (res) => {
          this.status.set(res);
          this.uploading.set(false);
          this.selectedFile.set(null);
          this.toast.show(this.text.imported, 'success', this.text.importedDetail(res.entry_count));
        },
        error: (err) => {
          this.uploading.set(false);
          this.toast.show(
            this.text.importFailed,
            'danger',
            err?.error?.message ?? this.text.importFailedHint,
          );
        },
      });
  }
}
