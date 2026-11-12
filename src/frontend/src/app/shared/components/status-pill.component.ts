import { Component, input } from '@angular/core';

export type StatusPillVariant =
  'online' | 'offline' | 'unregistered' | 'registered' | 'expiring' | 'expired' | 'conflict';

@Component({
  selector: 'app-status-pill',
  standalone: true,
  template: `
    <span class="status-pill" [class]="'status-pill status-' + variant()">
      <span class="status-dot" aria-hidden="true"></span>
      <span>{{ label() }}</span>
    </span>
  `,
})
export class StatusPillComponent {
  readonly variant = input.required<StatusPillVariant>();
  readonly label = input.required<string>();
}
