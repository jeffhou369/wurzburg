import { Component, input, output, HostListener } from '@angular/core';
import { A11yModule } from '@angular/cdk/a11y';
import { LucideX } from '@lucide/angular';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';

@Component({
  selector: 'app-sheet-container',
  standalone: true,
  imports: [A11yModule, LucideX],
  template: `
    @if (open()) {
      <div class="sheet-backdrop" (click)="closed.emit()">
        <aside
          class="sheet-panel"
          role="dialog"
          aria-modal="true"
          [attr.aria-label]="title()"
          cdkTrapFocus
          cdkTrapFocusAutoCapture
          (click)="$event.stopPropagation()"
        >
          <header class="sheet-header">
            <h2 class="sheet-title">{{ title() }}</h2>
            <button
              type="button"
              class="btn btn-ghost btn-sm"
              (click)="closed.emit()"
              [attr.aria-label]="text.closeSheet"
            >
              <svg lucideX [size]="18" [strokeWidth]="2"></svg>
            </button>
          </header>
          <div class="sheet-body">
            <ng-content />
          </div>
        </aside>
      </div>
    }
  `,
  styles: [
    `
      .sheet-backdrop {
        position: fixed;
        inset: 0;
        background-color: var(--color-overlay-backdrop);
        z-index: 900;
        display: flex;
        justify-content: flex-end;
      }

      .sheet-panel {
        width: 100%;
        max-width: 520px;
        height: 100%;
        background-color: var(--color-surface);
        border-left: 1px solid var(--color-border-default);
        box-shadow: var(--shadow-dialog);
        display: flex;
        flex-direction: column;
        animation: slideInRight var(--motion-enter) var(--ease-out);
      }

      @keyframes slideInRight {
        from {
          transform: translateX(100%);
        }
        to {
          transform: translateX(0);
        }
      }

      .sheet-header {
        display: flex;
        align-items: center;
        justify-content: space-between;
        padding: 20px 24px;
        border-bottom: 1px solid var(--color-border-subtle);
      }

      .sheet-title {
        margin: 0;
        font-size: var(--font-size-panel-title);
        font-weight: 600;
        color: var(--color-text-primary);
      }

      .sheet-body {
        flex: 1;
        overflow-y: auto;
        padding: 24px;
      }
    `,
  ],
})
export class SheetContainerComponent {
  protected readonly text = SHELL_TEXT.aria;
  readonly open = input<boolean>(false);
  readonly title = input.required<string>();
  readonly closed = output<void>();

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.open()) {
      this.closed.emit();
    }
  }
}
