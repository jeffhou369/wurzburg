import { Component, inject, signal } from '@angular/core';
import { Dialog } from '@angular/cdk/dialog';
import { LucidePlus, LucideRefreshCw, LucideTrash2, LucideCopy } from '@lucide/angular';
import { StatusPillComponent } from '../../shared/components/status-pill.component';
import { EmptyStateComponent } from '../../shared/components/empty-state.component';
import { ToastService } from '../../shared/components/toast.service';
import { IpRecordSheetComponent } from '../../shared/components/ip-record-sheet.component';
import { ConfirmDialogComponent } from '../../shared/components/confirm-dialog.component';
import { AuthService } from '../../core/auth.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { STATUS_TEXT } from '../../core/i18n/status.i18n';
import { SHOWCASE_TEXT } from './showcase.i18n';

@Component({
  selector: 'app-showcase-page',
  standalone: true,
  imports: [
    StatusPillComponent,
    EmptyStateComponent,
    IpRecordSheetComponent,
    LucidePlus,
    LucideRefreshCw,
    LucideTrash2,
    LucideCopy,
  ],
  template: `
    <header class="page-header">
      <h1 class="page-title">{{ text.title }}</h1>
      <p class="page-subtitle">
        {{ text.subtitle }}
      </p>
    </header>

    <div class="showcase-grid">
      <section class="card">
        <h2 class="section-title">{{ text.buttons.title }}</h2>
        <div class="row-wrap">
          <button type="button" class="btn btn-primary">
            <svg lucidePlus [size]="16" [strokeWidth]="2"></svg>
            {{ text.buttons.primary }}
          </button>
          <button type="button" class="btn btn-secondary">
            <svg lucideRefreshCw [size]="16" [strokeWidth]="2"></svg>
            {{ text.buttons.secondary }}
          </button>
          <button type="button" class="btn btn-ghost">{{ text.buttons.ghost }}</button>
          <button type="button" class="btn btn-danger-soft">{{ text.buttons.dangerSoft }}</button>
          <button type="button" class="btn btn-danger-solid" (click)="openDeleteDialog()">
            <svg lucideTrash2 [size]="16" [strokeWidth]="2"></svg>
            {{ text.buttons.dangerSolid }}
          </button>
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">{{ text.pills.title }}</h2>
        <div class="row-wrap">
          <app-status-pill variant="online" [label]="status.online" />
          <app-status-pill variant="offline" [label]="status.offline" />
          <app-status-pill variant="unregistered" [label]="status.unregistered" />
          <app-status-pill variant="registered" [label]="status.registered" />
          <app-status-pill variant="expiring" [label]="status.expiring" />
          <app-status-pill variant="expired" [label]="status.expired" />
          <app-status-pill variant="conflict" [label]="status.conflict" />
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">{{ text.interactions.title }}</h2>
        <div class="row-wrap">
          <button type="button" class="btn btn-secondary" (click)="sheetOpen.set(true)">
            {{ text.interactions.openSheet }}
          </button>
          <button type="button" class="btn btn-secondary" (click)="showSuccessToast()">
            {{ text.interactions.successToast }}
          </button>
          <button type="button" class="btn btn-secondary" (click)="showConflictToast()">
            {{ text.interactions.conflictToast }}
          </button>
          <button type="button" class="btn btn-secondary" (click)="toggleRole()">
            {{
              text.interactions.toggleRole(
                auth.isAdmin() ? shell.user.adminBadge : shell.user.userBadge
              )
            }}
          </button>
          <button type="button" class="btn btn-secondary" (click)="toggleBadges()">
            {{ text.interactions.toggleBadges }}
          </button>
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">{{ text.table.title }}</h2>
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ text.table.columns.status }}</th>
                <th>{{ text.table.columns.ipAddress }}</th>
                <th>{{ text.table.columns.macAddress }}</th>
                <th>{{ text.table.columns.subnet }}</th>
                <th>{{ text.table.columns.owner }}</th>
                <th>{{ text.table.columns.actions }}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>
                  <div class="status-combo">
                    <app-status-pill variant="online" [label]="status.online" />
                    <app-status-pill variant="unregistered" [label]="status.unregistered" />
                  </div>
                </td>
                <td class="font-mono">10.10.10.50</td>
                <td class="font-mono">b8:27:eb:11:22:33</td>
                <td class="font-mono">10.10.10.0/24</td>
                <td>—</td>
                <td>
                  <button
                    type="button"
                    class="btn btn-ghost btn-sm"
                    [attr.aria-label]="text.table.copyIp"
                  >
                    <svg lucideCopy [size]="14" [strokeWidth]="2"></svg>
                  </button>
                </td>
              </tr>
              <tr class="row-offline">
                <td>
                  <div class="status-combo">
                    <app-status-pill variant="offline" [label]="status.offline" />
                    <app-status-pill variant="expired" [label]="status.expired" />
                  </div>
                </td>
                <td class="font-mono">10.10.10.88</td>
                <td class="font-mono">00:50:56:89:ab:cd</td>
                <td class="font-mono">10.10.10.0/24</td>
                <td>chen@example.com</td>
                <td>
                  <button
                    type="button"
                    class="btn btn-ghost btn-sm"
                    [attr.aria-label]="text.table.copyIp"
                  >
                    <svg lucideCopy [size]="14" [strokeWidth]="2"></svg>
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card">
        <h2 class="section-title">{{ text.empty.title }}</h2>
        <app-empty-state
          [title]="text.empty.emptyTitle"
          [description]="text.empty.emptyDescription"
        />
      </section>
    </div>

    <app-ip-record-sheet
      [open]="sheetOpen()"
      ipAddress="10.10.10.50"
      (closed)="sheetOpen.set(false)"
    >
      <div slot="registration">
        <p>{{ text.sheet.registration }}</p>
      </div>
      <div slot="observations">
        <p>{{ text.sheet.observations }}</p>
      </div>
    </app-ip-record-sheet>
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
      .showcase-grid {
        display: flex;
        flex-direction: column;
        gap: 20px;
      }
      .section-title {
        margin: 0 0 16px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
      }
      .row-wrap {
        display: flex;
        flex-wrap: wrap;
        gap: 10px;
        align-items: center;
      }
      .table-wrap {
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
        overflow-x: auto;
      }
      .data-table {
        width: 100%;
        border-collapse: collapse;
        text-align: left;
        font-size: var(--font-size-meta);

        th {
          background-color: var(--color-surface-subtle);
          color: var(--color-text-secondary);
          font-size: var(--font-size-caption);
          font-weight: 500;
          padding: 10px 14px;
          border-bottom: 1px solid var(--color-border-default);
        }

        td {
          padding: 12px 14px;
          border-bottom: 1px solid var(--color-border-subtle);
          vertical-align: middle;
        }

        tbody tr:last-child td {
          border-bottom: none;
        }

        tbody tr:hover {
          background-color: var(--color-surface-hover);
        }
      }
      .status-combo {
        display: flex;
        flex-wrap: wrap;
        gap: 6px;
      }
    `,
  ],
})
export class ShowcasePage {
  private readonly dialog = inject(Dialog);
  protected readonly toast = inject(ToastService);
  protected readonly auth = inject(AuthService);
  protected readonly badges = inject(SidebarBadgesService);
  protected readonly sheetOpen = signal(false);
  protected readonly text = SHOWCASE_TEXT;
  protected readonly status = STATUS_TEXT;
  protected readonly shell = SHELL_TEXT;

