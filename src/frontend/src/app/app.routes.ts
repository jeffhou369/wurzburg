import { Routes } from '@angular/router';
import { ShellLayoutComponent } from './shared/components/shell-layout.component';
import { WorkbenchPage } from './features/workbench/workbench.page';
import { SubnetsPage } from './features/subnets/subnets.page';
import { SwitchesPage } from './features/switches/switches.page';
import { ConflictsPage } from './features/conflicts/conflicts.page';
import { NotificationsPage } from './features/notifications/notifications.page';
import { AuditPage } from './features/audit/audit.page';
import { SettingsPage } from './features/settings/settings.page';
import { ShowcasePage } from './features/showcase/showcase.page';
import { LoginPage } from './features/auth/login.page';
import { ChangePasswordPage } from './features/auth/change-password.page';
import { authGuard } from './core/auth.guard';

export const routes: Routes = [
  { path: 'login', component: LoginPage },
  { path: 'change-password', component: ChangePasswordPage },
  {
    path: '',
    component: ShellLayoutComponent,
    // 所有 Shell 內的頁面都需要有效工作階段；授權仍由後端 API 逐筆判斷
    canActivate: [authGuard],
    canActivateChild: [authGuard],
    children: [
      { path: '', component: WorkbenchPage },
      { path: 'subnets', component: SubnetsPage },
      { path: 'switches', component: SwitchesPage },
      { path: 'conflicts', component: ConflictsPage },
      { path: 'notifications', component: NotificationsPage },
      { path: 'audit', component: AuditPage },
      { path: 'settings', component: SettingsPage },
      { path: 'showcase', component: ShowcasePage },
    ],
  },
  { path: '**', redirectTo: '' },
];
