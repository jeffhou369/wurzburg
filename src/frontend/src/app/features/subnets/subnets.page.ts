import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { Dialog } from '@angular/cdk/dialog';
import { LucidePlus, LucideTrash2 } from '@lucide/angular';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { SheetContainerComponent } from '../../shared/components/sheet-container.component';
import { ConfirmDialogComponent } from '../../shared/components/confirm-dialog.component';
import { ToastService } from '../../shared/components/toast.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import { AuthService } from '../../core/auth.service';
import { SUBNETS_TEXT } from './subnets.i18n';
import type { components } from '../../core/api.types';

type Subnet = components['schemas']['Subnet'];
type SubnetDetail = components['schemas']['SubnetDetail'];
type UndeterminedPrefix = components['schemas']['UndeterminedPrefix'];
type DeletePreviewResponse = components['schemas']['DeletePreviewResponse'];

@Component({
  selector: 'app-subnets-page',
  standalone: true,
  imports: [FormsModule, EmptyStateComponent, SheetContainerComponent, LucidePlus, LucideTrash2],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ text.title }}</h1>
        <p class="page-subtitle">{{ text.subtitle }}</p>
      </div>
      @if (auth.isAdmin()) {
        <button type="button" class="btn btn-primary" (click)="addModalOpen.set(true)">
          <svg lucidePlus [size]="16" [strokeWidth]="2"></svg>
          <span>{{ text.addManual }}</span>
        </button>
      }
    </header>

    <div class="filter-pills">
      <button
        type="button"
        class="pill-btn"
        [class.active]="stateFilter() === ''"
        (click)="setFilter('')"
      >
        {{ text.filters.all }}
      </button>
      <button
        type="button"
        class="pill-btn"
        [class.active]="stateFilter() === 'pending'"
        (click)="setFilter('pending')"
      >
        {{ text.filters.pending(badges.pendingSubnetsCount()) }}
      </button>
      <button
        type="button"
        class="pill-btn"
        [class.active]="stateFilter() === 'managed'"
        (click)="setFilter('managed')"
      >
        {{ text.filters.managed }}
      </button>
      <button
        type="button"
        class="pill-btn"
        [class.active]="stateFilter() === 'unmanaged'"
        (click)="setFilter('unmanaged')"
      >
        {{ text.filters.unmanaged }}
      </button>
    </div>

    @if (subnets().length === 0) {
      <app-empty-state [title]="text.empty.title" [description]="text.empty.description" />
    } @else {
      <div class="subnet-grid">
        @for (s of subnets(); track s.id) {
          <article class="card subnet-card" (click)="openDetail(s.id)">
            <div class="subnet-top">
              <span class="font-mono subnet-cidr">{{ s.cidr }}</span>
              @switch (s.state) {
                @case ('pending') {
                  <span class="status-pill status-unregistered"
                    ><span class="status-dot"></span><span>{{ text.state.pending }}</span></span
                  >
                }
                @case ('managed') {
                  <span class="status-pill status-online"
                    ><span class="status-dot"></span><span>{{ text.state.managed }}</span></span
                  >
                }
                @default {
                  <span class="status-pill status-offline"
                    ><span class="status-dot"></span
                    ><span>{{ text.state.unmanaged(s.exclude_reason) }}</span></span
                  >
                }
              }
            </div>
            @if (s.source_missing) {
              <div class="source-warn">{{ text.card.sourceMissing }}</div>
            }
            <p class="subnet-desc">{{ s.description || '—' }}</p>
            <div class="subnet-meta font-mono">
              <span>{{ text.card.recorded(s.ip_count) }}</span>
              <span>{{ text.card.online(s.online_count) }}</span>
            </div>
          </article>
        }
      </div>
    }

    @if (undetermined().length > 0) {
      <section class="card undetermined-section">
        <h2 class="section-title">{{ text.undetermined.title(undetermined().length) }}</h2>
        <p class="section-desc">
          {{ text.undetermined.description }}
        </p>
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ text.undetermined.columns.ipAddress }}</th>
                <th>{{ text.undetermined.columns.macAddress }}</th>
                <th>{{ text.undetermined.columns.switchName }}</th>
                <th>{{ text.undetermined.columns.vrf }}</th>
                <th>{{ text.undetermined.columns.interface }}</th>
                <th>{{ text.undetermined.columns.lastSeen }}</th>
              </tr>
            </thead>
            <tbody>
              @for (u of undetermined(); track u.ip_address) {
                <tr>
                  <td class="font-mono">{{ u.ip_address }}</td>
                  <td class="font-mono">{{ u.mac_address }}</td>
                  <td>{{ u.switch_name }}</td>
                  <td>{{ u.vrf_name }}</td>
                  <td class="font-mono">{{ u.if_name }}</td>
                  <td class="font-mono">{{ u.last_seen_at }}</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </section>
    }

    <app-sheet-container
      [open]="selectedDetail() !== null"
      [title]="
        selectedDetail() ? text.detail.titleWithCidr(selectedDetail()!.cidr) : text.detail.title
      "
      (closed)="selectedDetail.set(null)"
    >
      @if (selectedDetail(); as d) {
        <div class="detail-stack">
          @if (auth.isAdmin()) {
            <div class="detail-block">
              <h3>{{ text.detail.stateHeading }}</h3>
              <div class="state-actions">
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  (click)="changeState(d, 'managed')"
                  [disabled]="d.state === 'managed'"
                >
                  {{ text.detail.setManaged }}
                </button>
                <select class="form-select reason-select" [(ngModel)]="unmanageReason">
                  <option value="dhcp">{{ text.detail.reasons.dhcp }}</option>
                  <option value="wireless">{{ text.detail.reasons.wireless }}</option>
                  <option value="other">{{ text.detail.reasons.other }}</option>
                </select>
                <button
                  type="button"
                  class="btn btn-danger-soft btn-sm"
                  (click)="changeState(d, 'unmanaged')"
                >
                  {{ text.detail.setUnmanaged }}
                </button>
              </div>
            </div>
          }

          <div class="detail-block">
            <h3>{{ text.detail.gatewayHeading }}</h3>
            @if (d.gateways.length === 0) {
              <p class="muted-text">{{ text.detail.noGateways }}</p>
            } @else {
              <ul class="gw-list">
                @for (g of d.gateways; track g.ip_address) {
                  <li>
                    <span class="font-mono">{{ g.ip_address }}</span>
                    <span class="muted-text"
                      >{{ g.switch_name }} · {{ g.vrf_name }} · {{ g.if_name }}</span
                    >
                  </li>
                }
              </ul>
            }
          </div>

          <div class="detail-block">
            <h3>{{ text.detail.exclusionHeading }}</h3>
            @for (ex of d.exclusions; track ex.id) {
              <div class="excl-row">
                <div>
                  <span class="font-mono">{{ ex.start_ip }} – {{ ex.end_ip }}</span>
                  <span class="excl-tag">{{ ex.reason }}</span>
                </div>
                @if (auth.isAdmin()) {
                  <button
                    type="button"
                    class="btn btn-ghost btn-sm"
                    (click)="removeExclusion(d.id, ex.id)"
                    [attr.aria-label]="text.detail.removeExclusion"
                  >
                    <svg lucideTrash2 [size]="14"></svg>
                  </button>
                }
              </div>
            }
            @if (auth.isAdmin()) {
              <form class="excl-form" (ngSubmit)="addExclusion(d.id)">
                <input
                  type="text"
                  class="form-input font-mono"
                  [placeholder]="text.detail.exclusionStart"
                  [(ngModel)]="newExcl.start_ip"
                  name="startIp"
                  required
                />
                <input
                  type="text"
                  class="form-input font-mono"
                  [placeholder]="text.detail.exclusionEnd"
                  [(ngModel)]="newExcl.end_ip"
                  name="endIp"
                  required
                />
                <select class="form-select" [(ngModel)]="newExcl.reason" name="exclReason">
                  <option value="dhcp">{{ text.detail.exclusionReasons.dhcp }}</option>
                  <option value="wireless">{{ text.detail.exclusionReasons.wireless }}</option>
                  <option value="other">{{ text.detail.exclusionReasons.other }}</option>
                </select>
                <button type="submit" class="btn btn-secondary btn-sm">
                  {{ text.detail.addExclusion }}
                </button>
              </form>
            }
          </div>
        </div>
      }
    </app-sheet-container>

    <app-sheet-container
      [open]="addModalOpen()"
      [title]="text.manual.title"
      (closed)="addModalOpen.set(false)"
    >
      <form class="form-stack" (ngSubmit)="createManualSubnet()">
        <div class="field-group">
          <label for="manualCidr">IPv4 CIDR</label>
          <input
            id="manualCidr"
            name="manualCidr"
            type="text"
            class="form-input font-mono"
            placeholder="10.10.50.0/24"
            [(ngModel)]="manualForm.cidr"
            required
          />
        </div>
        <div class="field-group">
          <label for="manualDesc">{{ text.manual.description }}</label>
          <input
            id="manualDesc"
            name="manualDesc"
            type="text"
            class="form-input"
            [(ngModel)]="manualForm.description"
          />
        </div>
        <div class="sheet-actions">
          <button type="button" class="btn btn-secondary" (click)="addModalOpen.set(false)">
            {{ text.actions.cancel }}
          </button>
          <button type="submit" class="btn btn-primary">{{ text.manual.submit }}</button>
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
        gap: 16px;
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
      .filter-pills {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 20px;
      }
      .pill-btn {
        background-color: var(--color-surface);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-full);
        padding: 6px 14px;
        font-size: var(--font-size-meta);
        cursor: pointer;
        &.active {
          background-color: var(--color-accent);
          color: var(--color-text-on-accent);
          border-color: var(--color-accent);
        }
      }
      .subnet-grid {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
        gap: 16px;
        margin-bottom: 28px;
      }
      .subnet-card {
        cursor: pointer;
        padding: 18px;
        &:hover {
          border-color: var(--color-border-hover);
        }
      }
      .subnet-top {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 8px;
        margin-bottom: 8px;
      }
      .subnet-cidr {
        font-size: 16px;
        font-weight: 600;
      }
      .source-warn {
        font-size: 12px;
        color: var(--color-warning-text);
        margin-bottom: 6px;
      }
      .subnet-desc {
        margin: 0 0 12px 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
      .subnet-meta {
        display: flex;
        justify-content: space-between;
        font-size: var(--font-size-caption);
        color: var(--color-text-secondary);
        border-top: 1px solid var(--color-border-subtle);
        padding-top: 10px;
      }
      .undetermined-section {
        margin-top: 12px;
      }
      .section-title {
        margin: 0 0 4px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
      }
      .section-desc {
        margin: 0 0 14px 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
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
          padding: 8px 12px;
          text-align: left;
        }
        td {
          padding: 8px 12px;
          border-bottom: 1px solid var(--color-border-subtle);
        }
      }
      .detail-stack {
        display: flex;
        flex-direction: column;
        gap: 24px;
      }
      .detail-block h3 {
        margin: 0 0 10px 0;
        font-size: var(--font-size-meta);
        font-weight: 600;
      }
      .state-actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        align-items: center;
      }
      .reason-select {
        width: auto;
      }
      .gw-list {
        list-style: none;
        padding: 0;
        margin: 0;
        li {
          display: flex;
          justify-content: space-between;
          padding: 6px 0;
          border-bottom: 1px solid var(--color-border-subtle);
        }
      }
      .excl-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 8px 0;
        border-bottom: 1px solid var(--color-border-subtle);
      }
      .excl-tag {
        margin-left: 8px;
        font-size: 11px;
        padding: 2px 6px;
        background-color: var(--color-surface-subtle);
        border-radius: var(--radius-sm);
      }
      .excl-form {
        display: grid;
        grid-template-columns: 1fr 1fr auto auto;
        gap: 8px;
        margin-top: 12px;
      }
      .form-stack {
        display: flex;
        flex-direction: column;
        gap: 16px;
      }
      .field-group {
        display: flex;
        flex-direction: column;
        gap: 6px;
      }
      .sheet-actions {
        display: flex;
        justify-content: flex-end;
        gap: 8px;
      }
      .muted-text {
        color: var(--color-text-muted);
        font-size: var(--font-size-meta);
      }
    `,
  ],
})
export class SubnetsPage implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly dialog = inject(Dialog);
  private readonly toast = inject(ToastService);
  protected readonly badges = inject(SidebarBadgesService);
  protected readonly auth = inject(AuthService);
  protected readonly text = SUBNETS_TEXT;

  protected readonly subnets = signal<Subnet[]>([]);
  protected readonly undetermined = signal<UndeterminedPrefix[]>([]);
  protected readonly stateFilter = signal('');
  protected readonly selectedDetail = signal<SubnetDetail | null>(null);
  protected readonly addModalOpen = signal(false);

  protected unmanageReason = 'dhcp';
  protected newExcl = { start_ip: '', end_ip: '', reason: 'dhcp' };
  protected manualForm = { cidr: '', description: '' };

  ngOnInit(): void {
    this.loadSubnets();
    this.loadUndetermined();
  }

  setFilter(state: string): void {
    this.stateFilter.set(state);
    this.loadSubnets();
  }

  loadSubnets(): void {
    const q = this.stateFilter() ? `?state=${this.stateFilter()}` : '';
    this.http.get<{ pending_count: number; items: Subnet[] }>(`/api/v1/subnets${q}`).subscribe({
      next: (res) => {
        this.subnets.set(res.items ?? []);
        this.badges.pendingSubnetsCount.set(res.pending_count);
      },
      error: () => {},
    });
  }

  loadUndetermined(): void {
    this.http.get<{ items: UndeterminedPrefix[] }>('/api/v1/subnets/undetermined').subscribe({
      next: (res) => this.undetermined.set(res.items ?? []),
      error: () => {},
    });
  }

  openDetail(id: number): void {
    this.http.get<SubnetDetail>(`/api/v1/subnets/${id}`).subscribe({
      next: (res) => this.selectedDetail.set(res),
      error: () => {},
    });
  }

  createManualSubnet(): void {
    this.http
      .post('/api/v1/subnets', this.manualForm, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: () => {
          this.addModalOpen.set(false);
          this.manualForm = { cidr: '', description: '' };
          this.loadSubnets();
          this.toast.show(this.text.toast.created, 'success');
        },
        error: (err) =>
          this.toast.show(this.text.toast.createFailed, 'danger', err?.error?.message),
      });
  }

  changeState(subnet: SubnetDetail, targetState: 'managed' | 'unmanaged'): void {
    if (targetState === 'managed') {
      this.http
        .put<SubnetDetail>(
          `/api/v1/subnets/${subnet.id}/state`,
          { state: 'managed' },
          { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
        )
        .subscribe({
          next: (res) => {
            this.selectedDetail.set(res);
            this.loadSubnets();
            this.toast.show(this.text.toast.setManaged, 'success');
          },
        });
      return;
    }

    this.http
      .post<DeletePreviewResponse>(
        `/api/v1/subnets/${subnet.id}/state/preview`,
        { state: 'unmanaged' },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (prev) => {
          const execute = (token?: string) => {
            this.http
              .put<SubnetDetail>(
                `/api/v1/subnets/${subnet.id}/state`,
                { state: 'unmanaged', exclude_reason: this.unmanageReason, preview_token: token },
                { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
              )
              .subscribe({
                next: (res) => {
                  this.selectedDetail.set(res);
                  this.loadSubnets();
                  this.toast.show(this.text.toast.setUnmanaged, 'info');
                },
                error: (err) =>
                  this.toast.show(this.text.toast.changeFailed, 'danger', err?.error?.message),
              });
          };

          if (prev.requires_confirm) {
            const ref = this.dialog.open<boolean>(ConfirmDialogComponent, {
              data: {
                title: this.text.unmanageDialog.title,
                message: this.text.unmanageDialog.message(
                  prev.total_ip_records,
                  prev.owned_ip_records,
                ),
                confirmLabel: this.text.unmanageDialog.confirm,
                danger: true,
              },
              role: 'alertdialog',
              ariaLabel: this.text.unmanageDialog.title,
              ariaLabelledBy: 'confirm-dialog-title',
              ariaDescribedBy: 'confirm-dialog-message',
            });
            ref.closed.subscribe((ok) => {
              if (ok) execute(prev.preview_token);
            });
          } else {
            execute();
          }
        },
      });
  }

  addExclusion(subnetId: number): void {
    this.http
      .post<DeletePreviewResponse>(`/api/v1/subnets/${subnetId}/exclusions/preview`, this.newExcl, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (prev) => {
          const execute = (token?: string) => {
            this.http
              .post(
                `/api/v1/subnets/${subnetId}/exclusions`,
                { ...this.newExcl, preview_token: token },
                { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
              )
              .subscribe({
                next: () => {
                  this.newExcl = { start_ip: '', end_ip: '', reason: 'dhcp' };
                  this.openDetail(subnetId);
                  this.loadSubnets();
                  this.toast.show(this.text.toast.exclusionAdded, 'success');
                },
                error: (err) =>
                  this.toast.show(
                    this.text.toast.exclusionAddFailed,
                    'danger',
                    err?.error?.message,
                  ),
              });
          };

          if (prev.requires_confirm) {
            const ref = this.dialog.open<boolean>(ConfirmDialogComponent, {
              data: {
                title: this.text.exclusionDialog.title,
                message: this.text.exclusionDialog.message(
                  prev.total_ip_records,
                  prev.owned_ip_records,
                ),
                confirmLabel: this.text.exclusionDialog.confirm,
                danger: true,
              },
              role: 'alertdialog',
              ariaLabel: this.text.exclusionDialog.title,
              ariaLabelledBy: 'confirm-dialog-title',
              ariaDescribedBy: 'confirm-dialog-message',
            });
            ref.closed.subscribe((ok) => {
              if (ok) execute(prev.preview_token);
            });
          } else {
            execute();
          }
        },
        error: (err) =>
          this.toast.show(this.text.toast.exclusionInvalid, 'danger', err?.error?.message),
      });
  }

  removeExclusion(subnetId: number, exclusionId: number): void {
    this.http
      .delete(`/api/v1/subnets/${subnetId}/exclusions/${exclusionId}`, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: () => {
          this.openDetail(subnetId);
          this.toast.show(this.text.toast.exclusionRemoved, 'info');
        },
      });
  }
}
