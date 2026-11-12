import { Type } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideRouter } from '@angular/router';
import { DIALOG_DATA, DialogRef } from '@angular/cdk/dialog';
import { AuditPage } from './features/audit/audit.page';
import { ChangePasswordPage } from './features/auth/change-password.page';
import { LoginPage } from './features/auth/login.page';
import { ConflictsPage } from './features/conflicts/conflicts.page';
import { NotificationsPage } from './features/notifications/notifications.page';
import { AuthSettingsComponent } from './features/settings/auth-settings.component';
import { CampusSettingsComponent } from './features/settings/campus-settings.component';
import { MailSettingsComponent } from './features/settings/mail-settings.component';
import { OuiSettingsComponent } from './features/settings/oui-settings.component';
import { RetentionSettingsComponent } from './features/settings/retention-settings.component';
import { SettingsPage } from './features/settings/settings.page';
import { SetupWizardDialogComponent } from './features/settings/setup-wizard.dialog';
import { ShowcasePage } from './features/showcase/showcase.page';
import { SubnetsPage } from './features/subnets/subnets.page';
import { SwitchesPage } from './features/switches/switches.page';
import { UnfinishedChecklistComponent } from './features/workbench/unfinished-checklist.component';
import { WorkbenchPage } from './features/workbench/workbench.page';
import { ConfirmDialogComponent } from './shared/components/confirm-dialog.component';
import { EmptyStateComponent } from './shared/components/empty-state.component';
import { IpRecordSheetComponent } from './shared/components/ip-record-sheet.component';
import { SheetContainerComponent } from './shared/components/sheet-container.component';
import { ShellLayoutComponent } from './shared/components/shell-layout.component';
import { ToastContainerComponent } from './shared/components/toast-container.component';

/**
 * 設計 token 回歸檢查（TestBed 元件層級，非瀏覽器 E2E）。
 *
 * 測試建置會載入與正式版相同的全域樣式（src/styles.scss），所以可以直接以計算後樣式
 * 確認 token 真的存在；再把帶有樣式的元件建立起來，掃描它們注入的 <style> 內容：
 * 1. 引用的 var(--token) 必須已在全域定義，不能引用不存在的 token 而靜默失效。
 * 2. 元件樣式不得自己寫死色碼（#hex、rgb()/rgba()/hsl()），一律走集中 token。
 * 新增帶樣式的元件時請一併加入下方清單。
 */
const STYLED_COMPONENTS: Type<unknown>[] = [
  AuditPage,
  ChangePasswordPage,
  LoginPage,
  ConflictsPage,
  NotificationsPage,
  AuthSettingsComponent,
  CampusSettingsComponent,
  MailSettingsComponent,
  OuiSettingsComponent,
  RetentionSettingsComponent,
  SettingsPage,
  SetupWizardDialogComponent,
  ShowcasePage,
  SubnetsPage,
  SwitchesPage,
  UnfinishedChecklistComponent,
  WorkbenchPage,
  ConfirmDialogComponent,
  EmptyStateComponent,
  IpRecordSheetComponent,
  SheetContainerComponent,
  ShellLayoutComponent,
  ToastContainerComponent,
];

function rootToken(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function withTheme<T>(theme: 'light' | 'dark', fn: () => T): T {
  const previous = document.documentElement.getAttribute('data-theme');
  if (theme === 'dark') {
    document.documentElement.setAttribute('data-theme', 'dark');
  } else {
    document.documentElement.removeAttribute('data-theme');
  }
  try {
    return fn();
  } finally {
    if (previous === null) {
      document.documentElement.removeAttribute('data-theme');
    } else {
      document.documentElement.setAttribute('data-theme', previous);
    }
  }
}

describe('設計 token（集中於 src/styles/tokens.scss）', () => {
  const requiredTokens = [
    '--color-success',
    '--color-success-subtle',
    '--color-success-border',
    '--color-success-text',
    '--color-danger',
    '--color-danger-subtle',
    '--color-danger-border',
    '--color-danger-text',
    '--color-danger-strong',
    '--color-text-on-success',
    '--color-text-on-danger',
    '--color-overlay-backdrop',
    '--shadow-dialog',
    '--motion-fast',
    '--ease-out',
  ];

  for (const theme of ['light', 'dark'] as const) {
    it(`${theme} 主題下必要 token 皆有定義`, () => {
      withTheme(theme, () => {
        for (const name of requiredTokens) {
          expect(rootToken(name)).withContext(`${theme}: ${name}`).not.toBe('');
        }
      });
    });
  }

  it('深色主題的遮罩與實心狀態色上的文字色需有獨立設定，不可沿用淺色值', () => {
    const light = withTheme('light', () => ({
      overlay: rootToken('--color-overlay-backdrop'),
      onSuccess: rootToken('--color-text-on-success'),
    }));
    const dark = withTheme('dark', () => ({
      overlay: rootToken('--color-overlay-backdrop'),
      onSuccess: rootToken('--color-text-on-success'),
    }));
    expect(dark.overlay).not.toBe(light.overlay);
    expect(dark.onSuccess).not.toBe(light.onSuccess);
  });

  it('曾被誤用但從未定義的 token 不應存在，避免日後又有人引用', () => {
    for (const name of [
      '--color-online',
      '--color-online-bg',
      '--color-online-border',
      '--color-conflict',
      '--color-conflict-bg',
      '--color-conflict-border',
      '--shadow-xl',
      '--motion-subtle',
    ]) {
      expect(rootToken(name)).withContext(name).toBe('');
    }
  });
});

describe('元件樣式只引用已定義的 token 且不寫死色碼', () => {
  let componentCss: { source: string; css: string }[] = [];

  beforeAll(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: DialogRef, useValue: { close: () => {} } },
        { provide: DIALOG_DATA, useValue: {} },
      ],
    });

    const baseline = new Set(Array.from(document.head.querySelectorAll('style')));
    const fixtures = STYLED_COMPONENTS.map((type) => TestBed.createComponent(type));
    componentCss = Array.from(document.head.querySelectorAll('style'))
      .filter((el) => !baseline.has(el))
      .map((el, index) => ({ source: `元件樣式 #${index}`, css: el.textContent ?? '' }));
    fixtures.forEach((f) => f.destroy());
  });

  it('清單中的元件確實注入了樣式', () => {
    expect(componentCss.length).toBeGreaterThanOrEqual(STYLED_COMPONENTS.length - 4);
  });

  it('引用的 var(--token) 都已在全域定義', () => {
    const missing = new Set<string>();
    for (const { css } of componentCss) {
      for (const match of css.matchAll(/var\(\s*(--[a-zA-Z0-9_-]+)/g)) {
        // 元件自己以 `--name:` 宣告的區域變數不算
        const declaredLocally = new RegExp(`${match[1]}\\s*:`).test(css);
        if (!declaredLocally && rootToken(match[1]) === '') {
          missing.add(match[1]);
        }
      }
    }
    expect(Array.from(missing)).toEqual([]);
  });

  it('元件樣式不得出現寫死的色碼（改用 tokens.scss）', () => {
    const offenders: string[] = [];
    for (const { css } of componentCss) {
      for (const match of css.matchAll(
        // 十六進位色碼只認宣告值位置（後面緊接 ; , ) } !），避免把 #id 選擇器誤判為色碼
        /(?<=[:\s,(])#[0-9a-fA-F]{3,8}(?=\s*[;,)}!])|rgba?\([^)]*\)|hsla?\([^)]*\)/g,
      )) {
        offenders.push(match[0]);
      }
    }
    expect(offenders).toEqual([]);
  });
});
