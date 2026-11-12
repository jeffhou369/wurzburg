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
import { sanitizeReturnUrl } from '../../core/safe-return-url';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { STATUS_TEXT } from '../../core/i18n/status.i18n';
import { UnfinishedChecklistComponent } from './unfinished-checklist.component';
import { WORKBENCH_TEXT } from './workbench.i18n';
import { SetupWizardDialogComponent } from '../settings/setup-wizard.dialog';
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
    UnfinishedChecklistComponent,
    LucideSearch,
    LucideDownload,
    LucideCopy,
    LucideRefreshCw,
    LucideCheckSquare,
  ],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ text.title }}</h1>
        <p class="page-subtitle">{{ text.subtitle }}</p>
      </div>
      @if (stats(); as st) {
        <div class="sync-header-badge">
          @if (st.sync_in_progress) {
            <app-status-pill variant="online" [label]="text.sync.running" />
          } @else if (st.last_sync_success) {
            <app-status-pill variant="online" [label]="text.sync.succeeded" />
          } @else {
            <app-status-pill variant="conflict" [label]="text.sync.failed" />
          }
          @if (st.last_sync_at) {
            <span class="font-mono muted-text">{{ formatTime(st.last_sync_at) }}</span>
          }
        </div>
      }
    </header>

    <section [attr.aria-label]="text.aria.unfinishedSettings">
      <app-unfinished-checklist (resumeWizard)="openSetupWizard()" />
    </section>

    <!-- 儀表板 KPI 列 (Ticket 16) -->
    @if (stats(); as st) {
      <section class="kpi-grid" [attr.aria-label]="text.aria.dashboard">
        <div class="card kpi-card">
          <div class="kpi-label">{{ text.kpi.managedTotal.label }}</div>
          <div class="kpi-value font-mono">{{ st.managed_total }}</div>
          <div class="kpi-sub">{{ text.kpi.managedTotal.sub(st.switches_count) }}</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">{{ text.kpi.online.label }}</div>
          <div class="kpi-value font-mono text-success">{{ st.online_count }}</div>
          <div class="kpi-sub">{{ text.kpi.online.sub }}</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">{{ text.kpi.offline.label }}</div>
          <div class="kpi-value font-mono text-muted">{{ st.offline_count }}</div>
          <div class="kpi-sub">{{ text.kpi.offline.sub }}</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">{{ text.kpi.unregisteredOnline.label }}</div>
          <div class="kpi-value font-mono text-warning">{{ st.unregistered_online_count }}</div>
          <div class="kpi-sub">{{ text.kpi.unregisteredOnline.sub }}</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">{{ text.kpi.expired.label }}</div>
          <div class="kpi-value font-mono text-danger">{{ st.expired_count }}</div>
          <div class="kpi-sub">{{ text.kpi.expired.sub }}</div>
        </div>
        <div class="card kpi-card">
          <div class="kpi-label">{{ text.kpi.conflicts.label }}</div>
          <div class="kpi-value font-mono text-danger">{{ st.open_conflicts_count }}</div>
          <div class="kpi-sub">{{ text.kpi.conflicts.sub }}</div>
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
            [placeholder]="text.toolbar.searchPlaceholder"
            [(ngModel)]="filters.search"
            (keyup.enter)="applyFilters(1)"
          />
        </div>
        <div class="toolbar-buttons">
          <button type="button" class="btn btn-secondary" (click)="applyFilters(1)">
            <svg lucideRefreshCw [size]="16"></svg>
            <span>{{ text.toolbar.apply }}</span>
          </button>
          <button type="button" class="btn btn-primary" (click)="exportExcel()">
            <svg lucideDownload [size]="16"></svg>
            <span>{{ text.toolbar.exportExcel(pagination()?.total_items ?? 0) }}</span>
          </button>
        </div>
      </div>

      <div class="filter-row">
        <select
          class="form-select filter-select"
          [(ngModel)]="filters.subnet_id"
          (change)="applyFilters(1)"
        >
          <option value="">{{ text.filters.allSubnets }}</option>
          @for (sub of subnets(); track sub.id) {
            <option [value]="sub.id">{{ sub.cidr }}</option>
          }
        </select>
        <select
          class="form-select filter-select"
          [(ngModel)]="filters.presence"
          (change)="applyFilters(1)"
        >
          <option value="">{{ text.filters.allPresence }}</option>
          <option value="online">{{ status.online }}</option>
          <option value="offline">{{ status.offline }}</option>
        </select>
        <select
          class="form-select filter-select"
          [(ngModel)]="filters.registration"
          (change)="applyFilters(1)"
        >
          <option value="">{{ text.filters.allRegistration }}</option>
          <option value="unregistered">{{ status.unregistered }}</option>
          <option value="registered">{{ status.registered }}</option>
          <option value="expired">{{ status.expired }}</option>
        </select>
        <label class="filter-chip" [class.active]="filters.expiring_soon">
          <input type="checkbox" [(ngModel)]="filters.expiring_soon" (change)="applyFilters(1)" />
          <span>{{ text.filters.expiringSoon }}</span>
        </label>
        <label class="filter-chip" [class.active]="filters.has_conflict">
          <input type="checkbox" [(ngModel)]="filters.has_conflict" (change)="applyFilters(1)" />
          <span>{{ text.filters.hasConflict }}</span>
        </label>
        <label class="filter-chip" [class.active]="filters.owner_me">
          <input type="checkbox" [(ngModel)]="filters.owner_me" (change)="applyFilters(1)" />
          <span>{{ text.filters.ownerMe }}</span>
        </label>
        <label class="filter-chip" [class.active]="filters.pending_confirmation">
          <input
            type="checkbox"
            [(ngModel)]="filters.pending_confirmation"
            (change)="applyFilters(1)"
          />
          <span>{{ text.filters.pendingConfirmation }}</span>
        </label>
      </div>
    </section>

    <!-- 批次操作列 (Ticket 27) -->
    @if (selectedCount() > 0) {
      <div class="batch-bar">
        <div class="batch-info">
          <svg lucideCheckSquare [size]="16"></svg>
          @if (selectAllFiltered()) {
            <span>{{ text.batchBar.allFiltered(pagination()?.total_items ?? 0) }}</span>
            <button type="button" class="btn btn-ghost btn-sm" (click)="clearSelection()">
              {{ text.batchBar.clear }}
            </button>
          } @else {
            <span>{{ text.batchBar.pageSelected(selectedIds().size) }}</span>
            @if ((pagination()?.total_items ?? 0) > items().length) {
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                (click)="selectAllFiltered.set(true)"
              >
                {{ text.batchBar.selectAllFiltered(pagination()?.total_items ?? 0) }}
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
            {{ text.batchBar.register }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" (click)="executeBatch('confirm')">
            {{ text.batchBar.confirm }}
          </button>
          <button
            type="button"
            class="btn btn-danger-soft btn-sm"
            (click)="confirmBatchAction('release')"
          >
            {{ text.batchBar.release }}
          </button>
          @if (auth.isAdmin()) {
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              (click)="batchReassignOpen.set(true)"
            >
              {{ text.batchBar.reassign }}
            </button>
          }
        </div>
      </div>
    }

    <!-- IP 主列表 (Ticket 15) -->
    @if (items().length === 0 && !loading()) {
      <app-empty-state [title]="text.empty.title" [description]="text.empty.description" />
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
                    [attr.aria-label]="text.aria.selectPage"
                  />
                </th>
                <th>{{ text.columns.status }}</th>
                <th class="sortable" (click)="toggleSort('ip_address')">
                  {{ text.columns.ipAddress }}
                </th>
                <th>{{ text.columns.macAddress }}</th>
                <th>{{ text.columns.vendor }}</th>
                <th>{{ text.columns.deviceType }}</th>
                <th>{{ text.columns.subnetSwitch }}</th>
                <th class="sortable" (click)="toggleSort('owner_username')">
                  {{ text.columns.owner }}
                </th>
                <th class="sortable" (click)="toggleSort('expires_at')">
                  {{ text.columns.expiresAt }}
                </th>
                <th class="sortable" (click)="toggleSort('last_seen_at')">
                  {{ text.columns.lastSeen }}
                </th>
                <th>{{ text.columns.actions }}</th>
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
                      [attr.aria-label]="text.aria.selectRow(item.ip_address)"
                    />
                  </td>
                  <td>
                    <div class="status-combo">
                      @if (item.has_conflict) {
                        <app-status-pill variant="conflict" [label]="status.conflict" />
                      }
                      <app-status-pill
                        [variant]="item.presence === 'online' ? 'online' : 'offline'"
                        [label]="item.presence === 'online' ? status.online : status.offline"
                      />
                      @if (item.registration === 'unregistered') {
                        <app-status-pill variant="unregistered" [label]="status.unregistered" />
                      } @else if (item.registration === 'expired') {
                        <app-status-pill variant="expired" [label]="status.expired" />
                      } @else if (item.expiring_soon) {
                        <app-status-pill variant="expiring" [label]="status.expiring" />
                      } @else {
                        <app-status-pill variant="registered" [label]="status.registered" />
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
                      [attr.aria-label]="text.aria.copyIp"
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
            <span class="muted-text">{{
              pager.summary(pg.total_items, pg.page, pg.total_pages || 1)
            }}</span>
            <div class="pg-actions">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                [disabled]="pg.page <= 1"
                (click)="applyFilters(pg.page - 1)"
              >
                {{ pager.previous }}
              </button>
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                [disabled]="pg.page >= pg.total_pages"
                (click)="applyFilters(pg.page + 1)"
              >
                {{ pager.next }}
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
              <strong>{{ text.detail.mac }}</strong><span class="font-mono">{{
                d.mac_address
              }}</span>
            </div>
            <div>
              <strong>{{ text.detail.vendor }}</strong>{{ d.vendor_name }}
            </div>
            <div>
              <strong>{{ text.detail.subnet }}</strong><span class="font-mono">{{
                d.subnet_cidr
              }}</span>
            </div>
            <div>
              <strong>{{ text.detail.state }}</strong>{{ d.state_label }}
            </div>
          </div>

          @if (d.registration === 'unregistered' || d.can_edit) {
            <form class="form-stack" (ngSubmit)="saveRegistration(d)">
              <div class="field-group">
                <label for="regMail">{{ text.form.ownerMail }}</label>
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
                <label for="regType">{{ text.form.deviceType }}</label>
                <input
                  id="regType"
                  name="regType"
                  type="text"
                  class="form-input"
                  [(ngModel)]="regForm.device_type"
                />
              </div>
              <div class="field-group">
                <label for="regHost">{{ text.form.hostname }}</label>
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
                  >{{ text.form.notes }}
                  <span class="public-note">{{ text.form.notesPublic }}</span></label
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
                  <label for="regOwnerUser">{{ text.form.ownerUsername }}</label>
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
                    {{ text.form.confirm }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-danger-soft"
                    (click)="singleAction(d.id, 'release')"
                  >
                    {{ text.form.release }}
                  </button>
                }
                <button type="submit" class="btn btn-primary">
                  {{ d.registration === 'unregistered' ? text.form.register : text.form.save }}
                </button>
              </div>
            </form>

            @if (d.registration !== 'unregistered' && auth.isAdmin()) {
              <div class="reassign-box">
                <label for="reassignTarget">{{ text.form.reassignLabel }}</label>
                <div class="reassign-row">
                  <input
                    id="reassignTarget"
                    type="text"
                    class="form-input"
                    [placeholder]="text.form.reassignPlaceholder"
                    [(ngModel)]="reassignTargetUser"
                  />
                  <button type="button" class="btn btn-secondary" (click)="reassignSingle(d.id)">
                    {{ text.form.reassign }}
                  </button>
                </div>
              </div>
            }
          } @else {
            <div class="readonly-notice">
              {{ text.readonly.ownedPrefix
              }}<strong>{{ d.owner_display_name || d.owner_username }}</strong
              >{{ text.readonly.ownedSuffix(d.owner_mail) }}
            </div>
            <dl class="readonly-fields">
              <div>
                <dt>{{ text.readonly.deviceType }}</dt>
                <dd>{{ d.device_type || '—' }}</dd>
              </div>
              <div>
                <dt>{{ text.readonly.hostname }}</dt>
                <dd class="font-mono">{{ d.hostname || '—' }}</dd>
              </div>
              <div>
                <dt>{{ text.readonly.notes }}</dt>
                <dd>{{ d.notes || '—' }}</dd>
              </div>
              <div>
                <dt>{{ text.readonly.expiresAt }}</dt>
                <dd class="font-mono">{{ d.expires_at ? formatDate(d.expires_at) : '—' }}</dd>
              </div>
            </dl>
          }
        }
      </div>

      <div slot="observations" class="obs-slot">
        @if (selectedDetail(); as d) {
          <div class="online-explainer">
            <strong>{{ text.observations.explainerTitle }}</strong>
            {{ text.observations.explainerBody }}
          </div>

          <h3 class="sub-title">{{ text.observations.sourcesTitle(d.sightings.length) }}</h3>
          @if (d.sightings.length === 0) {
            <p class="muted-text">{{ text.observations.noSources }}</p>
          } @else {
            <ul class="obs-list">
              @for (s of d.sightings; track s.switch_name + s.vrf_name) {
                <li>
                  <div>
                    <strong>{{ s.switch_name }}</strong> ({{ s.vrf_name }} /
                    <span class="font-mono">{{ s.if_name }}</span>)
                  </div>
                  <div class="font-mono muted-text">
                    {{ text.observations.sightingLine(s.mac_address, formatTime(s.last_seen_at)) }}
                  </div>
                </li>
              }
            </ul>
          }

          <h3 class="sub-title">{{ text.observations.historyTitle }}</h3>
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
      [title]="text.batchRegister.title"
      (closed)="batchRegisterOpen.set(false)"
    >
      <form class="form-stack" (ngSubmit)="submitBatchRegister()">
        <p class="muted-text">{{ text.batchRegister.hint }}</p>
        <div class="field-group">
          <label for="bMail">{{ text.batchRegister.ownerMail }}</label>
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
          <label for="bType">{{ text.batchRegister.deviceType }}</label>
          <input
            id="bType"
            name="bType"
            type="text"
            class="form-input"
            [(ngModel)]="batchRegForm.device_type"
          />
        </div>
        <div class="field-group">
          <label for="bNotes">{{ text.batchRegister.notes }}</label>
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
            {{ actions.cancel }}
          </button>
          <button type="submit" class="btn btn-primary">{{ text.batchRegister.submit }}</button>
        </div>
      </form>
    </app-sheet-container>

    <!-- 批次改派 Sheet -->
    <app-sheet-container
      [open]="batchReassignOpen()"
      [title]="text.batchReassign.title"
      (closed)="batchReassignOpen.set(false)"
    >
      <form class="form-stack" (ngSubmit)="submitBatchReassign()">
        <div class="field-group">
          <label for="bTarget">{{ text.batchReassign.target }}</label>
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
            {{ actions.cancel }}
          </button>
          <button type="submit" class="btn btn-primary">{{ text.batchReassign.submit }}</button>
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

  protected readonly text = WORKBENCH_TEXT;
  protected readonly status = STATUS_TEXT;
  protected readonly pager = SHELL_TEXT.pagination;
  protected readonly actions = SHELL_TEXT.actions;

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
    pending_confirmation: false,
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

  protected currentTimezone = 'Asia/Taipei';

  ngOnInit(): void {
    this.route.queryParams.subscribe((params) => {
      if (params['wizard'] === 'true' && this.auth.isAdmin()) {
        this.openSetupWizard(params['returnUrl']);
      }
      this.filters.search = params['search'] ?? '';
      this.filters.subnet_id = params['subnet_id'] ?? '';
      this.filters.presence = params['presence'] ?? '';
      this.filters.registration = params['registration'] ?? '';
      this.filters.expiring_soon = params['expiring_soon'] === 'true';
      this.filters.has_conflict = params['has_conflict'] === 'true';
      this.filters.owner_me = params['owner_me'] === 'true';
      this.filters.pending_confirmation = params['pending_confirmation'] === 'true';
      this.filters.page = Number(params['page'] ?? 1);
      this.loadRecords();
    });

    this.loadStats();
    this.loadSubnets();
    this.loadCampus();
  }

  loadCampus(): void {
    this.http.get<{ campus_name: string; timezone: string }>('/api/v1/settings/campus').subscribe({
      next: (res) => {
        if (res.timezone) {
          this.currentTimezone = res.timezone;
        }
      },
      error: () => {},
    });
  }

  /**
   * 開啟設定精靈。returnUrl 為登入或改密碼前原本要去的站內網址：
   * 精靈結束後回到該處；沒有的話移除網址上的 wizard 參數，避免之後切換篩選又重開精靈。
   */
  openSetupWizard(rawReturnUrl?: unknown): void {
    const returnUrl = sanitizeReturnUrl(rawReturnUrl);
    const ref = this.dialog.open(SetupWizardDialogComponent, { disableClose: true });
    ref.closed.subscribe(() => {
      this.loadStats();
      this.loadSubnets();
      if (returnUrl !== '/') {
        void this.router.navigateByUrl(returnUrl);
      } else if (this.route.snapshot.queryParamMap.get('wizard')) {
        void this.router.navigate([], {
          relativeTo: this.route,
          queryParams: { wizard: null, returnUrl: null },
          queryParamsHandling: 'merge',
        });
      }
    });
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
        pending_confirmation: this.filters.pending_confirmation ? 'true' : null,
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
    if (this.filters.pending_confirmation) p = p.set('pending_confirmation', 'true');
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
          // 不憑空組出信箱：沿用既有登記信箱，其次是目前帳號的信箱，否則留白讓使用者填寫
          owner_mail: res.owner_mail || this.auth.currentUser()?.mail || '',
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
              this.toast.show(this.text.toast.registerSucceeded, 'success');
              this.openDetail(d.id);
              this.loadRecords();
              this.loadStats();
            } else {
              this.toast.show(this.text.toast.registerFailed, 'danger', res.results[0]?.reason);
            }
          },
          error: (err) =>
            this.toast.show(this.text.toast.registerFailed, 'danger', err?.error?.message),
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
            this.toast.show(this.text.toast.saveSucceeded, 'success');
          },
          error: (err) =>
            this.toast.show(this.text.toast.saveFailed, 'danger', err?.error?.message),
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
            this.toast.show(
              action === 'confirm' ? this.text.toast.confirmed : this.text.toast.released,
              'success',
            );
            this.openDetail(id);
            this.loadRecords();
            this.loadStats();
          } else {
            this.toast.show(this.text.toast.actionFailed, 'danger', res.results[0]?.reason);
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
            this.toast.show(this.text.toast.reassigned, 'success');
            this.openDetail(id);
            this.loadRecords();
          } else {
            this.toast.show(this.text.toast.reassignFailed, 'danger', res.results[0]?.reason);
          }
        },
        error: (err) =>
          this.toast.show(this.text.toast.reassignFailed, 'danger', err?.error?.message),
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
          pending_confirmation: this.filters.pending_confirmation,
        },
      };
    }
    return { ip_record_ids: Array.from(this.selectedIds()) };
  }

  confirmBatchAction(action: 'release'): void {
    const ref = this.dialog.open<boolean>(ConfirmDialogComponent, {
      data: {
        title: this.text.releaseDialog.title,
        message: this.text.releaseDialog.message(this.selectedCount()),
        confirmLabel: this.text.releaseDialog.confirm,
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
            this.text.toast.batchDone,
            'success',
            this.text.toast.batchResult(res.succeeded_count, res.skipped_count),
          );
        },
        error: (err) => this.toast.show(this.text.toast.batchFailed, 'danger', err?.error?.message),
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
            this.text.toast.batchRegisterDone,
            'success',
            this.text.toast.batchResult(res.succeeded_count, res.skipped_count),
          );
        },
        error: (err) =>
          this.toast.show(this.text.toast.batchRegisterFailed, 'danger', err?.error?.message),
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
            this.text.toast.batchReassignDone,
            'success',
            this.text.toast.batchResult(res.succeeded_count, res.skipped_count),
          );
        },
        error: (err) =>
          this.toast.show(this.text.toast.batchReassignFailed, 'danger', err?.error?.message),
      });
  }

  copyIP(ip: string): void {
    if (navigator?.clipboard) {
      void navigator.clipboard.writeText(ip);
      this.toast.show(this.text.toast.ipCopied, 'info', ip);
    }
  }

  formatTime(iso: string): string {
    try {
      return new Date(iso).toLocaleString('zh-TW', {
        timeZone: this.currentTimezone,
        hour12: false,
      });
    } catch {
      return new Date(iso).toLocaleString('zh-TW', { hour12: false });
    }
  }

  formatDate(iso: string): string {
    try {
      return new Date(iso).toLocaleDateString('zh-TW', { timeZone: this.currentTimezone });
    } catch {
      return new Date(iso).toLocaleDateString('zh-TW');
    }
  }
}
