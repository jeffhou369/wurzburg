import { Component, OnInit, inject, signal, computed } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient, HttpParams } from '@angular/common/http';
import { ActivatedRoute, Router } from '@angular/router';
import { Dialog } from '@angular/cdk/dialog';
import {
  LucideSearch,
  LucideDownload,
  LucideCopy,
  LucideRefreshCw,
  LucideCheckSquare,
} from '@lucide/angular';
import { StatusPillComponent } from '../../shared/components/status-pill.component';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { IpRecordSheetComponent } from '../../shared/components/ip-record-sheet.component';
import { ConfirmDialogComponent } from '../../shared/components/confirm-dialog.component';
import { SheetContainerComponent } from '../../shared/components/sheet-container.component';
import { ToastService } from '../../shared/components/toast.service';
import { AuthService } from '../../core/auth.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import type { components } from '../../core/api.types';

type DashboardSummary = components['schemas']['DashboardSummary'];
type IPRecordSummary = components['schemas']['IPRecordSummary'] & { state_label?: string };
type IPRecordDetail = components['schemas']['IPRecordDetail'] & { state_label?: string };
type PaginationMeta = components['schemas']['PaginationMeta'];
type Subnet = components['schemas']['Subnet'];
type BatchActionResult = components['schemas']['BatchActionResult'];

