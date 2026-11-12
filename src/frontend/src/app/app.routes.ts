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

export const routes: Routes = [
  {
    path: '',
    component: ShellLayoutComponent,
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
