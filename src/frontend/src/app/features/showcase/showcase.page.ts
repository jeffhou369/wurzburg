import { Component, inject, signal } from '@angular/core';
import { Dialog } from '@angular/cdk/dialog';
import { LucidePlus, LucideRefreshCw, LucideTrash2, LucideCopy } from '@lucide/angular';
import { StatusPillComponent } from '../../shared/components/status-pill.component';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { ToastService } from '../../shared/components/toast.service';
import { IpRecordSheetComponent } from '../../shared/components/ip-record-sheet.component';
import { ConfirmDialogComponent } from '../../shared/components/confirm-dialog.component';
import { AuthService } from '../../core/auth.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';

@Component({
  selector: 'app-showcase-page',
  standalone: true,
  imports: [
    StatusPillComponent,
    EmptyStateComponent,
    IpRecordSheetComponent,
    LucidePlus,
    LucideRefreshCw,
    LucideTrash2,
    LucideCopy,
  ],
  template: `
    <header class="page-header">
      <h1 class="page-title">設計系統與基礎元件展示</h1>
      <p class="page-subtitle">
        驗證 EasyCLIProxyAPI 淺色主題 Token、無障礙對比、狀態膠囊、表格灰階與 CDK 浮層
      </p>
    </header>

    <div class="showcase-grid">
      <section class="card">
        <h2 class="section-title">1. 按鈕規範（Buttons）</h2>
        <div class="row-wrap">
          <button type="button" class="btn btn-primary">
            <svg lucidePlus [size]="16" [strokeWidth]="2"></svg>
            主要操作（新增登記）
          </button>
          <button type="button" class="btn btn-secondary">
            <svg lucideRefreshCw [size]="16" [strokeWidth]="2"></svg>
            次要操作（立即同步）
          </button>
          <button type="button" class="btn btn-ghost">幽靈按鈕（檢視明細）</button>
          <button type="button" class="btn btn-danger-soft">柔和危險（停止同步）</button>
          <button type="button" class="btn btn-danger-solid" (click)="openDeleteDialog()">
            <svg lucideTrash2 [size]="16" [strokeWidth]="2"></svg>
            實心危險（刪除確認）
          </button>
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">2. 狀態燈號與膠囊（不可僅靠顏色）</h2>
        <div class="row-wrap">
          <app-status-pill variant="online" label="在線" />
          <app-status-pill variant="offline" label="離線" />
          <app-status-pill variant="unregistered" label="未登記" />
          <app-status-pill variant="registered" label="已登記" />
          <app-status-pill variant="expiring" label="即將到期" />
          <app-status-pill variant="expired" label="已到期" />
          <app-status-pill variant="conflict" label="衝突" />
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">3. 互動測試（CDK 浮層、Toast、角色與側欄徽章）</h2>
        <div class="row-wrap">
          <button type="button" class="btn btn-secondary" (click)="sheetOpen.set(true)">
            開啟右側 IP Sheet
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            (click)="
              toast.show('同步完成', 'success', '已成功自 core-sw-01 讀取 1,240 筆 ARP 紀錄')
            "
          >
            觸發成功 Toast
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            (click)="
              toast.show('偵測到新 MAC 衝突', 'danger', '10.10.10.50 同時出現兩個不同的 MAC 位址')
            "
          >
            觸發衝突 Toast
          </button>
          <button type="button" class="btn btn-secondary" (click)="toggleRole()">
            切換檢視身分（目前：{{ auth.isAdmin() ? '系統管理員' : '一般使用者' }}）
          </button>
          <button type="button" class="btn btn-secondary" (click)="toggleBadges()">
            切換側欄提示計數
          </button>
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">4. 表格外框與離線灰階樣式</h2>
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>狀態</th>
                <th>IP 位址</th>
                <th>MAC 位址</th>
                <th>網段</th>
                <th>擁有者</th>
                <th>動作</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>
                  <div class="status-combo">
                    <app-status-pill variant="online" label="在線" />
                    <app-status-pill variant="unregistered" label="未登記" />
                  </div>
                </td>
                <td class="font-mono">10.10.10.50</td>
                <td class="font-mono">b8:27:eb:11:22:33</td>
                <td class="font-mono">10.10.10.0/24</td>
                <td>—</td>
                <td>
                  <button type="button" class="btn btn-ghost btn-sm" aria-label="複製 IP">
                    <svg lucideCopy [size]="14" [strokeWidth]="2"></svg>
                  </button>
                </td>
              </tr>
              <tr class="row-offline">
                <td>
                  <div class="status-combo">
                    <app-status-pill variant="offline" label="離線" />
                    <app-status-pill variant="expired" label="已到期" />
                  </div>
                </td>
                <td class="font-mono">10.10.10.88</td>
                <td class="font-mono">00:50:56:89:ab:cd</td>
                <td class="font-mono">10.10.10.0/24</td>
                <td>chen@example.com</td>
                <td>
                  <button type="button" class="btn btn-ghost btn-sm" aria-label="複製 IP">
                    <svg lucideCopy [size]="14" [strokeWidth]="2"></svg>
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">5. 空狀態（Empty State）</h2>
        <app-empty-state
          title="沒有待處理的未登記 IP"
          description="所有納管網段內在線裝置皆已完成負責人登記。"
        />
      </section>
    </div>

    <app-ip-record-sheet
      [open]="sheetOpen()"
      ipAddress="10.10.10.50"
      (closed)="sheetOpen.set(false)"
    >
      <div slot="registration">
        <p>這裡是預留的登記表單插槽（展示狀態）。</p>
      </div>
      <div slot="observations">
        <p>這裡是預留的觀測與歷程插槽（展示狀態）。</p>
      </div>
    </app-ip-record-sheet>
  `,
  styles: [
    `
      .page-header {
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
      .showcase-grid {
        display: flex;
        flex-direction: column;
        gap: 20px;
      }
      .section-title {
        margin: 0 0 16px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
      }
      .row-wrap {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        align-items: center;
      }
      .table-wrap {
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
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
          vertical-align: middle;
        }

        tbody tr:last-child td {
          border-bottom: none;
        }

        tbody tr:hover {
          background-color: var(--color-surface-hover);
        }
      }
      .status-combo {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
      }
    `,
  ],
})
export class ShowcasePage {
  private readonly dialog = inject(Dialog);
  protected readonly toast = inject(ToastService);
  protected readonly auth = inject(AuthService);
  protected readonly badges = inject(SidebarBadgesService);
  protected readonly sheetOpen = signal(false);

  openDeleteDialog(): void {
    const ref = this.dialog.open<boolean>(ConfirmDialogComponent, {
      data: {
        title: '確認將網段改為不納管',
        message:
          '此變更將直接刪除該網段下 42 筆 IP 紀錄（其中 12 筆已登記），且無法復原。是否繼續？',
        confirmLabel: '確認刪除',
        danger: true,
      },
    });

    ref.closed.subscribe((confirmed) => {
      if (confirmed) {
        this.toast.show('已確認刪除操作', 'warning', '明細已寫入稽核紀錄');
      }
    });
  }

  toggleRole(): void {
    this.auth.setRole(this.auth.isAdmin() ? 'user' : 'admin');
  }

  toggleBadges(): void {
    if (this.badges.pendingSubnetsCount() === 0) {
      this.badges.pendingSubnetsCount.set(3);
      this.badges.openConflictsCount.set(2);
    } else {
      this.badges.pendingSubnetsCount.set(0);
      this.badges.openConflictsCount.set(0);
    }
  }
}
