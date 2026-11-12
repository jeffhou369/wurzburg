import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { SheetContainerComponent } from '../../shared/components/sheet-container.component';
import { ToastService } from '../../shared/components/toast.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import { AuthService } from '../../core/auth.service';
import type { components } from '../../core/api.types';

type ConflictSummary = components['schemas']['ConflictSummary'];
type ConflictDetail = components['schemas']['ConflictDetail'];

@Component({
  selector: 'app-conflicts-page',
  standalone: true,
  imports: [FormsModule, EmptyStateComponent, SheetContainerComponent],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">MAC 衝突告警</h1>
        <p class="page-subtitle">檢視並裁決同一位址出現多個 MAC 或已登記位址變更 MAC 之情形</p>
      </div>
    </header>

    @if (items().length === 0) {
      <app-empty-state
        title="目前無衝突紀錄"
        description="所有納管網段的 IP 與 MAC 對應關係皆一致。"
      />
    } @else {
      <div class="card table-card">
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>狀態</th>
                <th>IP 位址</th>
                <th>採信 MAC</th>
                <th>觀測到的 MAC</th>
                <th>擁有者</th>
                <th>最近發現</th>
              </tr>
            </thead>
            <tbody>
              @for (c of items(); track c.id) {
                <tr class="clickable-row" (click)="openDetail(c.id)">
                  <td>
                    @switch (c.status) {
                      @case ('open') {
                        <span class="status-pill status-conflict"
                          ><span class="status-dot"></span><span>未處理</span></span
                        >
                      }
                      @case ('in_progress') {
                        <span class="status-pill status-unregistered"
                          ><span class="status-dot"></span><span>處理中</span></span
                        >
                      }
                      @default {
                        <span class="status-pill status-online"
                          ><span class="status-dot"></span><span>已解消</span></span
                        >
                      }
                    }
                  </td>
                  <td class="font-mono">{{ c.ip_address }}</td>
                  <td class="font-mono">{{ c.adopted_mac }}</td>
                  <td class="font-mono">{{ c.conflict_macs.join(', ') }}</td>
                  <td>{{ c.owner_username || '未登記' }}</td>
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
      [title]="selected() ? '衝突裁決：' + selected()!.ip_address : '衝突詳細資料'"
      (closed)="selected.set(null)"
    >
      @if (selected(); as d) {
        <div class="detail-stack">
          <div class="info-box">
            <div>
              <strong>採信 MAC：</strong> <span class="font-mono">{{ d.adopted_mac }}</span>
            </div>
            <div><strong>擁有者：</strong> {{ d.owner_username || '未登記（並存衝突）' }}</div>
          </div>

          <div>
            <h3 class="sub-title">觀測來源明細</h3>
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
              <h3 class="sub-title">裁決處置</h3>
              <div class="radio-stack">
                @if (d.has_owner) {
                  <label class="radio-item">
                    <input
                      type="radio"
                      name="resolution"
                      value="adopt_new_keep_reg"
                      [(ngModel)]="resolveForm.resolution"
                    />
                    <span>採信新的 MAC，保留既有登記（擁有者更換設備）</span>
                  </label>
                  <label class="radio-item">
                    <input
                      type="radio"
                      name="resolution"
                      value="adopt_new_release_reg"
                      [(ngModel)]="resolveForm.resolution"
                    />
                    <span>採信新的 MAC，並釋放登記（位址已轉由他人使用）</span>
                  </label>
                } @else {
                  <label class="radio-item">
                    <input
                      type="radio"
                      name="resolution"
                      value="adopt_new_keep_reg"
                      [(ngModel)]="resolveForm.resolution"
                    />
                    <span>採信新的 MAC</span>
                  </label>
                }
                <label class="radio-item">
                  <input
                    type="radio"
                    name="resolution"
                    value="keep_adopted"
                    [(ngModel)]="resolveForm.resolution"
                  />
                  <span>維持原本的 MAC（進入處理中，等待錯誤裝置撤除）</span>
                </label>
              </div>

              @if (resolveForm.resolution !== 'keep_adopted') {
                <div class="field-group">
                  <label for="adoptedMacSelect">選擇採信之 MAC</label>
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
                <label for="resolveNote">處置說明（選填）</label>
                <textarea
                  id="resolveNote"
                  name="resolveNote"
                  class="form-textarea"
                  rows="2"
                  [(ngModel)]="resolveForm.note"
                ></textarea>
              </div>

              <button type="submit" class="btn btn-primary">套用裁決</button>
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
          this.toast.show('衝突裁決已套用', 'success');
        },
        error: (err) => this.toast.show('裁決失敗', 'danger', err?.error?.message),
      });
  }

  formatTime(iso: string): string {
    return new Date(iso).toLocaleString('zh-TW', { timeZone: 'Asia/Taipei', hour12: false });
  }
}
