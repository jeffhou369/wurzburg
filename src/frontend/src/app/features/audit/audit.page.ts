import { Component, OnInit, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { LucideChevronDown, LucideChevronUp } from '@lucide/angular';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import type { components } from '../../core/api.types';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { AUDIT_TEXT } from './audit.i18n';

type AuditLogEntry = components['schemas']['AuditLogEntry'];
type PaginationMeta = components['schemas']['PaginationMeta'];

@Component({
  selector: 'app-audit-page',
  standalone: true,
  imports: [EmptyStateComponent, LucideChevronDown, LucideChevronUp],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ text.title }}</h1>
        <p class="page-subtitle">{{ text.subtitle }}</p>
      </div>
    </header>

    @if (loading()) {
      <div class="card">
        <p class="muted-text">{{ text.loading }}</p>
      </div>
    } @else if (items().length === 0) {
      <app-empty-state [title]="text.empty.title" [description]="text.empty.description" />
    } @else {
      <div class="card table-card">
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ text.columns.time }}</th>
                <th>{{ text.columns.actor }}</th>
                <th>{{ text.columns.sourceIp }}</th>
                <th>{{ text.columns.action }}</th>
                <th>{{ text.columns.target }}</th>
                <th>{{ text.columns.summary }}</th>
                <th>{{ text.columns.details }}</th>
              </tr>
            </thead>
            <tbody>
              @for (item of items(); track item.id) {
                <tr>
                  <td class="font-mono">{{ formatTime(item.created_at) }}</td>
                  <td>
                    <span>{{ item.username }}</span>
                    @if (item.user_role) {
                      <span class="role-tag">{{ item.user_role }}</span>
                    }
                  </td>
                  <td class="font-mono">{{ item.ip_address }}</td>
                  <td class="font-mono">{{ item.action }}</td>
                  <td>{{ item.target_type }} / {{ item.target_id }}</td>
                  <td>{{ item.summary }}</td>
                  <td>
                    @if (item.details_json) {
                      <button
                        type="button"
                        class="btn btn-ghost btn-sm"
                        (click)="toggleExpand(item.id)"
                        [attr.aria-expanded]="expandedIds().has(item.id)"
                      >
                        @if (expandedIds().has(item.id)) {
                          <svg lucideChevronUp [size]="14"></svg> {{ text.collapse }}
                        } @else {
                          <svg lucideChevronDown [size]="14"></svg> {{ text.expand }}
                        }
                      </button>
                    } @else {
                      <span class="muted-text">—</span>
                    }
                  </td>
                </tr>
                @if (item.details_json && expandedIds().has(item.id)) {
                  <tr class="details-row">
                    <td colspan="7">
                      <pre class="details-pre font-mono">{{ formatJson(item.details_json) }}</pre>
                    </td>
                  </tr>
                }
              }
            </tbody>
          </table>
        </div>

        @if (pagination(); as pg) {
          <div class="pagination-bar">
            <span class="muted-text">{{
              pager.summary(pg.total_items, pg.page, pg.total_pages || 1)
            }}</span>
            <div class="pg-actions">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                [disabled]="pg.page <= 1"
                (click)="loadPage(pg.page - 1)"
              >
                {{ pager.previous }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                [disabled]="pg.page >= pg.total_pages"
                (click)="loadPage(pg.page + 1)"
              >
                {{ pager.next }}
              </button>
            </div>
          </div>
        }
      </div>
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
      .table-card {
        padding: 0;
        overflow: hidden;
      }
      .table-wrap {
        overflow-x: auto;
      }
      .data-table {
        width: 100%;
        border-collapse: collapse;
        text-align: left;
        font-size: var(--font-size-meta);

        th {
          background-color: var(--color-surface-subtle);
          color: var(--color-text-secondary);
          font-size: var(--font-size-caption);
          font-weight: 500;
          padding: 10px 14px;
          border-bottom: 1px solid var(--color-border-default);
        }
        td {
          padding: 12px 14px;
          border-bottom: 1px solid var(--color-border-subtle);
          vertical-align: top;
        }
      }
      .role-tag {
        display: inline-block;
        margin-left: 6px;
        padding: 1px 6px;
        border-radius: var(--radius-sm);
        background-color: var(--color-surface-subtle);
        font-size: 11px;
        color: var(--color-text-muted);
      }
      .details-row td {
        background-color: var(--color-surface-subtle);
        padding: 12px 18px;
      }
      .details-pre {
        margin: 0;
        font-size: 12px;
        white-space: pre-wrap;
        word-break: break-all;
      }
      .pagination-bar {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 12px 16px;
        border-top: 1px solid var(--color-border-default);
      }
      .pg-actions {
        display: flex;
        gap: 8px;
      }
      .muted-text {
        color: var(--color-text-muted);
        font-size: var(--font-size-meta);
      }
    `,
  ],
})
export class AuditPage implements OnInit {
  private readonly http = inject(HttpClient);
  protected readonly text = AUDIT_TEXT;
  protected readonly pager = SHELL_TEXT.pagination;
  protected readonly items = signal<AuditLogEntry[]>([]);
  protected readonly pagination = signal<PaginationMeta | null>(null);
  protected readonly loading = signal(true);
  protected readonly expandedIds = signal<Set<number>>(new Set());

  ngOnInit(): void {
    this.loadPage(1);
  }

  loadPage(page: number): void {
    this.loading.set(true);
    this.http
      .get<{ items: AuditLogEntry[]; pagination: PaginationMeta }>(
        `/api/v1/audit/logs?page=${page}&page_size=50`,
      )
      .subscribe({
        next: (res) => {
          this.items.set(res.items ?? []);
          this.pagination.set(res.pagination);
          this.loading.set(false);
        },
        error: () => {
          this.items.set([]);
          this.loading.set(false);
        },
      });
  }

  toggleExpand(id: number): void {
    this.expandedIds.update((set) => {
      const next = new Set(set);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }

  formatTime(iso: string): string {
    return new Date(iso).toLocaleString('zh-TW', { timeZone: 'Asia/Taipei', hour12: false });
  }

  formatJson(obj: unknown): string {
    return JSON.stringify(obj, null, 2);
  }
}
