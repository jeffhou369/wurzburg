import { Injectable, signal, effect } from '@angular/core';

export type AppTheme = 'light' | 'dark';

@Injectable({
  providedIn: 'root',
})
export class ThemeService {
  readonly currentTheme = signal<AppTheme>('light');

  constructor() {
    try {
      const saved = localStorage.getItem('wurzburg_theme');
      if (saved === 'dark' || saved === 'light') {
        this.currentTheme.set(saved);
      }
    } catch {
      // 無法讀取瀏覽器儲存時，仍以預設淺色主題啟動。
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
        try {
          localStorage.setItem('wurzburg_theme', theme);
        } catch {
          // 儲存遭封鎖不影響目前頁面的主題切換。
        }
      }
    });
  }

  toggleTheme(): void {
    this.currentTheme.update((t) => (t === 'light' ? 'dark' : 'light'));
  }
}
