import { Component, input } from '@angular/core';
import { LucideInbox } from '@lucide/angular';

@Component({
  selector: 'app-empty-state',
  standalone: true,
  imports: [LucideInbox],
  template: `
    <div class="empty-state" role="status">
      <div class="empty-icon-wrap" aria-hidden="true">
        <svg lucideInbox [size]="24" [strokeWidth]="1.75"></svg>
      </div>
      <h3 class="empty-title">{{ title() }}</h3>
      @if (description()) {
        <p class="empty-desc">{{ description() }}</p>
      }
      <div class="empty-actions">
        <ng-content />
      </div>
    </div>
  `,
  styles: [
    `
      .empty-state {
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        text-align: center;
        padding: 48px 24px;
        border: 1px dashed var(--color-border-default);
        border-radius: var(--radius-lg);
        background-color: var(--color-surface);
      }

      .empty-icon-wrap {
        width: 48px;
        height: 48px;
        border-radius: var(--radius-full);
        background-color: var(--color-surface-subtle);
        color: var(--color-text-muted);
        display: flex;
        align-items: center;
        justify-content: center;
        margin-bottom: 12px;
      }

      .empty-title {
        margin: 0 0 4px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
        color: var(--color-text-primary);
      }

      .empty-desc {
        margin: 0 0 16px 0;
        font-size: var(--font-size-body);
        color: var(--color-text-muted);
        max-width: 360px;
      }

      .empty-actions {
        display: flex;
        gap: 8px;
      }
    `,
  ],
})
export class EmptyStateComponent {
  readonly title = input.required<string>();
  readonly description = input<string>('');
}
