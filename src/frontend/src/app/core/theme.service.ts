import { Injectable, signal, effect } from '@angular/core';

export type AppTheme = 'light' | 'dark';

@Injectable({
  providedIn: 'root',
})
export class ThemeService {
  readonly currentTheme = signal<AppTheme>('light');

  constructor() {
    const saved = localStorage.getItem('wurzburg_theme') as AppTheme | null;
    if (saved === 'dark' || saved === 'light') {
      this.currentTheme.set(saved);
    }

    effect(() => {
      const theme = this.currentTheme();
      if (typeof document !== 'undefined') {
        const root = document.documentElement;
        if (theme === 'dark') {
          root.setAttribute('data-theme', 'dark');
        } else {
          root.removeAttribute('data-theme');
        }
        localStorage.setItem('wurzburg_theme', theme);
      }
    });
  }

  toggleTheme(): void {
    this.currentTheme.update((t) => (t === 'light' ? 'dark' : 'light'));
  }
}
