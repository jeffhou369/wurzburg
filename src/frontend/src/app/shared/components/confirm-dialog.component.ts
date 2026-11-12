import { Component, inject } from '@angular/core';
import { DIALOG_DATA, DialogRef } from '@angular/cdk/dialog';
import { LucideTriangleAlert } from '@lucide/angular';

export interface ConfirmDialogData {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

@Component({
  selector: 'app-confirm-dialog',
  standalone: true,
  imports: [LucideTriangleAlert],
  template: `
    <div class="dialog-panel" role="alertdialog" aria-modal="true">
      <div class="dialog-header">
        @if (data.danger) {
          <span class="danger-icon" aria-hidden="true">
            <svg lucideTriangleAlert [size]="20" [strokeWidth]="2"></svg>
          </span>
        }
        <h2 class="dialog-title">{{ data.title }}</h2>
      </div>
      <p class="dialog-message">{{ data.message }}</p>
      <div class="dialog-actions">
        <button type="button" class="btn btn-secondary" (click)="dialogRef.close(false)">
          {{ data.cancelLabel ?? '取消' }}
        </button>
        <button
          type="button"
          [class]="data.danger ? 'btn btn-danger-solid' : 'btn btn-primary'"
          (click)="dialogRef.close(true)"
        >
          {{ data.confirmLabel ?? '確認' }}
        </button>
      </div>
    </div>
  `,
  styles: [
    `
      .dialog-panel {
        background-color: var(--color-surface);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-lg);
        padding: 24px;
        width: min(460px, calc(100vw - 32px));
        box-shadow: var(--shadow-dialog);
      }

      .dialog-header {
        display: flex;
        align-items: center;
        gap: 10px;
        margin-bottom: 12px;
      }

      .danger-icon {
        color: var(--color-danger);
        display: inline-flex;
      }

      .dialog-title {
        margin: 0;
        font-size: var(--font-size-dialog-title);
        font-weight: 600;
        color: var(--color-text-primary);
      }

      .dialog-message {
        margin: 0 0 24px 0;
        font-size: var(--font-size-body);
        color: var(--color-text-secondary);
        line-height: var(--line-height-body);
      }

      .dialog-actions {
        display: flex;
        justify-content: flex-end;
        gap: 10px;
      }
    `,
  ],
})
export class ConfirmDialogComponent {
  readonly dialogRef = inject(DialogRef<boolean>);
  readonly data = inject<ConfirmDialogData>(DIALOG_DATA);
}
