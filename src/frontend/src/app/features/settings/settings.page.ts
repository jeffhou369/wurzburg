import { Component, signal } from '@angular/core';
import { OuiSettingsComponent } from './oui-settings.component';
import { RetentionSettingsComponent } from './retention-settings.component';

export type SettingsTab = 'campus' | 'auth' | 'mail' | 'oui' | 'retention';

/**
 * 系統設定頁（預留各功能子頁插槽）
 */
@Component({
  selector: 'app-settings-page',
  standalone: true,
  imports: [OuiSettingsComponent, RetentionSettingsComponent],
  template: `
    <header class="page-header">
      <h1 class="page-title">系統設定</h1>
      <p class="page-subtitle">管理園區基本資訊、驗證來源、SMTP 通知、本地 OUI 資料與政策參數</p>
    </header>

    <div class="settings-tabs" role="tablist">
      <button
        type="button"
        role="tab"
        class="tab-pill"
        [class.active]="activeTab() === 'campus'"
        (click)="activeTab.set('campus')"
      >
        園區與精靈
      </button>
      <button
        type="button"
        role="tab"
        class="tab-pill"
        [class.active]="activeTab() === 'auth'"
        (click)="activeTab.set('auth')"
      >
        驗證來源 (AD / RADIUS)
      </button>
      <button
        type="button"
        role="tab"
        class="tab-pill"
        [class.active]="activeTab() === 'mail'"
        (click)="activeTab.set('mail')"
      >
        郵件與通知
      </button>
      <button
        type="button"
        role="tab"
        class="tab-pill"
        [class.active]="activeTab() === 'oui'"
        (click)="activeTab.set('oui')"
      >
        本地 OUI 資料庫
      </button>
      <button
        type="button"
        role="tab"
        class="tab-pill"
        [class.active]="activeTab() === 'retention'"
        (click)="activeTab.set('retention')"
      >
        自動清除與保存天數
      </button>
    </div>

    <div class="card">
      @switch (activeTab()) {
        @case ('campus') {
          <ng-content select="[slot=settings-campus]">
            <p class="slot-hint">園區顯示名稱、時區與重新開啟設定精靈（Ticket 32）。</p>
          </ng-content>
        }
        @case ('auth') {
          <ng-content select="[slot=settings-auth]">
            <p class="slot-hint">驗證來源選擇與 AD / RADIUS 連線設定（Ticket 07 / 21 / 22）。</p>
          </ng-content>
        }
        @case ('mail') {
          <ng-content select="[slot=settings-mail]">
            <p class="slot-hint">SMTP 與收件清單設定（Ticket 19 / 28 / 29 / 30）。</p>
          </ng-content>
        }
        @case ('oui') {
          <app-oui-settings />
        }
        @case ('retention') {
          <app-retention-settings />
        }
      }
    </div>
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
      .settings-tabs {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin-bottom: 20px;
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

        &:hover:not(.active) {
          background-color: var(--color-surface-hover);
        }
      }
      .slot-hint {
        margin: 0;
        color: var(--color-text-muted);
      }
    `,
  ],
})
export class SettingsPage {
  readonly activeTab = signal<SettingsTab>('campus');
}
