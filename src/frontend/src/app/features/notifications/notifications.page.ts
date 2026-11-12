import { Component } from '@angular/core';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';

@Component({
  selector: 'app-notifications-page',
  standalone: true,
  imports: [EmptyStateComponent],
  template: `
    <header class="page-header">
      <h1 class="page-title">通知紀錄</h1>
      <p class="page-subtitle">檢視未登記彙整、每週總表與到期催認之 SMTP 寄送紀錄與去重狀態</p>
    </header>
    <app-empty-state
      title="尚無通知寄送紀錄"
      description="此頁面預留給郵件通知與去重管理（Ticket 19 / 28 / 29 / 30）。"
    />
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
    `,
  ],
})
export class NotificationsPage {}
