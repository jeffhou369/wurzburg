import { Injectable, signal } from '@angular/core';

export type ToastVariant = 'info' | 'success' | 'warning' | 'danger';

export interface ToastMessage {
  id: string;
  title: string;
  detail?: string;
  variant: ToastVariant;
}

@Injectable({
  providedIn: 'root',
})
export class ToastService {
  readonly toasts = signal<ToastMessage[]>([]);

  show(title: string, variant: ToastVariant = 'info', detail?: string, durationMs = 4500): void {
    const id = Math.random().toString(36).slice(2, 10);
    this.toasts.update((list) => [...list, { id, title, detail, variant }]);

    if (durationMs > 0) {
      setTimeout(() => this.dismiss(id), durationMs);
    }
  }

  dismiss(id: string): void {
    this.toasts.update((list) => list.filter((t) => t.id !== id));
  }
}
