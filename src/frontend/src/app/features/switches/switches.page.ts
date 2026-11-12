import { Component } from '@angular/core';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';

@Component({
  selector: 'app-switches-page',
  standalone: true,
  imports: [EmptyStateComponent],
  template: `
    <header class="page-header">
      <h1 class="page-title">交換器與 ARP 同步</h1>
      <p class="page-subtitle">管理 ARP 來源交換器、VRF 讀取設定與即時輪詢紀錄</p>
    </header>
    <app-empty-state
      title="交換器管理介面"
      description="此頁面預留給交換器端點設定與輪詢同步狀態（Ticket 09 / 11）。"
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
export class SwitchesPage {}
