import { Component, OnInit, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { RouterOutlet } from '@angular/router';

interface HealthStatus {
  status: string;
  timestamp: string;
  database: string;
  campus: string;
}

@Component({
  selector: 'app-root',
  imports: [RouterOutlet],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App implements OnInit {
  private readonly http = inject(HttpClient);

  protected readonly title = signal('Würzburg');
  protected readonly health = signal<HealthStatus | null>(null);
  protected readonly loading = signal(true);
  protected readonly error = signal<string | null>(null);

  ngOnInit(): void {
    this.fetchHealth();
  }

  protected fetchHealth(): void {
    this.loading.set(true);
    this.error.set(null);
    this.http.get<HealthStatus>('/api/v1/health').subscribe({
      next: (data) => {
        this.health.set(data);
        this.loading.set(false);
      },
      error: () => {
        this.error.set('無法連線至後端服務');
        this.loading.set(false);
      },
    });
  }
}
