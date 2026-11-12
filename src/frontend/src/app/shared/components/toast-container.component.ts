import { Component, inject } from '@angular/core';
import { LucideX, LucideCircleCheck, LucideTriangleAlert, LucideInfo } from '@lucide/angular';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { ToastService } from './toast.service';

@Component({
  selector: 'app-toast-container',
  standalone: true,
  imports: [LucideX, LucideCircleCheck, LucideTriangleAlert, LucideInfo],
  template: `
    <div class="toast-region" aria-live="polite" aria-atomic="true">
      @for (toast of toastService.toasts(); track toast.id) {
        <div class="toast-card" [class]="'toast-card toast-' + toast.variant" role="status">
          <div class="toast-icon" aria-hidden="true">
            @switch (toast.variant) {
              @case ('success') {
                <svg lucideCircleCheck [size]="18" [strokeWidth]="2"></svg>
              }
              @case ('warning') {
                <svg lucideTriangleAlert [size]="18" [strokeWidth]="2"></svg>
              }
              @case ('danger') {
                <svg lucideTriangleAlert [size]="18" [strokeWidth]="2"></svg>
              }
              @default {
                <svg lucideInfo [size]="18" [strokeWidth]="2"></svg>
              }
            }
          </div>
          <div class="toast-body">
            <div class="toast-title">{{ toast.title }}</div>
            @if (toast.detail) {
              <div class="toast-detail">{{ toast.detail }}</div>
            }
          </div>
          <button
            type="button"
            class="toast-close"
            (click)="toastService.dismiss(toast.id)"
            [attr.aria-label]="text.closeToast"
          >
            <svg lucideX [size]="16" [strokeWidth]="2"></svg>
          </button>
        </div>
      }
    </div>
  `,
  styles: [
    `
      .toast-region {
        position: fixed;
        top: 16px;
        right: 16px;
        z-index: 1000;
        display: flex;
        flex-direction: column;
        gap: 8px;
        max-width: 380px;
        width: calc(100vw - 32px);
        pointer-events: none;
      }

      .toast-card {
        pointer-events: auto;
        display: flex;
        align-items: flex-start;
        gap: 10px;
        padding: 12px 14px;
        border-radius: var(--radius-lg);
        background-color: var(--color-surface);
        border: 1px solid var(--color-border-default);
        box-shadow: var(--shadow-lg);
      }

      .toast-icon {
        flex-shrink: 0;
        margin-top: 2px;
      }

      .toast-success .toast-icon {
        color: var(--color-success);
      }

      .toast-warning .toast-icon {
        color: var(--color-warning);
      }

      .toast-danger .toast-icon {
        color: var(--color-danger);
      }

      .toast-info .toast-icon {
        color: var(--color-info);
      }

      .toast-body {
        flex: 1;
        min-width: 0;
      }

      .toast-title {
        font-size: var(--font-size-body);
        font-weight: 600;
        color: var(--color-text-primary);
      }

      .toast-detail {
        margin-top: 2px;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }

      .toast-close {
        background: transparent;
        border: none;
        padding: 4px;
        border-radius: var(--radius-sm);
        color: var(--color-text-muted);
        cursor: pointer;

        &:hover {
          background-color: var(--color-surface-hover);
          color: var(--color-text-primary);
        }

        &:focus-visible {
          outline: 2px solid var(--color-border-focus);
        }
      }
    `,
  ],
})
export class ToastContainerComponent {
  protected readonly toastService = inject(ToastService);
  protected readonly text = SHELL_TEXT.aria;
}