@Component({
  selector: 'app-workbench-page',
  standalone: true,
  imports: [
    FormsModule,
    StatusPillComponent,
    EmptyStateComponent,
    IpRecordSheetComponent,
    SheetContainerComponent,
    LucideSearch,
    LucideDownload,
    LucideCopy,
    LucideRefreshCw,
    LucideCheckSquare,
  ],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">IP 總覽</h1>
        <p class="page-subtitle">納管網段內所有 IPv4 位址之即時狀態、ARP 觀測與登記管理</p>
      </div>
      @if (stats(); as st) {
        <div class="sync-header-badge">
          @if (st.sync_in_progress) {
            <app-status-pill variant="online" label="同步中" />
          } @else if (st.last_sync_success) {
            <app-status-pill variant="online" label="最近同步成功" />
          } @else {
            <app-status-pill variant="conflict" label="同步有失敗項目" />
          }
          @if (st.last_sync_at) {
            <span class="font-mono muted-text">{{ formatTime(st.last_sync_at) }}</span>
          }
        </div>
      }
    </header>

    <section aria-label="尚未完成的設定">
      <ng-content select="[slot=checklist]" />
    </section>

    <!-- 儀表板 KPI 列 (Ticket 16) -->
    @if (stats(); as st) {
      <section class="kpi-grid" aria-label="統計儀表板">
        <div class="card kpi-card">
          <div class="kpi-label">納管 IP 總數</div>
          <div class="kpi-value font-mono">{{ st.managed_total }}</div>
          <div class="kpi-sub">監控 {{ st.switches_count }} 台交換器</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">在線</div>
          <div class="kpi-value font-mono text-success">{{ st.online_count }}</div>
          <div class="kpi-sub">交換器 ARP 表中尚有紀錄</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">離線</div>
          <div class="kpi-value font-mono text-muted">{{ st.offline_count }}</div>
          <div class="kpi-sub">保留歷史紀錄未刪除</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">未登記（在線）</div>
          <div class="kpi-value font-mono text-warning">{{ st.unregistered_online_count }}</div>
          <div class="kpi-sub">待負責人完成登記</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">已到期</div>
          <div class="kpi-value font-mono text-danger">{{ st.expired_count }}</div>
          <div class="kpi-sub">超過一年待重新確認</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">MAC 衝突</div>
          <div class="kpi-value font-mono text-danger">{{ st.open_conflicts_count }}</div>
          <div class="kpi-sub">需管理員裁決項目</div>
        </div>
      </section>
    }

    <!-- 工具列與篩選條件 (Ticket 15 & 26) -->
    <section class="card toolbar-card">
      <div class="toolbar-top">
        <div class="search-box">
          <svg lucideSearch [size]="16" class="search-icon"></svg>
          <input
            type="search"
            class="form-input search-input"
            placeholder="搜尋 IP、MAC、擁有者、主機名稱或備註..."
            [(ngModel)]="filters.search"
            (keyup.enter)="applyFilters(1)"
          />
        </div>
        <div class="toolbar-buttons">
          <button type="button" class="btn btn-secondary" (click)="applyFilters(1)">
            <svg lucideRefreshCw [size]="16"></svg>
            <span>套用查詢</span>
          </button>
          <button type="button" class="btn btn-primary" (click)="exportExcel()">
            <svg lucideDownload [size]="16"></svg>
            <span>匯出 Excel ({{ pagination()?.total_items ?? 0 }})</span>
          </button>
        </div>
      </div>

      <div class="filter-row">
        <select
          class="form-select filter-select"
          [(ngModel)]="filters.subnet_id"
          (change)="applyFilters(1)"
        >
          <option value="">全部網段</option>
          @for (sub of subnets(); track sub.id) {
            <option [value]="sub.id">{{ sub.cidr }}</option>
          }
        </select>
        <select
          class="form-select filter-select"
          [(ngModel)]="filters.presence"
          (change)="applyFilters(1)"
        >
          <option value="">全部在線狀態</option>
          <option value="online">在線</option>
          <option value="offline">離線</option>
        </select>
        <select
          class="form-select filter-select"
          [(ngModel)]="filters.registration"
          (change)="applyFilters(1)"
        >
          <option value="">全部登記狀態</option>
          <option value="unregistered">未登記</option>
          <option value="registered">已登記</option>
          <option value="expired">已到期</option>
        </select>
        <label class="filter-chip" [class.active]="filters.expiring_soon">
          <input type="checkbox" [(ngModel)]="filters.expiring_soon" (change)="applyFilters(1)" />
          <span>即將到期</span>
        </label>
        <label class="filter-chip" [class.active]="filters.has_conflict">
          <input type="checkbox" [(ngModel)]="filters.has_conflict" (change)="applyFilters(1)" />
          <span>有衝突</span>
        </label>
        <label class="filter-chip" [class.active]="filters.owner_me">
          <input type="checkbox" [(ngModel)]="filters.owner_me" (change)="applyFilters(1)" />
          <span>我的登記</span>
        </label>
      </div>
    </section>

    <!-- 批次操作列 (Ticket 27) -->
    @if (selectedCount() > 0) {
      <div class="batch-bar">
        <div class="batch-info">
          <svg lucideCheckSquare [size]="16"></svg>
          @if (selectAllFiltered()) {
            <span>已全選目前篩選結果共 {{ pagination()?.total_items ?? 0 }} 筆</span>
            <button type="button" class="btn btn-ghost btn-sm" (click)="clearSelection()">
              取消全選
            </button>
          } @else {
            <span>已選取本頁 {{ selectedIds().size }} 筆</span>
            @if ((pagination()?.total_items ?? 0) > items().length) {
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                (click)="selectAllFiltered.set(true)"
              >
                全選符合篩選的全部 {{ pagination()?.total_items }} 筆
              </button>
            }
          }
        </div>
        <div class="batch-actions">
          <button
            type="button"
            class="btn btn-primary btn-sm"
            (click)="batchRegisterOpen.set(true)"
          >
            批次登記
          </button>
          <button type="button" class="btn btn-secondary btn-sm" (click)="executeBatch('confirm')">
            批次重新確認
          </button>
          <button
            type="button"
            class="btn btn-danger-soft btn-sm"
            (click)="confirmBatchAction('release')"
          >
            批次釋放
          </button>
          @if (auth.isAdmin()) {
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              (click)="batchReassignOpen.set(true)"
            >
              批次改派
            </button>
          }
        </div>
      </div>
    }

    <!-- IP 主列表 (Ticket 15) -->
    @if (items().length === 0 && !loading()) {
      <app-empty-state
        title="查無符合條件的 IP 紀錄"
        description="嘗試調整搜尋條件或至網段管理確認已納管的網段。"
      />
    } @else {
      <div class="card table-card">
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th class="col-check">
                  <input
                    type="checkbox"
                    [checked]="allCurrentPageSelected()"
                    (change)="toggleSelectPage($event)"
                    aria-label="選取本頁全部"
                  />
                </th>
                <th>狀態</th>
                <th class="sortable" (click)="toggleSort('ip_address')">IP 位址</th>
                <th>MAC 位址</th>
                <th>廠商</th>
                <th>裝置類型</th>
                <th>網段 / 交換器</th>
                <th class="sortable" (click)="toggleSort('owner_username')">擁有者</th>
                <th class="sortable" (click)="toggleSort('expires_at')">到期日</th>
                <th class="sortable" (click)="toggleSort('last_seen_at')">最後見到</th>
                <th>動作</th>
              </tr>
            </thead>
            <tbody>
              @for (item of items(); track item.id) {
                <tr
                  [class.row-offline]="item.presence === 'offline'"
                  class="clickable-row"
                  (click)="openDetail(item.id)"
                >
                  <td (click)="$event.stopPropagation()">
                    <input
                      type="checkbox"
                      [checked]="selectAllFiltered() || selectedIds().has(item.id)"
                      (change)="toggleRow(item.id)"
                      [attr.aria-label]="'選取 ' + item.ip_address"
                    />
                  </td>
                  <td>
                    <div class="status-combo">
                      @if (item.has_conflict) {
                        <app-status-pill variant="conflict" label="衝突" />
                      }
                      <app-status-pill
                        [variant]="item.presence === 'online' ? 'online' : 'offline'"
                        [label]="item.presence === 'online' ? '在線' : '離線'"
                      />
                      @if (item.registration === 'unregistered') {
                        <app-status-pill variant="unregistered" label="未登記" />
                      } @else if (item.registration === 'expired') {
                        <app-status-pill variant="expired" label="已到期" />
                      } @else if (item.expiring_soon) {
                        <app-status-pill variant="expiring" label="即將到期" />
                      } @else {
                        <app-status-pill variant="registered" label="已登記" />
                      }
                    </div>
                  </td>
                  <td class="font-mono ip-cell">{{ item.ip_address }}</td>
                  <td class="font-mono">{{ item.mac_address }}</td>
                  <td>{{ item.vendor_name }}</td>
                  <td>{{ item.device_type || '—' }}</td>
                  <td>
                    <div class="font-mono">{{ item.subnet_cidr }}</div>
                    <div class="muted-text">{{ item.switch_names?.join(', ') || '—' }}</div>
                  </td>
                  <td>
                    @if (item.owner_username) {
                      <div>{{ item.owner_display_name || item.owner_username }}</div>
                      <div class="muted-text">{{ item.owner_mail }}</div>
                    } @else {
                      <span class="muted-text">—</span>
                    }
                  </td>
                  <td class="font-mono">
                    {{ item.expires_at ? formatDate(item.expires_at) : '—' }}
                  </td>
                  <td class="font-mono">{{ formatTime(item.last_seen_at) }}</td>
                  <td (click)="$event.stopPropagation()">
                    <button
                      type="button"
                      class="btn btn-ghost btn-sm"
                      (click)="copyIP(item.ip_address)"
                      aria-label="複製 IP"
                    >
                      <svg lucideCopy [size]="14"></svg>
                    </button>
                  </td>
                </tr>
              }
            </tbody>
          </table>
        </div>

        @if (pagination(); as pg) {
          <div class="pagination-bar">
            <span class="muted-text"
              >共 {{ pg.total_items }} 筆（第 {{ pg.page }} / {{ pg.total_pages || 1 }} 頁）</span
            >
            <div class="pg-actions">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                [disabled]="pg.page <= 1"
                (click)="applyFilters(pg.page - 1)"
              >
                上一頁
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                [disabled]="pg.page >= pg.total_pages"
                (click)="applyFilters(pg.page + 1)"
              >
                下一頁
              </button>
            </div>
          </div>
        }
      </div>
    }

    <!-- 右側 IP 紀錄詳情與登記 Sheet (Ticket 13 & 18) -->
    <app-ip-record-sheet
      [open]="selectedDetail() !== null"
      [ipAddress]="selectedDetail()?.ip_address ?? null"
      (closed)="selectedDetail.set(null)"
    >
      <div slot="registration" class="reg-slot">
        @if (selectedDetail(); as d) {
          <div class="readonly-meta">
            <div>
              <strong>MAC：</strong><span class="font-mono">{{ d.mac_address }}</span>
            </div>
            <div><strong>廠商：</strong>{{ d.vendor_name }}</div>
            <div>
              <strong>網段：</strong><span class="font-mono">{{ d.subnet_cidr }}</span>
            </div>
            <div><strong>狀態：</strong>{{ d.state_label }}</div>
          </div>

          @if (d.registration === 'unregistered' || d.can_edit) {
            <form class="form-stack" (ngSubmit)="saveRegistration(d)">
              <div class="field-group">
                <label for="regMail">擁有者信箱（必填，到期催認寄送至此）</label>
                <input
                  id="regMail"
                  name="regMail"
                  type="email"
                  class="form-input"
                  [(ngModel)]="regForm.owner_mail"
                  required
                />
              </div>
              <div class="field-group">
                <label for="regType">裝置類型（自填）</label>
                <input
                  id="regType"
                  name="regType"
                  type="text"
                  class="form-input"
                  [(ngModel)]="regForm.device_type"
                />
              </div>
              <div class="field-group">
                <label for="regHost">主機名稱</label>
                <input
                  id="regHost"
                  name="regHost"
                  type="text"
                  class="form-input font-mono"
                  [(ngModel)]="regForm.hostname"
                />
              </div>
              <div class="field-group">
                <label for="regNotes"
                  >備註 <span class="public-note">（注意：所有登入者都看得到）</span></label
                >
                <textarea
                  id="regNotes"
                  name="regNotes"
                  class="form-textarea"
                  rows="3"
                  [(ngModel)]="regForm.notes"
                ></textarea>
              </div>
              @if (d.registration === 'unregistered' && auth.isAdmin()) {
                <div class="field-group">
                  <label for="regOwnerUser">指定擁有者帳號（選填，留空則登記給自己）</label>
                  <input
                    id="regOwnerUser"
                    name="regOwnerUser"
                    type="text"
                    class="form-input"
                    [(ngModel)]="regForm.owner_username"
                  />
                </div>
              }

              <div class="sheet-actions">
                @if (d.registration !== 'unregistered') {
                  <button
                    type="button"
                    class="btn btn-secondary"
                    (click)="singleAction(d.id, 'confirm')"
                  >
                    重新確認（延長一年）
                  </button>
                  <button
                    type="button"
                    class="btn btn-danger-soft"
                    (click)="singleAction(d.id, 'release')"
                  >
                    釋放
                  </button>
                }
                <button type="submit" class="btn btn-primary">
                  {{ d.registration === 'unregistered' ? '完成登記' : '儲存變更' }}
                </button>
              </div>
            </form>

            @if (d.registration !== 'unregistered' && auth.isAdmin()) {
              <div class="reassign-box">
                <label for="reassignTarget">改派擁有者帳號（僅系統管理員）</label>
                <div class="reassign-row">
                  <input
                    id="reassignTarget"
                    type="text"
                    class="form-input"
                    placeholder="輸入目標帳號名稱"
                    [(ngModel)]="reassignTargetUser"
                  />
                  <button type="button" class="btn btn-secondary" (click)="reassignSingle(d.id)">
                    改派
                  </button>
                </div>
              </div>
            }
          } @else {
            <div class="readonly-notice">
              此 IP 紀錄已由 <strong>{{ d.owner_display_name || d.owner_username }}</strong>（{{
                d.owner_mail
              }}）登記。僅擁有者本人可編輯；若須調整或釋放，請聯絡系統管理員。
            </div>
            <dl class="readonly-fields">
              <div>
                <dt>裝置類型</dt>
                <dd>{{ d.device_type || '—' }}</dd>
              </div>
              <div>
                <dt>主機名稱</dt>
                <dd class="font-mono">{{ d.hostname || '—' }}</dd>
              </div>
              <div>
                <dt>備註</dt>
                <dd>{{ d.notes || '—' }}</dd>
              </div>
              <div>
                <dt>到期日</dt>
                <dd class="font-mono">{{ d.expires_at ? formatDate(d.expires_at) : '—' }}</dd>
              </div>
            </dl>
          }
        }
      </div>

      <div slot="observations" class="obs-slot">
        @if (selectedDetail(); as d) {
          <div class="online-explainer">
            <strong>關於「在線」狀態：</strong>
            「在線」代表交換器的 ARP table
            中目前仍有此位址。實體裝置關機或拔線後，須等待交換器自身的 ARP 逾時到期（如 IOS-XE
            預設約 4 小時、NX-OS 約 25 分鐘）並經過寬限輪數後才會轉為離線。
          </div>

          <h3 class="sub-title">目前觀測來源 ({{ d.sightings.length }})</h3>
          @if (d.sightings.length === 0) {
            <p class="muted-text">目前無任何交換器觀測到此位址。</p>
          } @else {
            <ul class="obs-list">
              @for (s of d.sightings; track s.switch_name + s.vrf_name) {
                <li>
                  <div>
                    <strong>{{ s.switch_name }}</strong> ({{ s.vrf_name }} /
                    <span class="font-mono">{{ s.if_name }}</span>)
                  </div>
                  <div class="font-mono muted-text">
                    MAC: {{ s.mac_address }} · 末見: {{ formatTime(s.last_seen_at) }}
                  </div>
                </li>
              }
            </ul>
          }

          <h3 class="sub-title">變更歷程</h3>
          <ul class="event-list">
            @for (e of d.events; track e.id) {
              <li>
                <span class="font-mono muted-text">{{ formatTime(e.occurred_at) }}</span>
                <span class="event-badge">{{ e.event_type }}</span>
                <span>{{ e.details }}</span>
              </li>
            }
          </ul>
        }
      </div>
    </app-ip-record-sheet>

    <!-- 批次登記 Sheet -->
    <app-sheet-container
      [open]="batchRegisterOpen()"
      title="批次登記未登記之 IP 紀錄"
      (closed)="batchRegisterOpen.set(false)"
    >
      <form class="form-stack" (ngSubmit)="submitBatchRegister()">
        <p class="muted-text">
          填寫之欄位將套用至所選範圍內所有尚未登記的位址（已有擁有者之項目將自動略過並回報）。
        </p>
        <div class="field-group">
          <label for="bMail">擁有者信箱（必填）</label>
          <input
            id="bMail"
            name="bMail"
            type="email"
            class="form-input"
            [(ngModel)]="batchRegForm.owner_mail"
            required
          />
        </div>
        <div class="field-group">
          <label for="bType">裝置類型（選填）</label>
          <input
            id="bType"
            name="bType"
            type="text"
            class="form-input"
            [(ngModel)]="batchRegForm.device_type"
          />
        </div>
        <div class="field-group">
          <label for="bNotes">備註（所有登入者都看得到）</label>
          <textarea
            id="bNotes"
            name="bNotes"
            class="form-textarea"
            rows="3"
            [(ngModel)]="batchRegForm.notes"
          ></textarea>
        </div>
        <div class="sheet-actions">
          <button type="button" class="btn btn-secondary" (click)="batchRegisterOpen.set(false)">
            取消
          </button>
          <button type="submit" class="btn btn-primary">執行批次登記</button>
        </div>
      </form>
    </app-sheet-container>

    <!-- 批次改派 Sheet -->
    <app-sheet-container
      [open]="batchReassignOpen()"
      title="批次改派擁有者"
      (closed)="batchReassignOpen.set(false)"
    >
      <form class="form-stack" (ngSubmit)="submitBatchReassign()">
        <div class="field-group">
          <label for="bTarget">目標擁有者帳號</label>
          <input
            id="bTarget"
            name="bTarget"
            type="text"
            class="form-input"
            [(ngModel)]="batchReassignTarget"
            required
          />
        </div>
        <div class="sheet-actions">
          <button type="button" class="btn btn-secondary" (click)="batchReassignOpen.set(false)">
            取消
          </button>
          <button type="submit" class="btn btn-primary">確認批次改派</button>
        </div>
      </form>
    </app-sheet-container>
  `,
  styles: [
    `
      .page-header {
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        flex-wrap: wrap;
        gap: 12px;
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
      .sync-header-badge {
        display: flex;
        align-items: center;
        gap: 10px;
      }
      .kpi-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
        gap: 14px;
        margin-bottom: 20px;
      }
      .kpi-card {
        padding: 18px 20px;
      }
      .kpi-label {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
        font-weight: 500;
      }
      .kpi-value {
        font-size: var(--font-size-display);
        font-weight: 600;
        margin: 4px 0;
      }
      .kpi-sub {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .text-success {
        color: var(--color-success-text);
      }
      .text-warning {
        color: var(--color-warning-text);
      }
      .text-danger {
        color: var(--color-danger-text);
      }
      .text-muted {
        color: var(--color-text-disabled);
      }
      .toolbar-card {
        padding: 16px 20px;
        margin-bottom: 16px;
        display: flex;
        flex-direction: column;
        gap: 12px;
      }
      .toolbar-top {
        display: flex;
        flex-wrap: wrap;
        gap: 12px;
        justify-content: space-between;
      }
      .search-box {
        position: relative;
        flex: 1;
        min-width: 240px;
      }
      .search-icon {
        position: absolute;
        left: 12px;
        top: 50%;
        transform: translateY(-50%);
        color: var(--color-text-muted);
      }
      .search-input {
        padding-left: 36px;
      }
      .toolbar-buttons {
        display: flex;
        gap: 10px;
      }
      .filter-row {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        align-items: center;
      }
      .filter-select {
        width: auto;
        min-width: 150px;
      }
      .filter-chip {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        padding: 6px 12px;
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-full);
        font-size: var(--font-size-meta);
        cursor: pointer;
        &.active {
          background-color: var(--color-accent-soft);
          border-color: var(--color-accent);
          font-weight: 600;
        }
      }
      .batch-bar {
        display: flex;
        flex-wrap: wrap;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        padding: 12px 18px;
        background-color: var(--color-surface-subtle);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
        margin-bottom: 16px;
      }
      .batch-info {
        display: flex;
        align-items: center;
        gap: 10px;
        font-size: var(--font-size-meta);
        font-weight: 600;
      }
      .batch-actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
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
          color: var(--color-text-secondary);
          font-size: var(--font-size-caption);
          padding: 10px 12px;
          text-align: left;
          border-bottom: 1px solid var(--color-border-default);
          &.sortable {
            cursor: pointer;
          }
        }
        td {
          padding: 12px;
          border-bottom: 1px solid var(--color-border-subtle);
          vertical-align: middle;
        }
      }
      .clickable-row {
        cursor: pointer;
        &:hover {
          background-color: var(--color-surface-hover);
        }
      }
      .status-combo {
        display: flex;
        flex-wrap: wrap;
        gap: 4px;
      }
      .ip-cell {
        font-weight: 600;
      }
      .pagination-bar {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 12px 16px;
        border-top: 1px solid var(--color-border-default);
      }
      .pg-actions {
        display: flex;
        gap: 8px;
      }
      .reg-slot,
      .obs-slot {
        display: flex;
        flex-direction: column;
        gap: 18px;
      }
      .readonly-meta,
      .online-explainer {
        background-color: var(--color-surface-subtle);
        padding: 12px;
        border-radius: var(--radius-md);
        font-size: var(--font-size-meta);
        display: flex;
        flex-direction: column;
        gap: 4px;
      }
      .form-stack {
        display: flex;
        flex-direction: column;
        gap: 14px;
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
      .public-note {
        font-weight: 400;
        color: var(--color-text-muted);
      }
      .sheet-actions {
        display: flex;
        flex-wrap: wrap;
        justify-content: flex-end;
        gap: 8px;
      }
      .reassign-box {
        border-top: 1px solid var(--color-border-default);
        padding-top: 14px;
        display: flex;
        flex-direction: column;
        gap: 8px;
        font-size: var(--font-size-meta);
      }
      .reassign-row {
        display: flex;
        gap: 8px;
      }
      .readonly-notice {
        background-color: var(--color-warning-subtle);
        color: var(--color-warning-text);
        border: 1px solid var(--color-warning-border);
        padding: 12px;
        border-radius: var(--radius-md);
        font-size: var(--font-size-meta);
      }
      .readonly-fields {
        margin: 0;
        display: flex;
        flex-direction: column;
        gap: 8px;
        dt {
          font-size: var(--font-size-caption);
          color: var(--color-text-muted);
        }
        dd {
          margin: 2px 0 0 0;
        }
      }
      .sub-title {
        margin: 0;
        font-size: var(--font-size-meta);
        font-weight: 600;
      }
      .obs-list,
      .event-list {
        list-style: none;
        padding: 0;
        margin: 0;
        li {
          padding: 8px 0;
          border-bottom: 1px solid var(--color-border-subtle);
          font-size: var(--font-size-meta);
        }
      }
      .event-badge {
        display: inline-block;
        margin: 0 6px;
        padding: 1px 6px;
        border-radius: var(--radius-sm);
        background-color: var(--color-surface-subtle);
        font-size: 11px;
      }
      .muted-text {
        color: var(--color-text-muted);
        font-size: var(--font-size-caption);
      }
    `,
  ],
})
export class WorkbenchPage implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly dialog = inject(Dialog);
  private readonly toast = inject(ToastService);
  protected readonly auth = inject(AuthService);
  private readonly badges = inject(SidebarBadgesService);

  protected readonly stats = signal<DashboardSummary | null>(null);
  protected readonly subnets = signal<Subnet[]>([]);
  protected readonly items = signal<IPRecordSummary[]>([]);
  protected readonly pagination = signal<PaginationMeta | null>(null);
  protected readonly loading = signal(false);

  protected readonly selectedDetail = signal<IPRecordDetail | null>(null);
  protected readonly selectedIds = signal<Set<number>>(new Set());
  protected readonly selectAllFiltered = signal(false);
  protected readonly batchRegisterOpen = signal(false);
  protected readonly batchReassignOpen = signal(false);

  protected filters = {
    search: '',
    subnet_id: '',
    presence: '',
    registration: '',
    expiring_soon: false,
    has_conflict: false,
    owner_me: false,
    sort_by: 'ip_address',
    sort_dir: 'asc',
    page: 1,
  };

  protected regForm = {
    owner_mail: '',
    device_type: '',
    hostname: '',
    notes: '',
    owner_username: '',
  };
  protected reassignTargetUser = '';
  protected batchRegForm = { owner_mail: '', device_type: '', notes: '' };
  protected batchReassignTarget = '';

  protected readonly selectedCount = computed(() =>
    this.selectAllFiltered() ? (this.pagination()?.total_items ?? 0) : this.selectedIds().size,
  );

  ngOnInit(): void {
    this.route.queryParams.subscribe((params) => {
      this.filters.search = params['search'] ?? '';
      this.filters.subnet_id = params['subnet_id'] ?? '';
      this.filters.presence = params['presence'] ?? '';
      this.filters.registration = params['registration'] ?? '';
      this.filters.expiring_soon = params['expiring_soon'] === 'true';
      this.filters.has_conflict = params['has_conflict'] === 'true';
      this.filters.owner_me = params['owner_me'] === 'true';
      this.filters.page = Number(params['page'] ?? 1);
      this.loadRecords();
    });

    this.loadStats();
    this.loadSubnets();
  }

  loadStats(): void {
    this.http.get<DashboardSummary>('/api/v1/dashboard/stats').subscribe({
      next: (res) => {
        this.stats.set(res);
        this.badges.openConflictsCount.set(res.open_conflicts_count);
      },
      error: () => {},
    });
  }

  loadSubnets(): void {
    this.http.get<{ pending_count: number; items: Subnet[] }>('/api/v1/subnets').subscribe({
      next: (res) => {
        this.subnets.set(res.items ?? []);
        this.badges.pendingSubnetsCount.set(res.pending_count);
      },
      error: () => {},
    });
  }

  applyFilters(page: number): void {
    this.filters.page = page;
    this.clearSelection();
    void this.router.navigate([], {
      relativeTo: this.route,
      queryParams: {
        search: this.filters.search || null,
        subnet_id: this.filters.subnet_id || null,
        presence: this.filters.presence || null,
        registration: this.filters.registration || null,
        expiring_soon: this.filters.expiring_soon ? 'true' : null,
        has_conflict: this.filters.has_conflict ? 'true' : null,
        owner_me: this.filters.owner_me ? 'true' : null,
        page: page > 1 ? page : null,
      },
    });
  }

  toggleSort(field: string): void {
    if (this.filters.sort_by === field) {
      this.filters.sort_dir = this.filters.sort_dir === 'asc' ? 'desc' : 'asc';
    } else {
      this.filters.sort_by = field;
      this.filters.sort_dir = 'asc';
    }
    this.loadRecords();
  }

  private buildHttpParams(): HttpParams {
    let p = new HttpParams().set('page', String(this.filters.page)).set('page_size', '50');
    if (this.filters.search) p = p.set('search', this.filters.search);
    if (this.filters.subnet_id) p = p.set('subnet_id', this.filters.subnet_id);
    if (this.filters.presence) p = p.set('presence', this.filters.presence);
    if (this.filters.registration) p = p.set('registration', this.filters.registration);
    if (this.filters.expiring_soon) p = p.set('expiring_soon', 'true');
    if (this.filters.has_conflict) p = p.set('has_conflict', 'true');
    if (this.filters.owner_me) p = p.set('owner_me', 'true');
    p = p.set('sort_by', this.filters.sort_by).set('sort_dir', this.filters.sort_dir);
    return p;
  }

  loadRecords(): void {
    this.loading.set(true);
    this.http
      .get<{ items: IPRecordSummary[]; pagination: PaginationMeta }>('/api/v1/ip-records', {
        params: this.buildHttpParams(),
      })
      .subscribe({
        next: (res) => {
          this.items.set(res.items ?? []);
          this.pagination.set(res.pagination);
          this.loading.set(false);
        },
        error: () => {
          this.loading.set(false);
        },
      });
  }

  exportExcel(): void {
    const queryStr = this.buildHttpParams().toString();
    window.open(`/api/v1/ip-records/export?${queryStr}`, '_blank');
  }

  openDetail(id: number): void {
    this.http.get<IPRecordDetail>(`/api/v1/ip-records/${id}`).subscribe({
      next: (res) => {
        this.selectedDetail.set(res);
        this.regForm = {
          owner_mail: res.owner_mail || this.auth.currentUser()?.username + '@wurzburg.local' || '',
          device_type: res.device_type || '',
          hostname: res.hostname || '',
          notes: res.notes || '',
          owner_username: '',
        };
      },
      error: () => {},
    });
  }

  saveRegistration(d: IPRecordDetail): void {
    if (d.registration === 'unregistered') {
      this.http
        .post<BatchActionResult>(
          '/api/v1/ip-records/actions/register',
          {
            ip_record_ids: [d.id],
            owner_mail: this.regForm.owner_mail,
            device_type: this.regForm.device_type,
            hostname: this.regForm.hostname,
            notes: this.regForm.notes,
            owner_username: this.regForm.owner_username || undefined,
          },
          { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
        )
        .subscribe({
          next: (res) => {
            if (res.succeeded_count > 0) {
              this.toast.show('登記成功', 'success');
              this.openDetail(d.id);
              this.loadRecords();
              this.loadStats();
            } else {
              this.toast.show('登記失敗', 'danger', res.results[0]?.reason);
            }
          },
          error: (err) => this.toast.show('登記失敗', 'danger', err?.error?.message),
        });
    } else {
      this.http
        .put<IPRecordDetail>(
          `/api/v1/ip-records/${d.id}`,
          {
            owner_mail: this.regForm.owner_mail,
            device_type: this.regForm.device_type,
            hostname: this.regForm.hostname,
            notes: this.regForm.notes,
          },
          { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
        )
        .subscribe({
          next: (res) => {
            this.selectedDetail.set(res);
            this.loadRecords();
            this.toast.show('變更已儲存', 'success');
          },
          error: (err) => this.toast.show('儲存失敗', 'danger', err?.error?.message),
        });
    }
  }

  singleAction(id: number, action: 'confirm' | 'release'): void {
    this.http
      .post<BatchActionResult>(
        `/api/v1/ip-records/actions/${action}`,
        { ip_record_ids: [id] },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          if (res.succeeded_count > 0) {
            this.toast.show(action === 'confirm' ? '已延長有效期一年' : '已釋放登記', 'success');
            this.openDetail(id);
            this.loadRecords();
            this.loadStats();
          } else {
            this.toast.show('操作失敗', 'danger', res.results[0]?.reason);
          }
        },
      });
  }

  reassignSingle(id: number): void {
    if (!this.reassignTargetUser) return;
    this.http
      .post<BatchActionResult>(
        '/api/v1/ip-records/actions/reassign',
        { ip_record_ids: [id], target_username: this.reassignTargetUser },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          if (res.succeeded_count > 0) {
            this.reassignTargetUser = '';
            this.toast.show('已改派擁有者', 'success');
            this.openDetail(id);
            this.loadRecords();
          } else {
            this.toast.show('改派失敗', 'danger', res.results[0]?.reason);
          }
        },
        error: (err) => this.toast.show('改派失敗', 'danger', err?.error?.message),
      });
  }

  allCurrentPageSelected(): boolean {
    const list = this.items();
    if (list.length === 0) return false;
    return this.selectAllFiltered() || list.every((i) => this.selectedIds().has(i.id));
  }

  toggleSelectPage(event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    this.selectAllFiltered.set(false);
    if (checked) {
      this.selectedIds.set(new Set(this.items().map((i) => i.id)));
    } else {
      this.selectedIds.set(new Set());
    }
  }

  toggleRow(id: number): void {
    this.selectAllFiltered.set(false);
    this.selectedIds.update((s) => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  clearSelection(): void {
    this.selectAllFiltered.set(false);
    this.selectedIds.set(new Set());
  }

  private getBatchSelector(): Record<string, unknown> {
    if (this.selectAllFiltered()) {
      return {
        select_all_filtered: true,
        filter: {
          search: this.filters.search,
          subnet_id: Number(this.filters.subnet_id) || 0,
          presence: this.filters.presence,
          registration: this.filters.registration,
          expiring_soon: this.filters.expiring_soon,
          has_conflict: this.filters.has_conflict,
          owner_me: this.filters.owner_me,
        },
      };
    }
    return { ip_record_ids: Array.from(this.selectedIds()) };
  }

  confirmBatchAction(action: 'release'): void {
    const ref = this.dialog.open<boolean>(ConfirmDialogComponent, {
      data: {
        title: '確認批次釋放登記',
        message: `即將針對所選的 ${this.selectedCount()} 筆位址釋放登記（權限不符者將自動略過）。是否繼續？`,
        confirmLabel: '確認批次釋放',
        danger: true,
      },
    });
    ref.closed.subscribe((ok) => {
      if (ok) this.executeBatch(action);
    });
  }

  executeBatch(action: 'confirm' | 'release'): void {
    this.http
      .post<BatchActionResult>(`/api/v1/ip-records/actions/${action}`, this.getBatchSelector(), {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (res) => {
          this.clearSelection();
          this.loadRecords();
          this.loadStats();
          this.toast.show(
            '批次作業完成',
            'success',
            `成功 ${res.succeeded_count} 筆，略過 ${res.skipped_count} 筆`,
          );
        },
        error: (err) => this.toast.show('批次作業失敗', 'danger', err?.error?.message),
      });
  }

  submitBatchRegister(): void {
    this.http
      .post<BatchActionResult>(
        '/api/v1/ip-records/actions/register',
        { ...this.getBatchSelector(), ...this.batchRegForm },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          this.batchRegisterOpen.set(false);
          this.clearSelection();
          this.loadRecords();
          this.loadStats();
          this.toast.show(
            '批次登記完成',
            'success',
            `成功 ${res.succeeded_count} 筆，略過 ${res.skipped_count} 筆`,
          );
        },
        error: (err) => this.toast.show('批次登記失敗', 'danger', err?.error?.message),
      });
  }

  submitBatchReassign(): void {
    this.http
      .post<BatchActionResult>(
        '/api/v1/ip-records/actions/reassign',
        { ...this.getBatchSelector(), target_username: this.batchReassignTarget },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          this.batchReassignOpen.set(false);
          this.clearSelection();
          this.loadRecords();
          this.toast.show(
            '批次改派完成',
            'success',
            `成功 ${res.succeeded_count} 筆，略過 ${res.skipped_count} 筆`,
          );
        },
        error: (err) => this.toast.show('批次改派失敗', 'danger', err?.error?.message),
      });
  }

  copyIP(ip: string): void {
    if (navigator?.clipboard) {
      void navigator.clipboard.writeText(ip);
      this.toast.show('已複製 IP 位址', 'info', ip);
    }
  }

  formatTime(iso: string): string {
    return new Date(iso).toLocaleString('zh-TW', { timeZone: 'Asia/Taipei', hour12: false });
  }

  formatDate(iso: string): string {
    return new Date(iso).toLocaleDateString('zh-TW', { timeZone: 'Asia/Taipei' });
  }
}
