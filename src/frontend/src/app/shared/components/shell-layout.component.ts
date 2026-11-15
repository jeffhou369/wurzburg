import {
  Component,
  ElementRef,
  HostListener,
  OnInit,
  computed,
  inject,
  signal,
} from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { CdkTrapFocus } from '@angular/cdk/a11y';
import { CdkScrollable } from '@angular/cdk/scrolling';
import {
  LucideLayoutDashboard,
  LucideNetwork,
  LucideServer,
  LucideTriangleAlert,
  LucideMail,
  LucideHistory,
  LucideSettings,
  LucideSun,
  LucideMoon,
  LucideMenu,
  LucideX,
  LucideLogOut,
  LucidePanelLeftClose,
  LucidePanelLeftOpen,
} from '@lucide/angular';
import { SHELL_TEXT } from '../../core/i18n/shell.i18n';
import { ThemeService } from '../../core/theme.service';
import { AuthService } from '../../core/auth.service';
import { SidebarBadgesService } from '../../core/sidebar-badges.service';
import { ToastContainerComponent } from './toast-container.component';
import { SidebarHintDirective } from './sidebar-hint.directive';

const SIDEBAR_PREFERENCE_KEY = 'wurzburg_sidebar_collapsed';

function readSidebarPreference(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_PREFERENCE_KEY) === 'true';
  } catch {
    return false;
  }
}

