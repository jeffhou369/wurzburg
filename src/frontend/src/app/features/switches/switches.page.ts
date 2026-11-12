import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { Dialog } from '@angular/cdk/dialog';
import {
  LucidePlus,
  LucideRefreshCw,
  LucideTrash2,
  LucideTriangleAlert,
  LucidePlay,
  LucideChevronDown,
  LucideChevronUp,
} from '@lucide/angular';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { ToastService } from '../../shared/components/toast.service';
import { SheetContainerComponent } from '../../shared/components/sheet-container.component';
import { ConfirmDialogComponent } from '../../shared/components/confirm-dialog.component';
import type { components } from '../../core/api.types';

type SwitchItem = components['schemas']['Switch'];
type TestReadResult = components['schemas']['TestReadResult'];

@Component({
  selector: 'app-switches-page',
  standalone: true,
  imports: [
    FormsModule,
    EmptyStateComponent,
    SheetContainerComponent,
    LucidePlus,
    LucideRefreshCw,
    LucideTrash2,
    LucideTriangleAlert,
    LucidePlay,
    LucideChevronDown,
    LucideChevronUp,
  ],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">交換器與 ARP 同步</h1>
        <p class="page-subtitle">
          管理 ARP 來源交換器（IOS-XE / NX-OS）、VRF 讀取設定與即時輪詢狀態
        </p>
      </div>
      <div class="header-actions">
        @if (syncInProgress()) {
          <span class="status-pill status-online">
            <span class="status-dot"></span>
            <span>同步中</span>
          </span>
        }
        @if (roundIncomplete()) {
          <span class="status-pill status-unregistered">
            <span class="status-dot"></span>
            <span>本輪未完成</span>
          </span>
        }
        <button
          type="button"
          class="btn btn-secondary"
          (click)="triggerSync()"
          [disabled]="syncInProgress()"
        >
          <svg lucideRefreshCw [size]="16" [strokeWidth]="2"></svg>
          <span>立即同步</span>
        </button>
        <button type="button" class="btn btn-primary" (click)="openAddSwitch()">
          <svg lucidePlus [size]="16" [strokeWidth]="2"></svg>
          <span>新增交換器</span>
        </button>
      </div>
    </header>

    @if (loading()) {
      <div class="card"><p class="muted-text">正在載入交換器設定...</p></div>
    } @else if (switches().length === 0) {
      <app-empty-state
        title="尚未新增 ARP 來源交換器"
        description="新增至少一台擔任 L3 閘道的 Cisco 交換器以開始探索網段與追蹤 IP。"
      />
    } @else {
      <div class="switch-cards">
        @for (sw of switches(); track sw.id) {
          <section class="card switch-card">
            <div class="sw-header">
              <div class="sw-title-group">
                <h2 class="sw-name">{{ sw.name }}</h2>
                <span class="font-mono sw-ip">{{ sw.mgmt_ip }}</span>
                <span class="tag-pill">{{ sw.platform.toUpperCase() }}</span>
                <span class="tag-pill">{{ sw.snmp_version.toUpperCase() }}</span>
                <span class="tag-pill">{{ sw.role_label }}</span>
                @if (!sw.enabled) {
                  <span class="status-pill status-offline"
                    ><span class="status-dot"></span><span>已停用</span></span
                  >
                }
              </div>

              <div class="sw-actions">
                <button type="button" class="btn btn-secondary btn-sm" (click)="openAddVRF(sw)">
                  新增 VRF 設定
                </button>
                <button
                  type="button"
                  class="btn btn-danger-soft btn-sm"
                  (click)="toggleSwitchEnabled(sw)"
                >
                  {{ sw.enabled ? '停止輪詢' : '恢復輪詢' }}
                </button>
                <button
                  type="button"
                  class="btn btn-ghost btn-sm"
                  (click)="confirmDeleteSwitch(sw)"
                  aria-label="刪除交換器"
                >
                  <svg lucideTrash2 [size]="16"></svg>
                </button>
              </div>
            </div>

            @if (sw.has_v3_warning) {
              <div class="vrf-warning">
                <svg lucideTriangleAlert [size]="16"></svg>
                <span>僅設定預設全域 VRF，交換器上非預設 VRF 內的 ARP 位址將不會被看到。</span>
              </div>
            }

            <div class="vrf-list">
              @for (vrf of sw.vrf_configs; track vrf.id) {
                <div class="vrf-row">
                  <div class="vrf-info">
                    <span class="vrf-name">{{ vrf.vrf_name }}</span>
                    @if (vrf.snmp_context) {
                      <span class="font-mono vrf-ctx">Context: {{ vrf.snmp_context }}</span>
                    }
                    @if (vrf.has_credential) {
                      <span class="cred-badge">憑證已設定</span>
                    }
                  </div>

                  <div class="vrf-status">
                    @switch (vrf.last_status) {
                      @case ('success') {
                        <span class="status-pill status-online"
                          ><span class="status-dot"></span
                          ><span>成功（{{ vrf.last_learned_count }} 筆）</span></span
                        >
                      }
                      @case ('failed') {
                        <span class="status-pill status-conflict"
                          ><span class="status-dot"></span
                          ><span>失敗：{{ vrf.last_error_class }}</span></span
                        >
                        @if (vrf.last_error_summary) {
                          <button
                            type="button"
                            class="btn btn-ghost btn-sm"
                            (click)="toggleError(vrf.id)"
                          >
                            @if (expandedErrors().has(vrf.id)) {
                              <svg lucideChevronUp [size]="14"></svg>
                            } @else {
                              <svg lucideChevronDown [size]="14"></svg>
                            }
                          </button>
                        }
                      }
                      @case ('running') {
                        <span class="status-pill status-online"
                          ><span class="status-dot"></span><span>同步中</span></span
                        >
                      }
                      @default {
                        <span class="status-pill status-offline"
                          ><span class="status-dot"></span><span>尚未同步</span></span
                        >
                      }
                    }

                    @if (vrf.last_poll_at) {
                      <span class="font-mono muted-text">{{ formatTime(vrf.last_poll_at) }}</span>
                    }

                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      (click)="testRead(sw.id, vrf.id)"
                    >
                      <svg lucidePlay [size]="14"></svg>
                      <span>測試讀取</span>
                    </button>

                    @if (!vrf.is_default) {
                      <button
                        type="button"
                        class="btn btn-ghost btn-sm"
                        (click)="deleteVRF(sw.id, vrf.id)"
                        aria-label="刪除 VRF 設定"
                      >
                        <svg lucideTrash2 [size]="14"></svg>
                      </button>
                    }
                  </div>
                </div>
                @if (vrf.last_status === 'failed' && expandedErrors().has(vrf.id)) {
                  <div class="error-detail font-mono">{{ vrf.last_error_summary }}</div>
                }
              }
            </div>
          </section>
        }
      </div>
    }

    <app-sheet-container
      [open]="addSwitchOpen()"
      title="新增 ARP 來源交換器"
      (closed)="addSwitchOpen.set(false)"
    >
      <form class="form-stack" (ngSubmit)="saveNewSwitch()">
        <div class="field-group">
          <label for="swName">顯示名稱</label>
          <input
            id="swName"
            name="swName"
            type="text"
            class="form-input"
            [(ngModel)]="newSw.name"
            required
          />
        </div>
        <div class="field-group">
          <label for="swIp">管理 IPv4</label>
          <input
            id="swIp"
            name="swIp"
            type="text"
            class="form-input font-mono"
            [(ngModel)]="newSw.mgmt_ip"
            required
          />
        </div>
        <div class="field-group">
          <label for="swPlatform">作業系統平台</label>
          <select
            id="swPlatform"
            name="swPlatform"
            class="form-select"
            [(ngModel)]="newSw.platform"
          >
            <option value="ios-xe">Cisco IOS-XE</option>
            <option value="nx-os">Cisco NX-OS</option>
          </select>
        </div>
        <div class="field-group">
          <label for="swVer">SNMP 版本</label>
          <select id="swVer" name="swVer" class="form-select" [(ngModel)]="newSw.snmp_version">
            <option value="v3">SNMPv3（生產建議）</option>
            <option value="v2c">SNMPv2c（僅限唯讀）</option>
          </select>
        </div>
        @if (newSw.snmp_version === 'v2c') {
          <div class="field-group">
            <label for="swComm">唯讀 Community</label>
            <input
              id="swComm"
              name="swComm"
              type="password"
              class="form-input"
              [(ngModel)]="newSw.snmp_community"
              required
            />
          </div>
        } @else {
          <div class="field-group">
            <label for="v3User">SNMPv3 使用者名稱</label>
            <input
              id="v3User"
              name="v3User"
              type="text"
              class="form-input"
              [(ngModel)]="newSw.v3_username"
              required
            />
          </div>
          <div class="field-group">
            <label for="v3AuthPass">驗證密語（Auth Passphrase）</label>
            <input
              id="v3AuthPass"
              name="v3AuthPass"
              type="password"
              class="form-input"
              [(ngModel)]="newSw.v3_auth_pass"
            />
          </div>
          <div class="field-group">
            <label for="v3PrivPass">加密密語（Priv Passphrase）</label>
            <input
              id="v3PrivPass"
              name="v3PrivPass"
              type="password"
              class="form-input"
              [(ngModel)]="newSw.v3_priv_pass"
            />
          </div>
        }
        <div class="sheet-actions">
          <button type="button" class="btn btn-secondary" (click)="addSwitchOpen.set(false)">
            取消
          </button>
          <button type="submit" class="btn btn-primary">儲存交換器</button>
        </div>
      </form>
    </app-sheet-container>

    <app-sheet-container
      [open]="addVrfTarget() !== null"
      title="新增非全域 VRF 讀取設定"
      (closed)="addVrfTarget.set(null)"
    >
      @if (addVrfTarget(); as target) {
        <form class="form-stack" (ngSubmit)="saveNewVRF()">
          <div class="field-group">
            <label for="vrfName">VRF 顯示名稱</label>
            <input
              id="vrfName"
              name="vrfName"
              type="text"
              class="form-input"
              [(ngModel)]="newVrf.vrf_name"
              required
            />
          </div>
          @if (target.snmp_version === 'v3') {
            <div class="field-group">
              <label for="vrfCtx">SNMPv3 Context 名稱</label>
              <input
                id="vrfCtx"
                name="vrfCtx"
                type="text"
                class="form-input font-mono"
                [(ngModel)]="newVrf.snmp_context"
                required
              />
            </div>
          } @else {
            <div class="field-group">
              <label for="vrfComm">該 VRF 專屬唯讀 Community</label>
              <input
                id="vrfComm"
                name="vrfComm"
                type="password"
                class="form-input"
                [(ngModel)]="newVrf.snmp_community"
                required
              />
            </div>
          }
          <div class="sheet-actions">
            <button type="button" class="btn btn-secondary" (click)="addVrfTarget.set(null)">
              取消
            </button>
            <button type="submit" class="btn btn-primary">新增 VRF</button>
          </div>
        </form>
      }
    </app-sheet-container>
  `,
  styles: [
    `
      .page-header {
        display: flex;
        flex-wrap: wrap;
        justify-content: space-between;
        align-items: flex-start;
        gap: 16px;
        margin-bottom: 24px;
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
      .header-actions {
        display: flex;
        align-items: center;
        gap: 10px;
      }
      .switch-cards {
        display: flex;
        flex-direction: column;
        gap: 16px;
      }
      .sw-header {
        display: flex;
        flex-wrap: wrap;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        margin-bottom: 14px;
      }
      .sw-title-group {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 8px;
      }
      .sw-name {
        margin: 0;
        font-size: var(--font-size-panel-title);
        font-weight: 600;
      }
      .sw-ip {
        color: var(--color-text-muted);
        font-size: var(--font-size-meta);
      }
      .tag-pill {
        background-color: var(--color-surface-subtle);
        color: var(--color-text-secondary);
        padding: 2px 8px;
        border-radius: var(--radius-sm);
        font-size: var(--font-size-caption);
        font-weight: 500;
      }
      .sw-actions {
        display: flex;
        align-items: center;
        gap: 8px;
      }
      .vrf-warning {
        display: flex;
        align-items: center;
        gap: 8px;
        background-color: var(--color-warning-subtle);
        border: 1px solid var(--color-warning-border);
        color: var(--color-warning-text);
        padding: 8px 12px;
        border-radius: var(--radius-md);
        font-size: var(--font-size-meta);
        margin-bottom: 14px;
      }
      .vrf-list {
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
        overflow: hidden;
      }
      .vrf-row {
        display: flex;
        flex-wrap: wrap;
        justify-content: space-between;
        align-items: center;
        gap: 12px;
        padding: 12px 16px;
        border-bottom: 1px solid var(--color-border-subtle);
        &:last-child {
          border-bottom: none;
        }
      }
      .vrf-info {
        display: flex;
        align-items: center;
        gap: 10px;
      }
      .vrf-name {
        font-weight: 600;
      }
      .vrf-ctx {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .cred-badge {
        font-size: 11px;
        background-color: var(--color-surface-subtle);
        padding: 2px 6px;
        border-radius: var(--radius-sm);
        color: var(--color-text-muted);
      }
      .vrf-status {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 10px;
      }
      .error-detail {
        background-color: var(--color-danger-subtle);
        color: var(--color-danger-text);
        padding: 10px 16px;
        font-size: 12px;
        border-bottom: 1px solid var(--color-border-subtle);
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
        label {
          font-size: var(--font-size-meta);
          font-weight: 500;
        }
      }
      .sheet-actions {
        display: flex;
        justify-content: flex-end;
        gap: 10px;
        padding-top: 8px;
      }
      .muted-text {
        color: var(--color-text-muted);
        font-size: var(--font-size-meta);
      }
    `,
  ],
})
export class SwitchesPage implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly dialog = inject(Dialog);
  private readonly toast = inject(ToastService);

  protected readonly switches = signal<SwitchItem[]>([]);
  protected readonly loading = signal(true);
  protected readonly syncInProgress = signal(false);
  protected readonly roundIncomplete = signal(false);
  protected readonly expandedErrors = signal<Set<number>>(new Set());

  protected readonly addSwitchOpen = signal(false);
  protected readonly addVrfTarget = signal<SwitchItem | null>(null);

  protected newSw = {
    name: '',
    mgmt_ip: '',
    platform: 'ios-xe',
    snmp_version: 'v3',
    snmp_community: '',
    v3_username: '',
    v3_auth_pass: '',
    v3_priv_pass: '',
  };

  protected newVrf = {
    vrf_name: '',
    snmp_context: '',
    snmp_community: '',
  };

  ngOnInit(): void {
    this.loadSwitches();
    this.loadSyncStatus();
  }

  loadSwitches(): void {
    this.loading.set(true);
    this.http.get<{ items: SwitchItem[] }>('/api/v1/switches').subscribe({
      next: (res) => {
        this.switches.set(res.items ?? []);
        this.loading.set(false);
      },
      error: () => {
        this.switches.set([]);
        this.loading.set(false);
      },
    });
  }

  loadSyncStatus(): void {
    this.http
      .get<{ in_progress: boolean; round_incomplete: boolean }>('/api/v1/sync/status')
      .subscribe({
        next: (res) => {
          this.syncInProgress.set(res.in_progress);
          this.roundIncomplete.set(res.round_incomplete);
        },
        error: () => {},
      });
  }

  triggerSync(): void {
    this.http
      .post('/api/v1/sync/trigger', {}, { headers: { 'X-Requested-With': 'XMLHttpRequest' } })
      .subscribe({
        next: () => {
          this.syncInProgress.set(true);
          this.toast.show('已觸發立即同步', 'info');
        },
        error: (err) => this.toast.show('無法觸發同步', 'warning', err?.error?.message),
      });
  }

  openAddSwitch(): void {
    this.newSw = {
      name: '',
      mgmt_ip: '',
      platform: 'ios-xe',
      snmp_version: 'v3',
      snmp_community: '',
      v3_username: '',
      v3_auth_pass: '',
      v3_priv_pass: '',
    };
    this.addSwitchOpen.set(true);
  }

  saveNewSwitch(): void {
    this.http
      .post<SwitchItem>('/api/v1/switches', this.newSw, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: () => {
          this.addSwitchOpen.set(false);
          this.loadSwitches();
          this.toast.show('交換器新增成功', 'success');
        },
        error: (err) => this.toast.show('新增交換器失敗', 'danger', err?.error?.message),
      });
  }

  openAddVRF(sw: SwitchItem): void {
    this.newVrf = { vrf_name: '', snmp_context: '', snmp_community: '' };
    this.addVrfTarget.set(sw);
  }

  saveNewVRF(): void {
    const sw = this.addVrfTarget();
    if (!sw) return;
    this.http
      .post(`/api/v1/switches/${sw.id}/vrfs`, this.newVrf, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: () => {
          this.addVrfTarget.set(null);
          this.loadSwitches();
          this.toast.show('VRF 讀取設定已新增', 'success');
        },
        error: (err) => this.toast.show('新增失敗', 'danger', err?.error?.message),
      });
  }

  deleteVRF(swId: number, vrfId: number): void {
    this.http
      .delete(`/api/v1/switches/${swId}/vrfs/${vrfId}`, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: () => {
          this.loadSwitches();
          this.toast.show('VRF 讀取設定已刪除', 'info');
        },
        error: (err) => this.toast.show('刪除失敗', 'danger', err?.error?.message),
      });
  }

  toggleSwitchEnabled(sw: SwitchItem): void {
    this.http
      .put(
        `/api/v1/switches/${sw.id}`,
        {
          name: sw.name,
          mgmt_ip: sw.mgmt_ip,
          platform: sw.platform,
          snmp_version: sw.snmp_version,
          enabled: !sw.enabled,
        },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: () => this.loadSwitches(),
        error: (err) => this.toast.show('更新失敗', 'danger', err?.error?.message),
      });
  }

  confirmDeleteSwitch(sw: SwitchItem): void {
    const ref = this.dialog.open<boolean>(ConfirmDialogComponent, {
      data: {
        title: `確認刪除交換器「${sw.name}」`,
        message: '刪除交換器會立即移除其名下的 ARP 觀測來源，是否確定刪除？',
        confirmLabel: '確認刪除',
        danger: true,
      },
    });
    ref.closed.subscribe((ok) => {
      if (ok) {
        this.http
          .delete(`/api/v1/switches/${sw.id}`, {
            headers: { 'X-Requested-With': 'XMLHttpRequest' },
          })
          .subscribe({
            next: () => {
              this.loadSwitches();
              this.toast.show('交換器已刪除', 'info');
            },
          });
      }
    });
  }

  testRead(swId: number, vrfId: number): void {
    this.http
      .post<TestReadResult>(
        `/api/v1/switches/${swId}/vrfs/${vrfId}/test-read`,
        {},
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          if (res.success) {
            this.toast.show(
              '測試讀取成功',
              'success',
              `讀取到 ${res.arp_count} 筆 ARP、${res.subnet_count} 個介面網段`,
            );
          } else {
            this.toast.show(`測試讀取失敗 (${res.error_class})`, 'danger', res.error_summary);
          }
        },
        error: (err) => this.toast.show('測試執行失敗', 'danger', err?.error?.message),
      });
  }

  toggleError(vrfId: number): void {
    this.expandedErrors.update((s) => {
      const next = new Set(s);
      if (next.has(vrfId)) next.delete(vrfId);
      else next.add(vrfId);
      return next;
    });
  }

  formatTime(iso: string): string {
    return new Date(iso).toLocaleString('zh-TW', { timeZone: 'Asia/Taipei', hour12: false });
  }
}