  openDeleteDialog(): void {
    const ref = this.dialog.open<boolean>(ConfirmDialogComponent, {
      data: {
        title: this.text.deleteDialog.title,
        message: this.text.deleteDialog.message,
        confirmLabel: this.text.deleteDialog.confirm,
        danger: true,
      },
    });

    ref.closed.subscribe((confirmed) => {
      if (confirmed) {
        this.toast.show(
          this.text.deleteDialog.toastTitle,
          'warning',
          this.text.deleteDialog.toastDetail,
        );
      }
    });
  }

  showSuccessToast(): void {
    this.toast.show(
      this.text.interactions.successToastTitle,
      'success',
      this.text.interactions.successToastDetail,
    );
  }

  showConflictToast(): void {
    this.toast.show(
      this.text.interactions.conflictToastTitle,
      'danger',
      this.text.interactions.conflictToastDetail,
    );
  }

  toggleRole(): void {
    this.auth.setRole(this.auth.isAdmin() ? 'user' : 'admin');
  }

  toggleBadges(): void {
    if (this.badges.pendingSubnetsCount() === 0) {
      this.badges.pendingSubnetsCount.set(3);
      this.badges.openConflictsCount.set(2);
    } else {
      this.badges.pendingSubnetsCount.set(0);
      this.badges.openConflictsCount.set(0);
    }
  }
}
