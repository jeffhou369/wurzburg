import { Injectable, signal } from '@angular/core';

@Injectable({
  providedIn: 'root',
})
export class SidebarBadgesService {
  readonly pendingSubnetsCount = signal<number>(0);
  readonly openConflictsCount = signal<number>(0);
}
