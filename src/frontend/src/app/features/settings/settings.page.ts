import { Component, OnInit, inject, signal } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { Dialog } from '@angular/cdk/dialog';
import { AuthSettingsComponent } from './auth-settings.component';
import { CampusSettingsComponent } from './campus-settings.component';
import { MailSettingsComponent } from './mail-settings.component';
import { OuiSettingsComponent } from './oui-settings.component';
import { RetentionSettingsComponent } from './retention-settings.component';
import { SetupWizardDialogComponent } from './setup-wizard.dialog';

export type SettingsTab = 'campus' | 'auth' | 'mail' | 'oui' | 'retention';

/**
 * 系統設定頁（包含各功能子頁與重新開啟設定精靈入口）
 */
@Component({
  selector: 'app-settings-page',
  standalone: true,
  imports: [
    AuthSettingsComponent,
    CampusSettingsComponent,
    MailSettingsComponent,
    OuiSettingsComponent,
    RetentionSettingsComponent,
  ],
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
          <app-campus-settings (openWizard)="openSetupWizard()" />
        }
        @case ('auth') {
          <app-auth-settings />
        }
        @case ('mail') {
          <app-mail-settings />
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
export class SettingsPage implements OnInit {
  private readonly dialog = inject(Dialog);
  private readonly route = inject(ActivatedRoute);
  readonly activeTab = signal<SettingsTab>('campus');

  ngOnInit(): void {
    this.route.queryParams.subscribe((params) => {
      const tab = params['tab'] as SettingsTab | undefined;
      if (tab && ['campus', 'auth', 'mail', 'oui', 'retention'].includes(tab)) {
        this.activeTab.set(tab);
      }
    });
  }

  openSetupWizard(): void {
    this.dialog.open(SetupWizardDialogComponent, { disableClose: true });
  }
}
