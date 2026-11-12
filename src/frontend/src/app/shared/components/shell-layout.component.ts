import { Component, OnInit, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import {
  LucideLayoutDashboard,
  LucideNetwork,
  LucideServer,
  LucideTriangleAlert,
  LucideMail,
  LucideHistory,
  LucideSettings,
  LucidePalette,
  LucideSun,
  LucideMoon,
  LucideMenu,
  LucideX,
} from '@lucide/angular';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { ThemeService } from '../../core/theme.service';
import { AuthService } from '../../core/auth.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import { ToastContainerComponent } from './toast-container.component';

@Component({
  selector: 'app-shell-layout',
  standalone: true,
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    ToastContainerComponent,
    LucideLayoutDashboard,
    LucideNetwork,
    LucideServer,
    LucideTriangleAlert,
    LucideMail,
    LucideHistory,
    LucideSettings,
    LucidePalette,
    LucideSun,
    LucideMoon,
    LucideMenu,
    LucideX,
  ],
  template: `
    <div class="app-shell">
      <button
        type="button"
        class="mobile-menu-toggle btn btn-secondary btn-sm"
        (click)="mobileSidebarOpen.set(!mobileSidebarOpen())"
        [attr.aria-expanded]="mobileSidebarOpen()"
        [attr.aria-label]="text.aria.toggleSidebar"
      >
        @if (mobileSidebarOpen()) {
          <svg lucideX [size]="18" [strokeWidth]="2"></svg>
        } @else {
          <svg lucideMenu [size]="18" [strokeWidth]="2"></svg>
        }
        <span>{{ text.appName }}</span>
      </button>

      @if (mobileSidebarOpen()) {
        <div class="mobile-backdrop" (click)="mobileSidebarOpen.set(false)"></div>
      }

      <aside class="sidebar" [class.mobile-open]="mobileSidebarOpen()">
        <div class="sidebar-header">
          <div class="brand-mark" aria-hidden="true">W</div>
          <div class="brand-text">
            <div class="brand-name">{{ campusName() }}</div>
            <div class="brand-sub">{{ text.appSubtitle }}</div>
          </div>
        </div>

        <nav class="sidebar-nav" [attr.aria-label]="text.aria.mainNav">
          <a
            class="nav-item"
            routerLink="/"
            routerLinkActive="active"
            [routerLinkActiveOptions]="{ exact: true }"
            (click)="mobileSidebarOpen.set(false)"
          >
            <svg lucideLayoutDashboard [size]="18" [strokeWidth]="2"></svg>
            <span class="nav-label">{{ text.nav.workbench }}</span>
          </a>

          <a
            class="nav-item"
            routerLink="/subnets"
            routerLinkActive="active"
            (click)="mobileSidebarOpen.set(false)"
          >
            <svg lucideNetwork [size]="18" [strokeWidth]="2"></svg>
            <span class="nav-label">{{ text.nav.subnets }}</span>
            @if (badges.pendingSubnetsCount() > 0) {
              <span
                class="nav-badge nav-badge-warning"
                [attr.aria-label]="text.aria.pendingSubnetsCount"
              >
                {{ badges.pendingSubnetsCount() }}
              </span>
            }
          </a>

          @if (auth.isAdmin()) {
            <a
              class="nav-item"
              routerLink="/switches"
              routerLinkActive="active"
              (click)="mobileSidebarOpen.set(false)"
            >
              <svg lucideServer [size]="18" [strokeWidth]="2"></svg>
              <span class="nav-label">{{ text.nav.switches }}</span>
            </a>
          }

          <a
            class="nav-item"
            routerLink="/conflicts"
            routerLinkActive="active"
            (click)="mobileSidebarOpen.set(false)"
          >
            <svg lucideTriangleAlert [size]="18" [strokeWidth]="2"></svg>
            <span class="nav-label">{{ text.nav.conflicts }}</span>
            @if (badges.openConflictsCount() > 0) {
              <span
                class="nav-badge nav-badge-danger"
                [attr.aria-label]="text.aria.openConflictsCount"
              >
                {{ badges.openConflictsCount() }}
              </span>
            }
          </a>

          @if (auth.isAdmin()) {
            <a
              class="nav-item"
              routerLink="/notifications"
              routerLinkActive="active"
              (click)="mobileSidebarOpen.set(false)"
            >
              <svg lucideMail [size]="18" [strokeWidth]="2"></svg>
              <span class="nav-label">{{ text.nav.notifications }}</span>
            </a>

            <a
              class="nav-item"
              routerLink="/audit"
              routerLinkActive="active"
              (click)="mobileSidebarOpen.set(false)"
            >
              <svg lucideHistory [size]="18" [strokeWidth]="2"></svg>
              <span class="nav-label">{{ text.nav.audit }}</span>
            </a>

            <a
              class="nav-item"
              routerLink="/settings"
              routerLinkActive="active"
              (click)="mobileSidebarOpen.set(false)"
            >
              <svg lucideSettings [size]="18" [strokeWidth]="2"></svg>
              <span class="nav-label">{{ text.nav.settings }}</span>
            </a>
          }

          <div class="nav-divider" aria-hidden="true"></div>

          <a
            class="nav-item"
            routerLink="/showcase"
            routerLinkActive="active"
            (click)="mobileSidebarOpen.set(false)"
          >
            <svg lucidePalette [size]="18" [strokeWidth]="2"></svg>
            <span class="nav-label">{{ text.nav.showcase }}</span>
          </a>
        </nav>

        <div class="sidebar-footer">
          <div class="user-card">
            <div class="user-name">{{ auth.currentUser()?.displayName }}</div>
            <div class="user-role">
              {{ auth.isAdmin() ? text.user.adminBadge : text.user.userBadge }}
            </div>
          </div>

          <button
            type="button"
            class="btn btn-ghost btn-sm theme-btn"
            (click)="themeService.toggleTheme()"
            [attr.aria-label]="text.theme.toggleLabel"
          >
            @if (themeService.currentTheme() === 'light') {
              <svg lucideMoon [size]="16" [strokeWidth]="2"></svg>
              <span>{{ text.theme.dark }}</span>
            } @else {
              <svg lucideSun [size]="16" [strokeWidth]="2"></svg>
              <span>{{ text.theme.light }}</span>
            }
          </button>
        </div>
      </aside>

      <main class="workspace">
        <div class="workspace-inner">
          <router-outlet />
        </div>
      </main>
    </div>

    <app-toast-container />
  `,
  styles: [
    `
      .app-shell {
        display: grid;
        grid-template-columns: var(--sidebar-width) minmax(0, 1fr);
        min-height: 100vh;
        background-color: var(--color-workspace);
      }

      .mobile-menu-toggle {
        display: none;
        position: fixed;
        top: 12px;
        left: 16px;
        z-index: 600;
      }

      .mobile-backdrop {
        display: none;
      }

      .sidebar {
        background-color: var(--sidebar-bg);
        border-right: 1px solid var(--color-border-default);
        display: flex;
        flex-direction: column;
        padding: 20px 14px;
        height: 100vh;
        position: sticky;
        top: 0;
        overflow-y: auto;
      }

      .sidebar-header {
        display: flex;
        align-items: center;
        gap: 12px;
        padding: 4px 10px 20px 10px;
      }

      .brand-mark {
        width: 34px;
        height: 34px;
        border-radius: var(--radius-md);
        background-color: var(--color-accent);
        color: var(--color-text-on-accent);
        display: flex;
        align-items: center;
        justify-content: center;
        font-weight: 700;
        font-size: 16px;
        flex-shrink: 0;
      }

      .brand-name {
        font-size: var(--font-size-section);
        font-weight: 600;
        color: var(--color-text-primary);
        line-height: 1.25;
      }

      .brand-sub {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }

      .sidebar-nav {
        display: flex;
        flex-direction: column;
        gap: 4px;
        flex: 1;
      }

      .nav-item {
        display: flex;
        align-items: center;
        gap: 10px;
        padding: 9px 12px;
        border-radius: var(--radius-md);
        color: var(--color-text-secondary);
        text-decoration: none;
        font-size: var(--font-size-body);
        font-weight: 500;
        transition:
          background-color var(--motion-fast) var(--ease-out),
          color var(--motion-fast) var(--ease-out);

        &:hover {
          background-color: var(--color-surface-hover);
          color: var(--color-text-primary);
        }

        &.active {
          background-color: var(--color-surface);
          color: var(--color-text-primary);
          box-shadow: var(--shadow-xs);
          font-weight: 600;
        }

        &:focus-visible {
          outline: 2px solid var(--color-border-focus);
          outline-offset: -2px;
        }
      }

      .nav-label {
        flex: 1;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .nav-badge {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        min-width: 20px;
        height: 20px;
        padding: 0 6px;
        border-radius: var(--radius-full);
        font-size: 11px;
        font-weight: 600;
        font-variant-numeric: tabular-nums;

        &.nav-badge-warning {
          background-color: var(--color-warning-subtle);
          color: var(--color-warning-text);
          border: 1px solid var(--color-warning-border);
        }

        &.nav-badge-danger {
          background-color: var(--color-danger-subtle);
          color: var(--color-danger-text);
          border: 1px solid var(--color-danger-border);
        }
      }

      .nav-divider {
        height: 1px;
        background-color: var(--color-border-default);
        margin: 8px 10px;
      }

      .sidebar-footer {
        border-top: 1px solid var(--color-border-default);
        padding-top: 14px;
        display: flex;
        flex-direction: column;
        gap: 8px;
      }

      .user-card {
        padding: 4px 10px;
      }

      .user-name {
        font-size: var(--font-size-meta);
        font-weight: 600;
        color: var(--color-text-primary);
      }

      .user-role {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }

      .theme-btn {
        justify-content: flex-start;
        width: 100%;
      }

      .workspace {
        background-color: var(--color-workspace);
        min-width: 0;
        padding: 28px 32px;
      }

      .workspace-inner {
        max-width: 1360px;
        margin: 0 auto;
      }

      @media (max-width: 900px) {
        .app-shell {
          grid-template-columns: 1fr;
        }

        .mobile-menu-toggle {
          display: inline-flex;
        }

        .mobile-backdrop {
          display: block;
          position: fixed;
          inset: 0;
          background-color: var(--scrim);
          z-index: 650;
        }

        .sidebar {
          position: fixed;
          left: 0;
          top: 0;
          bottom: 0;
          width: var(--sidebar-width);
          z-index: 700;
          transform: translateX(-100%);
          transition: transform var(--motion-enter) var(--ease-out);

          &.mobile-open {
            transform: translateX(0);
          }
        }

        .workspace {
          padding: 64px 16px 24px 16px;
        }
      }
    `,
  ],
})
export class ShellLayoutComponent implements OnInit {
  private readonly http = inject(HttpClient);
  protected readonly text = SHELL_TEXT;
  protected readonly themeService = inject(ThemeService);
  protected readonly auth = inject(AuthService);
  protected readonly badges = inject(SidebarBadgesService);
  protected readonly mobileSidebarOpen = signal(false);
  protected readonly campusName = signal<string>(SHELL_TEXT.appName);

  ngOnInit(): void {
    this.http.get<{ campus_name: string }>('/api/v1/settings/campus').subscribe({
      next: (res) => {
        if (res.campus_name) {
          this.campusName.set(res.campus_name);
          document.title = this.text.documentTitle(res.campus_name);
        }
      },
      error: () => {},
    });
  }
}
