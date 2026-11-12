import { Component, input, output, signal } from '@angular/core';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { SheetContainerComponent } from './sheet-container.component';

export type IpRecordSheetTab = 'registration' | 'observations';

/**
 * IP 紀錄右側抽屜（預留插槽）
 *
 * 供後續功能工作項目各自填入而不衝突：
 * - [slot="registration"]：第 18 張（登記）填入
 * - [slot="observations"]：第 13 張（IP 紀錄的整併：觀測與歷程）填入
 */
@Component({
  selector: 'app-ip-record-sheet',
  standalone: true,
  imports: [SheetContainerComponent],
  template: `
    <app-sheet-container
      [open]="open()"
      [title]="ipAddress() ? ipAddress()! : text.fallbackTitle"
      (closed)="closed.emit()"
    >
      <div class="sheet-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          class="tab-btn"
          [class.active]="activeTab() === 'registration'"
          [attr.aria-selected]="activeTab() === 'registration'"
          (click)="activeTab.set('registration')"
        >
          {{ text.tabRegistration }}
        </button>
        <button
          type="button"
          role="tab"
          class="tab-btn"
          [class.active]="activeTab() === 'observations'"
          [attr.aria-selected]="activeTab() === 'observations'"
          (click)="activeTab.set('observations')"
        >
          {{ text.tabObservations }}
        </button>
      </div>

      <div class="tab-panel" role="tabpanel">
        @if (activeTab() === 'registration') {
          <ng-content select="[slot=registration]">
            <p class="slot-placeholder">{{ text.registrationPlaceholder }}</p>
          </ng-content>
        } @else {
          <ng-content select="[slot=observations]">
            <p class="slot-placeholder">{{ text.observationsPlaceholder }}</p>
          </ng-content>
        }
      </div>
    </app-sheet-container>
  `,
  styles: [
    `
      .sheet-tabs {
        display: flex;
        gap: 4px;
        border-bottom: 1px solid var(--color-border-default);
        margin-bottom: 20px;
      }

      .tab-btn {
        background: transparent;
        border: none;
        border-bottom: 2px solid transparent;
        padding: 8px 14px;
        font-size: var(--font-size-body);
        font-weight: 500;
        color: var(--color-text-muted);
        cursor: pointer;

        &.active {
          color: var(--color-text-primary);
          border-bottom-color: var(--color-accent);
        }

        &:hover:not(.active) {
          color: var(--color-text-primary);
        }
      }

      .slot-placeholder {
        color: var(--color-text-muted);
        font-size: var(--font-size-meta);
      }
    `,
  ],
})
export class IpRecordSheetComponent {
  protected readonly text = SHELL_TEXT.ipRecordSheet;
  readonly open = input<boolean>(false);
  readonly ipAddress = input<string | null>(null);
  readonly closed = output<void>();

  readonly activeTab = signal<IpRecordSheetTab>('registration');
}
