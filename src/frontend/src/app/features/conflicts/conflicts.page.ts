import { Component } from '@angular/core';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';

@Component({
  selector: 'app-conflicts-page',
  standalone: true,
  imports: [EmptyStateComponent],
  template: `
    <header class="page-header">
      <h1 class="page-title">MAC 衝突告警</h1>
      <p class="page-subtitle">檢視並裁決同位址出現多個 MAC 或已登記位址變更 MAC 之情形</p>
    </header>
    <app-empty-state
      title="目前無衝突紀錄"
      description="此頁面預留給衝突偵測與裁決功能（Ticket 25 / 31）。"
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
export class ConflictsPage {}
