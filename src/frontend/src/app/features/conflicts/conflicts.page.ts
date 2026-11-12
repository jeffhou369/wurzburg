import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { SheetContainerComponent } from '../../shared/components/sheet-container.component';
import { ToastService } from '../../shared/components/toast.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import { AuthService } from '../../core/auth.service';
import type { components } from '../../core/api.types';
import { CONFLICTS_TEXT } from './conflicts.i18n';

type ConflictSummary = components['schemas']['ConflictSummary'];
type ConflictDetail = components['schemas']['ConflictDetail'];

@Component({
  selector: 'app-conflicts-page',
  standalone: true,
  imports: [FormsModule, EmptyStateComponent, SheetContainerComponent],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ text.title }}</h1>
        <p class="page-subtitle">{{ text.subtitle }}</p>
      </div>
    </header>

    @if (items().length === 0) {
      <app-empty-state [title]="text.empty.title" [description]="text.empty.description" />
    } @else {
      <div class="card table-card">
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ text.columns.status }}</th>
                <th>{{ text.columns.ipAddress }}</th>
                <th>{{ text.columns.adoptedMac }}</th>
                <th>{{ text.columns.observedMacs }}</th>
                <th>{{ text.columns.owner }}</th>
                <th>{{ text.columns.lastDetected }}</th>
              </tr>
            </thead>
            <tbody>
              @for (c of items(); track c.id) {
                <tr class="clickable-row" (click)="openDetail(c.id)">
                  <td>
                    @switch (c.status) {
                      @case ('open') {
                        <span class="status-pill status-conflict"
                          ><span class="status-dot"></span><span>{{ text.status.open }}</span></span
                        >
                      }
                      @case ('in_progress') {
                        <span class="status-pill status-unregistered"
                          ><span class="status-dot"></span
                          ><span>{{ text.status.inProgress }}</span></span
                        >
                      }
                      @default {
                        <span class="status-pill status-online"
                          ><span class="status-dot"></span
                          ><span>{{ text.status.resolved }}</span></span
                        >
                      }
                    }
                  </td>
                  <td class="font-mono">{{ c.ip_address }}</td>
                  <td class="font-mono">{{ c.adopted_mac }}</td>
                  <td class="font-mono">{{ c.conflict_macs.join(', ') }}</td>
                  <td>{{ c.owner_username || text.unregistered }}</td>
                  <td class="font-mono">{{ formatTime(c.last_detected_at) }}</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </div>
    }

    <app-sheet-container
      [open]="selected() !== null"
      [title]="selected() ? text.sheet.title(selected()!.ip_address) : text.sheet.fallbackTitle"
      (closed)="selected.set(null)"
    >
      @if (selected(); as d) {
        <div class="detail-stack">
          <div class="info-box">
            <div>
              <strong>{{ text.sheet.adoptedMac }}</strong>
              <span class="font-mono">{{ d.adopted_mac }}</span>
            </div>
            <div>
              <strong>{{ text.sheet.owner }}</strong>
              {{ d.owner_username || text.sheet.ownerCoexist }}
            </div>
          </div>

          <div>
            <h3 class="sub-title">{{ text.sheet.sources }}</h3>
            @for (src of d.sources; track src.mac_address + src.switch_name) {
              <div class="source-row">
                <div class="font-mono">{{ src.mac_address }}</div>
                <div class="muted-text">
                  {{ src.switch_name }} · {{ src.vrf_name }} · {{ src.if_name }}
                </div>
              </div>
            }
          </div>

          @if (auth.isAdmin() && d.status !== 'resolved') {
            <form class="resolve-form" (ngSubmit)="submitResolution(d.id)">
              <h3 class="sub-title">{{ text.resolve.title }}</h3>
              <div class="radio-stack">
                @if (d.has_owner) {
                  <label class="radio-item">
                    <input
                      type="radio"
                      name="resolution"
                      value="adopt_new_keep_reg"
                      [(ngModel)]="resolveForm.resolution"
                    />
                    <span>{{ text.resolve.adoptNewKeepReg }}</span>
                  </label>
                  <label class="radio-item">
                    <input
                      type="radio"
                      name="resolution"
                      value="adopt_new_release_reg"
                      [(ngModel)]="resolveForm.resolution"
                    />
                    <span>{{ text.resolve.adoptNewReleaseReg }}</span>
                  </label>
                } @else {
                  <label class="radio-item">
                    <input
                      type="radio"
                      name="resolution"
                      value="adopt_new_keep_reg"
                      [(ngModel)]="resolveForm.resolution"
                    />
                    <span>{{ text.resolve.adoptNew }}</span>
                  </label>
                }
                <label class="radio-item">
                  <input
                    type="radio"
                    name="resolution"
                    value="keep_adopted"
                    [(ngModel)]="resolveForm.resolution"
                  />
                  <span>{{ text.resolve.keepAdopted }}</span>
                </label>
              </div>

              @if (resolveForm.resolution !== 'keep_adopted') {
                <div class="field-group">
                  <label for="adoptedMacSelect">{{ text.resolve.pickMac }}</label>
                  <select
                    id="adoptedMacSelect"
                    name="adoptedMac"
                    class="form-select font-mono"
                    [(ngModel)]="resolveForm.adopted_mac"
                  >
                    @for (m of d.conflict_macs; track m) {
                      <option [value]="m">{{ m }}</option>
                    }
                  </select>
                </div>
              }

              <div class="field-group">
                <label for="resolveNote">{{ text.resolve.note }}</label>
                <textarea
                  id="resolveNote"
                  name="resolveNote"
                  class="form-textarea"
                  rows="2"
                  [(ngModel)]="resolveForm.note"
                ></textarea>
              </div>

              <button type="submit" class="btn btn-primary">{{ text.resolve.submit }}</button>
            </form>
          }
        </div>
      }
    </app-sheet-container>
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
        font-size: var(--font-size-meta);
        th {
          background-color: var(--color-surface-subtle);
          padding: 10px 14px;
          text-align: left;
        }
        td {
          padding: 12px 14px;
          border-bottom: 1px solid var(--color-border-subtle);
        }
      }
      .clickable-row {
        cursor: pointer;
        &:hover {
          background-color: var(--color-surface-hover);
        }
      }
      .detail-stack {
        display: flex;
        flex-direction: column;
        gap: 20px;
      }
      .info-box {
        background-color: var(--color-surface-subtle);
        padding: 12px;
        border-radius: var(--radius-md);
        display: flex;
        flex-direction: column;
        gap: 6px;
        font-size: var(--font-size-meta);
      }
      .sub-title {
        margin: 0 0 10px 0;
        font-size: var(--font-size-meta);
        font-weight: 600;
      }
      .source-row {
        padding: 8px 0;
        border-bottom: 1px solid var(--color-border-subtle);
      }
      .resolve-form {
        display: flex;
        flex-direction: column;
        gap: 14px;
        border-top: 1px solid var(--color-border-default);
        padding-top: 16px;
      }
      .radio-stack {
        display: flex;
        flex-direction: column;
        gap: 8px;
      }
      .radio-item {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: var(--font-size-meta);
        cursor: pointer;
      }
      .field-group {
        display: flex;
        flex-direction: column;
        gap: 6px;
      }
      .muted-text {
        color: var(--color-text-muted);
        font-size: var(--font-size-caption);
      }
    `,
  ],
})
export class ConflictsPage implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);
  protected readonly badges = inject(SidebarBadgesService);
  protected readonly auth = inject(AuthService);
  protected readonly text = CONFLICTS_TEXT;

  protected readonly items = signal<ConflictSummary[]>([]);
  protected readonly selected = signal<ConflictDetail | null>(null);

  protected resolveForm = {
    resolution: 'adopt_new_keep_reg',
    adopted_mac: '',
    note: '',
  };

  ngOnInit(): void {
    this.loadConflicts();
  }

  loadConflicts(): void {
    this.http.get<{ open_count: number; items: ConflictSummary[] }>('/api/v1/conflicts').subscribe({
      next: (res) => {
        this.items.set(res.items ?? []);
        this.badges.openConflictsCount.set(res.open_count);
      },
      error: () => {},
    });
  }

  openDetail(id: number): void {
    this.http.get<ConflictDetail>(`/api/v1/conflicts/${id}`).subscribe({
      next: (res) => {
        this.selected.set(res);
        const other =
          res.conflict_macs.find((m) => m !== res.adopted_mac) ?? res.conflict_macs[0] ?? '';
        this.resolveForm = {
          resolution: 'adopt_new_keep_reg',
          adopted_mac: other,
          note: '',
        };
      },
      error: () => {},
    });
  }

  submitResolution(id: number): void {
    this.http
      .post<ConflictDetail>(`/api/v1/conflicts/${id}/resolve`, this.resolveForm, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: () => {
          this.selected.set(null);
          this.loadConflicts();
          this.toast.show(this.text.resolve.applied, 'success');
        },
        error: (err) => this.toast.show(this.text.resolve.failed, 'danger', err?.error?.message),
      });
  }

  formatTime(iso: string): string {
    return new Date(iso).toLocaleString('zh-TW', { timeZone: 'Asia/Taipei', hour12: false });
  }
}
