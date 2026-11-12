import { Component, OnInit, inject, signal } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { FormsModule } from '@angular/forms';
import { LucideRotateCcw } from '@lucide/angular';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { StatusPillComponent } from '../../shared/components/status-pill.component';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { NOTIFICATIONS_TEXT } from './notifications.i18n';

type NotificationLog = components['schemas']['NotificationLog'];
type NotificationState = components['schemas']['NotificationState'];
type PaginationMeta = components['schemas']['PaginationMeta'];

@Component({
  selector: 'app-notifications-page',
  standalone: true,
  imports: [FormsModule, EmptyStateComponent, StatusPillComponent, LucideRotateCcw],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ text.title }}</h1>
        <p class="page-subtitle">{{ text.subtitle }}</p>
      </div>
    </header>

    <div class="sub-tabs" role="tablist">
      <button
        type="button"
        role="tab"
        class="tab-pill"
        [class.active]="activeTab() === 'logs'"
        (click)="switchTab('logs')"
      >
        {{ text.tabs.logs }}
      </button>
      <button
        type="button"
        role="tab"
        class="tab-pill"
        [class.active]="activeTab() === 'states'"
        (click)="switchTab('states')"
      >
        {{ text.tabs.states }}
      </button>
    </div>

    @if (activeTab() === 'logs') {
      <div class="toolbar">
        <div class="filter-group">
          <label for="statusFilter">{{ text.logs.statusFilter }}</label>
          <select
            id="statusFilter"
            class="form-input filter-select"
            [(ngModel)]="statusFilter"
            (ngModelChange)="onStatusChange()"
          >
            <option value="">{{ text.logs.statusOptions.all }}</option>
            <option value="sent">{{ text.logs.statusOptions.sent }}</option>
            <option value="failed">{{ text.logs.statusOptions.failed }}</option>
            <option value="pending">{{ text.logs.statusOptions.pending }}</option>
          </select>
        </div>
      </div>

      @if (loadingLogs()) {
        <div class="card">
          <p class="muted-text">{{ text.logs.loading }}</p>
        </div>
      } @else if (logs().length === 0) {
        <app-empty-state
          [title]="text.logs.empty.title"
          [description]="text.logs.empty.description"
        />
      } @else {
        <div class="card table-card">
          <div class="table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ text.logs.columns.status }}</th>
                  <th>{{ text.logs.columns.type }}</th>
                  <th>{{ text.logs.columns.recipient }}</th>
                  <th>{{ text.logs.columns.subject }}</th>
                  <th>{{ text.logs.columns.attempts }}</th>
                  <th>{{ text.logs.columns.lastError }}</th>
                  <th>{{ text.logs.columns.time }}</th>
                </tr>
              </thead>
              <tbody>
                @for (item of logs(); track item.id) {
                  <tr>
                    <td>
                      @if (item.status === 'sent') {
                        <app-status-pill [label]="text.logs.status.sent" variant="online" />
                      } @else if (item.status === 'failed') {
                        <app-status-pill [label]="text.logs.status.failed" variant="conflict" />
                      } @else {
                        <app-status-pill [label]="text.logs.status.pending" variant="offline" />
                      }
                    </td>
                    <td>{{ formatNotifType(item.notif_type) }}</td>
                    <td>{{ item.recipient }}</td>
                    <td class="subject-cell">{{ item.subject }}</td>
                    <td class="font-mono">{{ item.attempt_count }}</td>
                    <td>
                      @if (item.last_error) {
                        <span class="error-text">{{ item.last_error }}</span>
                      } @else {
                        <span class="muted-text">—</span>
                      }
                    </td>
                    <td class="font-mono">{{ formatTime(item.sent_at ?? item.created_at) }}</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>

          @if (logPagination(); as pg) {
            <div class="pagination-bar">
              <span>{{ text.pageSummary(pg.page, pg.total_pages || 1, pg.total_items) }}</span>
              <div class="pagination-btns">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  [disabled]="pg.page <= 1"
                  (click)="loadLogs(pg.page - 1)"
                >
                  {{ pager.previous }}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  [disabled]="pg.page >= pg.total_pages"
                  (click)="loadLogs(pg.page + 1)"
                >
                  {{ pager.next }}
                </button>
              </div>
            </div>
          }
        </div>
      }
    } @else {
      @if (loadingStates()) {
        <div class="card">
          <p class="muted-text">{{ text.states.loading }}</p>
        </div>
      } @else if (states().length === 0) {
        <app-empty-state
          [title]="text.states.empty.title"
          [description]="text.states.empty.description"
        />
      } @else {
        <div class="card table-card">
          <div class="table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ text.states.columns.dedupKey }}</th>
                  <th>{{ text.states.columns.type }}</th>
                  <th>{{ text.states.columns.notifiedAt }}</th>
                  <th>{{ text.states.columns.updatedAt }}</th>
                  <th>{{ text.states.columns.actions }}</th>
                </tr>
              </thead>
              <tbody>
                @for (st of states(); track st.id) {
                  <tr>
                    <td class="font-mono">{{ st.dedup_key }}</td>
                    <td>{{ formatNotifType(st.notif_type) }}</td>
                    <td class="font-mono">{{ formatTime(st.notified_at) }}</td>
                    <td class="font-mono">{{ formatTime(st.updated_at) }}</td>
                    <td>
                      <button
                        type="button"
                        class="btn btn-secondary btn-sm"
                        [disabled]="resettingKey() === st.dedup_key"
                        (click)="resetState(st.dedup_key)"
                      >
                        <svg lucideRotateCcw [size]="14"></svg>
                        {{
                          resettingKey() === st.dedup_key
                            ? text.states.resetting
                            : text.states.reset
                        }}
                      </button>
                    </td>
                  </tr>
                }
              </tbody>
            </table>
          </div>

          @if (statePagination(); as spg) {
            <div class="pagination-bar">
              <span>{{ text.pageSummary(spg.page, spg.total_pages || 1, spg.total_items) }}</span>
              <div class="pagination-btns">
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  [disabled]="spg.page <= 1"
                  (click)="loadStates(spg.page - 1)"
                >
                  {{ pager.previous }}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm"
                  [disabled]="spg.page >= spg.total_pages"
                  (click)="loadStates(spg.page + 1)"
                >
                  {{ pager.next }}
                </button>
              </div>
            </div>
          }
        </div>
      }
    }
  `,
  styles: [
    `
      .page-header {
        margin-bottom: 20px;
      }
      .page-title {
        margin: 0 0 4px 0;
        font-size: var(--font-size-page-title);
        font-weight: 600;
      }
      .page-subtitle {
        margin: 0;
        color: var(--color-text-muted);
      }
      .sub-tabs {
        display: flex;
        gap: 8px;
        margin-bottom: 16px;
      }
      .tab-pill {
        background-color: var(--color-surface);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-full);
        padding: 6px 14px;
        font-size: var(--font-size-meta);
        font-weight: 500;
        color: var(--color-text-secondary);
        cursor: pointer;

        &.active {
          background-color: var(--color-accent);
          color: var(--color-text-on-accent);
          border-color: var(--color-accent);
        }
      }
      .toolbar {
        display: flex;
        justify-content: flex-end;
        margin-bottom: 12px;
      }
      .filter-group {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: var(--font-size-meta);
      }
      .filter-select {
        width: auto;
        min-width: 180px;
      }
      .table-card {
        padding: 0;
        overflow: hidden;
      }
      .table-wrap {
        overflow-x: auto;
      }
      .subject-cell {
        max-width: 300px;
        word-break: break-word;
      }
      .error-text {
        color: var(--color-danger-text);
        font-size: var(--font-size-caption);
      }
      .muted-text {
        color: var(--color-text-muted);
      }
      .pagination-bar {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 12px 16px;
        border-top: 1px solid var(--color-border-default);
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
      .pagination-btns {
        display: flex;
        gap: 8px;
      }
      .btn-sm {
        padding: 4px 10px;
        font-size: var(--font-size-caption);
        display: inline-flex;
        align-items: center;
        gap: 4px;
      }
    `,
  ],
})
export class NotificationsPage implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);
  protected readonly text = NOTIFICATIONS_TEXT;
  protected readonly pager = SHELL_TEXT.pagination;

  protected readonly activeTab = signal<'logs' | 'states'>('logs');
  protected readonly loadingLogs = signal(true);
  protected readonly loadingStates = signal(false);
  protected readonly logs = signal<NotificationLog[]>([]);
  protected readonly states = signal<NotificationState[]>([]);
  protected readonly logPagination = signal<PaginationMeta | null>(null);
  protected readonly statePagination = signal<PaginationMeta | null>(null);
  protected readonly resettingKey = signal<string | null>(null);

  protected statusFilter = '';

  ngOnInit(): void {
    this.loadLogs(1);
  }

  switchTab(tab: 'logs' | 'states'): void {
    this.activeTab.set(tab);
    if (tab === 'logs') {
      this.loadLogs(1);
    } else {
      this.loadStates(1);
    }
  }

  onStatusChange(): void {
    this.loadLogs(1);
  }

  loadLogs(page: number): void {
    this.loadingLogs.set(true);
    let params = new HttpParams().set('page', page).set('page_size', 50);
    if (this.statusFilter) {
      params = params.set('status', this.statusFilter);
    }
    this.http
      .get<{ items: NotificationLog[]; pagination: PaginationMeta }>('/api/v1/notifications/logs', {
        params,
      })
      .subscribe({
        next: (res) => {
          this.logs.set(res.items ?? []);
          this.logPagination.set(res.pagination);
          this.loadingLogs.set(false);
        },
        error: () => {
          this.loadingLogs.set(false);
        },
      });
  }

  loadStates(page: number): void {
    this.loadingStates.set(true);
    const params = new HttpParams().set('page', page).set('page_size', 50);
    this.http
      .get<{
        items: NotificationState[];
        pagination: PaginationMeta;
      }>('/api/v1/notifications/states', { params })
      .subscribe({
        next: (res) => {
          this.states.set(res.items ?? []);
          this.statePagination.set(res.pagination);
          this.loadingStates.set(false);
        },
        error: () => {
          this.loadingStates.set(false);
        },
      });
  }

  resetState(dedupKey: string): void {
    this.resettingKey.set(dedupKey);
    const encoded = encodeURIComponent(dedupKey);
    this.http
      .post(
        `/api/v1/notifications/states/${encoded}/reset`,
        {},
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: () => {
          this.resettingKey.set(null);
          this.toast.show(
            this.text.states.resetDone,
            'success',
            this.text.states.resetDoneDetail(dedupKey),
          );
          this.loadStates(this.statePagination()?.page ?? 1);
        },
        error: (err) => {
          this.resettingKey.set(null);
          this.toast.show(
            this.text.states.resetFailed,
            'danger',
            err?.error?.message ?? this.text.states.resetFailedHint,
          );
        },
      });
  }

  formatNotifType(t: string): string {
    return this.text.types[t] ?? t;
  }

  formatTime(val?: string | null): string {
    if (!val) return '—';
    const d = new Date(val);
    if (isNaN(d.getTime())) return val;
    return d.toLocaleString('zh-TW', { hour12: false });
  }
}