@Component({
  selector: 'app-shell-layout',
  standalone: true,
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    ToastContainerComponent,
    CdkTrapFocus,
    CdkScrollable,
    SidebarHintDirective,
    LucideLayoutDashboard,
    LucideNetwork,
    LucideServer,
    LucideTriangleAlert,
    LucideMail,
    LucideHistory,
    LucideSettings,
    LucideSun,
    LucideMoon,
    LucideMenu,
    LucideX,
    LucideLogOut,
    LucidePanelLeftClose,
    LucidePanelLeftOpen,
  ],
  template: `
    <div class="app-shell" [class.sidebar-collapsed]="compactSidebar()">
      <button
        type="button"
        class="mobile-menu-toggle btn btn-secondary"
        (click)="mobileSidebarOpen.set(true)"
        [attr.aria-expanded]="mobileSidebarOpen()"
        aria-controls="main-sidebar"
        [attr.aria-label]="text.aria.toggleSidebar"
      >
        <svg lucideMenu [size]="20" [strokeWidth]="2" aria-hidden="true"></svg>
      </button>

      @if (isMobile() && mobileSidebarOpen()) {
        <button
          type="button"
          class="mobile-backdrop"
          tabindex="-1"
          [attr.aria-label]="text.aria.closeSidebar"
          (click)="mobileSidebarOpen.set(false)"
        ></button>
      }

      @if (!isMobile() || mobileSidebarOpen()) {
        <aside
          id="main-sidebar"
          class="sidebar"
          [class.mobile-open]="isMobile() && mobileSidebarOpen()"
          [attr.role]="isMobile() ? 'dialog' : null"
          [attr.aria-modal]="isMobile() ? 'true' : null"
          [attr.aria-label]="text.aria.mainNav"
          [cdkTrapFocus]="isMobile()"
          [cdkTrapFocusAutoCapture]="isMobile()"
          cdkScrollable
        >
          <div class="sidebar-header">
            <div class="brand-mark" aria-hidden="true">W</div>
            <div class="brand-text">
              <div class="brand-name">{{ campusName() }}</div>
              <div class="brand-sub">{{ text.appSubtitle }}</div>
            </div>
            <button
              type="button"
              class="desktop-sidebar-toggle btn btn-ghost"
              (click)="toggleSidebar()"
              [attr.aria-expanded]="!sidebarCollapsed()"
              aria-controls="sidebar-navigation"
              [attr.aria-label]="
                sidebarCollapsed() ? text.aria.expandSidebar : text.aria.collapseSidebar
              "
              [appSidebarHint]="
                sidebarCollapsed() ? text.aria.expandSidebar : text.aria.collapseSidebar
              "
            >
              @if (sidebarCollapsed()) {
                <svg lucidePanelLeftOpen [size]="20" aria-hidden="true"></svg>
              } @else {
                <svg lucidePanelLeftClose [size]="20" aria-hidden="true"></svg>
              }
            </button>
            <button
              type="button"
              class="mobile-sidebar-close btn btn-ghost"
              (click)="mobileSidebarOpen.set(false)"
              [attr.aria-label]="text.aria.closeSidebar"
            >
              <svg lucideX [size]="20" aria-hidden="true"></svg>
            </button>
          </div>

          <nav id="sidebar-navigation" class="sidebar-nav" [attr.aria-label]="text.aria.mainNav">
            <a
              class="nav-item"
              routerLink="/"
              routerLinkActive="active"
              [routerLinkActiveOptions]="{ exact: true }"
              ariaCurrentWhenActive="page"
              [attr.aria-label]="text.nav.workbench"
              [appSidebarHint]="compactSidebar() ? text.nav.workbench : ''"
              (click)="mobileSidebarOpen.set(false)"
            >
              <svg lucideLayoutDashboard [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
              <span class="nav-label">{{ text.nav.workbench }}</span>
            </a>

            <a
              class="nav-item"
              routerLink="/subnets"
              routerLinkActive="active"
              ariaCurrentWhenActive="page"
              [attr.aria-label]="text.aria.subnetsLabel(badges.pendingSubnetsCount())"
              [appSidebarHint]="
                compactSidebar() ? text.aria.subnetsLabel(badges.pendingSubnetsCount()) : ''
              "
              (click)="mobileSidebarOpen.set(false)"
            >
              <svg lucideNetwork [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
              <span class="nav-label">{{ text.nav.subnets }}</span>
              @if (badges.pendingSubnetsCount() > 0) {
                <span class="nav-badge nav-badge-warning" aria-hidden="true">{{
                  badgeCount(badges.pendingSubnetsCount())
                }}</span>
              }
            </a>

            @if (auth.isAdmin()) {
              <a
                class="nav-item"
                routerLink="/switches"
                routerLinkActive="active"
                ariaCurrentWhenActive="page"
                [attr.aria-label]="text.nav.switches"
                [appSidebarHint]="compactSidebar() ? text.nav.switches : ''"
                (click)="mobileSidebarOpen.set(false)"
              >
                <svg lucideServer [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
                <span class="nav-label">{{ text.nav.switches }}</span>
              </a>
            }

            <a
              class="nav-item"
              routerLink="/conflicts"
              routerLinkActive="active"
              ariaCurrentWhenActive="page"
              [attr.aria-label]="text.aria.conflictsLabel(badges.openConflictsCount())"
              [appSidebarHint]="
                compactSidebar() ? text.aria.conflictsLabel(badges.openConflictsCount()) : ''
              "
              (click)="mobileSidebarOpen.set(false)"
            >
              <svg lucideTriangleAlert [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
              <span class="nav-label">{{ text.nav.conflicts }}</span>
              @if (badges.openConflictsCount() > 0) {
                <span class="nav-badge nav-badge-danger" aria-hidden="true">{{
                  badgeCount(badges.openConflictsCount())
                }}</span>
              }
            </a>

            @if (auth.isAdmin()) {
              <a
                class="nav-item"
                routerLink="/notifications"
                routerLinkActive="active"
                ariaCurrentWhenActive="page"
                [attr.aria-label]="text.nav.notifications"
                [appSidebarHint]="compactSidebar() ? text.nav.notifications : ''"
                (click)="mobileSidebarOpen.set(false)"
              >
                <svg lucideMail [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
                <span class="nav-label">{{ text.nav.notifications }}</span>
              </a>
              <a
                class="nav-item"
                routerLink="/audit"
                routerLinkActive="active"
                ariaCurrentWhenActive="page"
                [attr.aria-label]="text.nav.audit"
                [appSidebarHint]="compactSidebar() ? text.nav.audit : ''"
                (click)="mobileSidebarOpen.set(false)"
              >
                <svg lucideHistory [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
                <span class="nav-label">{{ text.nav.audit }}</span>
              </a>
              <a
                class="nav-item"
                routerLink="/settings"
                routerLinkActive="active"
                ariaCurrentWhenActive="page"
                [attr.aria-label]="text.nav.settings"
                [appSidebarHint]="compactSidebar() ? text.nav.settings : ''"
                (click)="mobileSidebarOpen.set(false)"
              >
                <svg lucideSettings [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
                <span class="nav-label">{{ text.nav.settings }}</span>
              </a>
            }
          </nav>

          <div class="sidebar-footer">
            <div
              class="user-card"
              role="group"
              [attr.tabindex]="compactSidebar() ? 0 : null"
              [attr.aria-label]="userLabel()"
              [appSidebarHint]="compactSidebar() ? userLabel() : ''"
            >
              <span class="user-avatar" aria-hidden="true">{{ userInitial() }}</span>
              <div class="user-details">
                <div class="user-name">{{ auth.currentUser()?.displayName }}</div>
                <div class="user-role">
                  {{
                    auth.currentUser()
                      ? auth.isAdmin()
                        ? text.user.adminBadge
                        : text.user.userBadge
                      : ''
                  }}
                </div>
              </div>
            </div>
          </div>
        </aside>
      }

      <main class="workspace" [attr.inert]="isMobile() && mobileSidebarOpen() ? '' : null">
        <div class="workspace-inner">
          <div class="workspace-actions" role="group" [attr.aria-label]="text.aria.accountActions">
            <button
              type="button"
              class="btn btn-ghost theme-btn"
              (click)="themeService.toggleTheme()"
              [attr.aria-label]="text.theme.toggleLabel"
            >
              @if (themeService.currentTheme() === 'light') {
                <svg lucideMoon [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
                <span class="theme-label">{{ text.theme.dark }}</span>
              } @else {
                <svg lucideSun [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
                <span class="theme-label">{{ text.theme.light }}</span>
              }
            </button>
            @if (auth.currentUser()) {
              <button type="button" class="btn btn-ghost" (click)="logout()">
                <svg lucideLogOut [size]="18" [strokeWidth]="2" aria-hidden="true"></svg>
                <span>{{ text.user.logout }}</span>
              </button>
            }
          </div>
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
        transition: grid-template-columns var(--motion-fast) var(--ease-out);
      }
      .app-shell.sidebar-collapsed {
        grid-template-columns: var(--sidebar-collapsed-width) minmax(0, 1fr);
      }
      .mobile-menu-toggle,
      .mobile-sidebar-close,
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
        min-width: 0;
      }
      .sidebar-header {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 4px 0 20px;
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
      .brand-text {
        flex: 1;
        min-width: 0;
        overflow-wrap: anywhere;
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
      .desktop-sidebar-toggle,
      .mobile-sidebar-close {
        width: 40px;
        height: 40px;
        padding: 8px;
        flex-shrink: 0;
      }
      .sidebar-nav {
        display: flex;
        flex-direction: column;
        gap: 4px;
        flex: 1;
      }
      .nav-item {
        position: relative;
        display: flex;
        align-items: center;
        gap: 10px;
        min-height: 44px;
        padding: 9px 12px;
        border-radius: var(--radius-md);
        color: var(--color-text-secondary);
        text-decoration: none;
        font-size: var(--font-size-body);
        font-weight: 500;
        transition:
          background-color var(--motion-fast) var(--ease-out),
          color var(--motion-fast) var(--ease-out);
        svg {
          flex-shrink: 0;
        }
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
      }
      .nav-item:focus-visible,
      .user-card:focus-visible {
        outline: 2px solid var(--color-border-focus);
        outline-offset: -2px;
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
      .sidebar-footer {
        border-top: 1px solid var(--color-border-default);
        padding-top: 14px;
      }
      .user-card {
        padding: 4px 10px;
        border-radius: var(--radius-md);
      }
      .user-name {
        font-size: var(--font-size-meta);
        font-weight: 600;
        color: var(--color-text-primary);
        overflow-wrap: anywhere;
      }
      .user-role {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .user-avatar {
        display: none;
      }
      .sidebar-collapsed {
        .sidebar {
          padding-inline: 8px;
        }
        .sidebar-header {
          flex-direction: column;
          padding-bottom: 12px;
        }
        .brand-text,
        .nav-label,
        .user-details {
          display: none;
        }
        .nav-item {
          justify-content: center;
          padding-inline: 0;
        }
        .nav-badge {
          position: absolute;
          right: 0;
          top: 0;
          height: 16px;
          min-width: 16px;
          padding: 0 3px;
          font-size: 10px;
        }
        .user-card {
          padding: 4px 0;
          display: flex;
          justify-content: center;
        }
        .user-avatar {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          width: 36px;
          height: 36px;
          border-radius: var(--radius-full);
          background: var(--color-surface);
          color: var(--color-text-primary);
          font-size: var(--font-size-meta);
          font-weight: 600;
        }
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
      .workspace-actions {
        display: flex;
        justify-content: flex-end;
        align-items: center;
        gap: 8px;
        min-height: 40px;
        margin-bottom: 16px;
      }
      @media (max-width: 900px) {
        .app-shell,
        .app-shell.sidebar-collapsed {
          grid-template-columns: minmax(0, 1fr);
        }
        .mobile-menu-toggle {
          display: inline-flex;
          position: fixed;
          top: 12px;
          left: 16px;
          width: 40px;
          height: 40px;
          padding: 8px;
          z-index: 600;
        }
        .mobile-backdrop {
          display: block;
          position: fixed;
          inset: 0;
          border: 0;
          padding: 0;
          background: var(--scrim);
          z-index: 650;
          cursor: pointer;
        }
        .sidebar {
          position: fixed;
          left: 0;
          top: 0;
          bottom: 0;
          width: var(--sidebar-width);
          max-width: 100vw;
          z-index: 700;
          animation: sidebar-enter var(--motion-enter) var(--ease-out);
        }
        .desktop-sidebar-toggle {
          display: none;
        }
        .mobile-sidebar-close {
          display: inline-flex;
        }
        .workspace {
          padding: 12px 16px 24px;
        }
        .workspace-actions {
          min-height: 40px;
          padding-left: 48px;
        }
        @keyframes sidebar-enter {
          from {
            transform: translateX(-100%);
          }
          to {
            transform: translateX(0);
          }
        }
      }
      @media (max-width: 480px) {
        .theme-label {
          display: none;
        }
        .theme-btn {
          width: 40px;
          height: 40px;
          padding: 8px;
        }
      }
    `,
  ],
})
export class ShellLayoutComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  protected readonly text = SHELL_TEXT;
  protected readonly themeService = inject(ThemeService);
  protected readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  protected readonly badges = inject(SidebarBadgesService);
  protected readonly mobileSidebarOpen = signal(false);
  protected readonly sidebarCollapsed = signal(readSidebarPreference());
  protected readonly isMobile = signal(
    typeof window !== 'undefined' && window.matchMedia('(max-width: 900px)').matches,
  );
  protected readonly compactSidebar = computed(() => this.sidebarCollapsed() && !this.isMobile());
  protected readonly campusName = signal<string>(SHELL_TEXT.appName);
  protected readonly userLabel = computed(() => {
    const user = this.auth.currentUser();
    return user
      ? this.text.user.identity(user.displayName || user.username, this.auth.isAdmin())
      : '';
  });
  protected readonly userInitial = computed(
    () =>
      Array.from(
        this.auth.currentUser()?.displayName || this.auth.currentUser()?.username || '',
      )[0] ?? '',
  );

  protected badgeCount(count: number): string | number {
    return count > 99 ? '99+' : count;
  }

  protected toggleSidebar(): void {
    this.sidebarCollapsed.update((value) => !value);
    try {
      localStorage.setItem(SIDEBAR_PREFERENCE_KEY, String(this.sidebarCollapsed()));
    } catch {
      // 瀏覽器禁止儲存時，仍可使用目前工作階段的折疊狀態。
    }
  }

  @HostListener('document:keydown.escape')
  protected closeMobileOnEscape(): void {
    this.mobileSidebarOpen.set(false);
  }

  @HostListener('window:resize')
  protected updateViewport(): void {
    const mobile = window.matchMedia('(max-width: 900px)').matches;
    if (mobile === this.isMobile()) return;
    const focusInSidebar = !!this.host.nativeElement
      .querySelector('.sidebar')
      ?.contains(document.activeElement);
    const focusOnMobileToggle = document.activeElement?.classList.contains('mobile-menu-toggle');
    this.mobileSidebarOpen.set(false);
    this.isMobile.set(mobile);
    if (focusInSidebar || focusOnMobileToggle) {
      setTimeout(() => {
        this.host.nativeElement
          .querySelector<HTMLButtonElement>(
            mobile ? '.mobile-menu-toggle' : '.desktop-sidebar-toggle',
          )
          ?.focus();
      });
    }
  }

  /** 登出後回到登入頁；不保留目的地，下一位使用者登入後從首頁開始。 */
  protected logout(): void {
    this.auth.logout().subscribe(() => void this.router.navigateByUrl('/login'));
  }

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
