import { Component } from '@angular/core';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';

@Component({
  selector: 'app-subnets-page',
  standalone: true,
  imports: [EmptyStateComponent],
  template: `
    <header class="page-header">
      <h1 class="page-title">網段管理</h1>
      <p class="page-subtitle">檢視並管理由 ARP 來源交換器發現之 IPv4 網段與排除範圍</p>
    </header>
    <app-empty-state
      title="網段管理介面"
      description="此頁面預留給網段發現、狀態切換與排除範圍功能（Ticket 12 / 24）。"
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
export class SubnetsPage {}
