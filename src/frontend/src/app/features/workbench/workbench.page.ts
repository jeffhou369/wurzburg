import { Component } from '@angular/core';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';

/**
 * 首頁工作臺（預留插槽容器）
 *
 * 插槽配置：
 * - [slot="checklist"]：第 32 張（設定精靈與未完成清單）填入
 * - [slot="dashboard"]：第 16 張（儀表板）填入
 * - [slot="ip-table"] ：第 15 張（首頁的 IP 表格與篩選）填入
 */
@Component({
  selector: 'app-workbench-page',
  standalone: true,
  imports: [EmptyStateComponent],
  template: `
    <header class="page-header">
      <div>
        <h1 class="page-title">IP 總覽</h1>
        <p class="page-subtitle">納管網段內所有 IPv4 位址之即時狀態與登記管理</p>
      </div>
    </header>

    <div class="workbench-slots">
      <section aria-label="尚未完成的設定">
        <ng-content select="[slot=checklist]" />
      </section>

      <section aria-label="統計儀表板">
        <ng-content select="[slot=dashboard]">
          <div class="card slot-card">
            <span class="slot-hint">儀表板預留插槽（Ticket 16）</span>
          </div>
        </ng-content>
      </section>

      <section aria-label="IP 管理表格">
        <ng-content select="[slot=ip-table]">
          <app-empty-state
            title="IP 管理表尚無資料"
            description="等待後續功能模組填入表格並開始輪詢交換器。"
          />
        </ng-content>
      </section>
    </div>
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
        color: var(--color-text-primary);
      }

      .page-subtitle {
        margin: 0;
        font-size: var(--font-size-body);
        color: var(--color-text-muted);
      }

      .workbench-slots {
        display: flex;
        flex-direction: column;
        gap: 20px;
      }

      .slot-card {
        padding: 16px 24px;
        background-color: var(--color-surface-subtle);
      }

      .slot-hint {
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
    `,
  ],
})
export class WorkbenchPage {}
