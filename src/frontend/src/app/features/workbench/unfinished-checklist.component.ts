import { Component, OnInit, inject, signal, output } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Router } from '@angular/router';
import { LucideTriangleAlert, LucideWand2, LucideChevronRight } from '@lucide/angular';
import { AuthService } from '../../core/auth.service';
import type { components } from '../../core/api.types';
import { CHECKLIST_TEXT } from './unfinished-checklist.i18n';

type UnfinishedItem = components['schemas']['UnfinishedSetupItem'];
type WizardStatus = components['schemas']['WizardStatus'];

@Component({
  selector: 'app-unfinished-checklist',
  standalone: true,
  imports: [LucideTriangleAlert, LucideWand2, LucideChevronRight],
  template: `
    @if (auth.isAdmin() && items().length > 0) {
      <div class="checklist-card card" role="alert">
        <div class="card-top">
          <div class="title-group">
            <svg
              lucideTriangleAlert
              [size]="20"
              class="warn-icon"
              aria-hidden="true"
              [strokeWidth]="2"
            ></svg>
            <div>
              <h2 class="checklist-title">
                {{ text.title(items().length) }}
              </h2>
              <p class="checklist-sub">
                {{ text.description }}
              </p>
            </div>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" (click)="resumeWizard.emit()">
            <svg lucideWand2 [size]="14"></svg>
            <span>{{ text.openWizard }}</span>
          </button>
        </div>

        <ul class="items-list" [attr.aria-label]="text.listLabel">
          @for (it of items(); track it.key) {
            <li class="item-row" (click)="navigate(it.route)">
              <div class="item-info">
                <span class="item-title">{{ it.title }}</span>
                <span class="item-consequence">{{ text.consequence(it.consequence) }}</span>
              </div>
              <button
                type="button"
                class="btn btn-ghost btn-sm nav-link-btn"
                [attr.aria-label]="text.goToSettingLabel(it.title)"
              >
                <span>{{ text.goToSetting }}</span>
                <svg lucideChevronRight [size]="14"></svg>
              </button>
            </li>
          }
        </ul>
      </div>
    }
  `,
  styles: [
    `
      .checklist-card {
        background-color: var(--color-warning-subtle);
        border: 1px solid var(--color-warning-border);
        margin-bottom: 24px;
        padding: 16px 20px;
      }
      .card-top {
        display: flex;
        justify-content: space-between;
        align-items: center;
        gap: 16px;
        margin-bottom: 12px;
        flex-wrap: wrap;
      }
      .title-group {
        display: flex;
        align-items: center;
        gap: 12px;
      }
      .warn-icon {
        color: var(--color-warning);
        flex-shrink: 0;
      }
      .checklist-title {
        margin: 0 0 2px 0;
        font-size: var(--font-size-body);
        font-weight: 600;
        color: var(--color-warning-text);
      }
      .checklist-sub {
        margin: 0;
        font-size: var(--font-size-caption);
        color: var(--color-text-secondary);
      }
      .items-list {
        list-style: none;
        margin: 0;
        padding: 0;
        display: flex;
        flex-direction: column;
        gap: 6px;
      }
      .item-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 8px 12px;
        background-color: var(--color-surface);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
        cursor: pointer;
        transition: background-color var(--motion-fast) var(--ease-out);
        &:hover {
          background-color: var(--color-surface-hover);
        }
      }
      .item-info {
        display: flex;
        align-items: center;
        gap: 12px;
        flex-wrap: wrap;
      }
      .item-title {
        font-weight: 600;
        font-size: var(--font-size-meta);
        color: var(--color-text-primary);
      }
      .item-consequence {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .nav-link-btn {
        display: inline-flex;
        align-items: center;
        gap: 4px;
        font-size: var(--font-size-caption);
      }
    `,
  ],
})
export class UnfinishedChecklistComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly router = inject(Router);
  protected readonly auth = inject(AuthService);
  protected readonly text = CHECKLIST_TEXT;

  readonly resumeWizard = output<void>();

  protected readonly items = signal<UnfinishedItem[]>([]);

  ngOnInit(): void {
    this.refresh();
  }

  refresh(): void {
    if (!this.auth.isAdmin()) {
      this.items.set([]);
      return;
    }
    this.http.get<WizardStatus>('/api/v1/settings/wizard').subscribe({
      next: (res) => {
        this.items.set(res.unfinished_items ?? []);
      },
      error: () => {
        this.items.set([]);
      },
    });
  }

  navigate(route: string): void {
    void this.router.navigateByUrl(route);
  }
}
