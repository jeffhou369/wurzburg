import { Component } from '@angular/core';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';

@Component({
  selector: 'app-audit-page',
  standalone: true,
  imports: [EmptyStateComponent],
  template: `
    <header class="page-header">
      <h1 class="page-title">稽核紀錄</h1>
      <p class="page-subtitle">追查所有登入、登記變更、網段調整、刪除明細與系統設定歷史</p>
    </header>
    <app-empty-state
      title="尚無稽核紀錄"
      description="此頁面預留給稽核日誌查詢介面（Ticket 08）。"
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
export class AuditPage {}
