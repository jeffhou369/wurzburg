import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { LucideTriangleAlert } from '@lucide/angular';
import { ToastService } from '../../shared/components/toast.service';
import { AUTH_SETTINGS_TEXT } from './auth-settings.i18n';

interface AuthConfigResponse {
  active_source: 'none' | 'ad' | 'radius';
  affected_registered_count: number;
  ad_url: string;
  ad_use_tls: boolean;
  ad_bind_dn: string;
  ad_has_bind_password: boolean;
  ad_base_dn: string;
  ad_allowed_group: string;
  ad_user_attr: string;
  ad_mail_attr: string;
  ad_name_attr: string;
  radius_host: string;
  radius_port: number;
  radius_timeout_ms: number;
  radius_has_secret: boolean;
}

@Component({
  selector: 'app-auth-settings',
  standalone: true,
  imports: [FormsModule, LucideTriangleAlert],
  template: `
    <div class="auth-settings">
      <h2 class="section-title">{{ text.title }}</h2>
      <p class="section-desc">
        {{ text.description }}
      </p>

      @if (
        affectedCount() > 0 && initialSource() !== 'none' && form.active_source !== initialSource()
      ) {
        <div class="alert-box alert-warning" role="alert">
          <svg lucideTriangleAlert [size]="18" [strokeWidth]="2"></svg>
          <div>
            <strong>{{ text.switchWarning.title }}</strong>
            {{ text.switchWarning.body(affectedCount()) }}
          </div>
        </div>
      }

      <div class="source-selector">
        <label class="radio-card" [class.selected]="form.active_source === 'ad'">
          <input type="radio" name="active_source" value="ad" [(ngModel)]="form.active_source" />
          <div>
            <div class="radio-title">Active Directory (LDAP / LDAPS)</div>
            <div class="radio-sub">{{ text.sources.adDescription }}</div>
          </div>
        </label>

        <label class="radio-card" [class.selected]="form.active_source === 'radius'">
          <input
            type="radio"
            name="active_source"
            value="radius"
            [(ngModel)]="form.active_source"
          />
          <div>
            <div class="radio-title">RADIUS (PAP)</div>
            <div class="radio-sub">{{ text.sources.radiusDescription }}</div>
          </div>
        </label>
      </div>

      @if (form.active_source === 'ad') {
        <form class="form-grid" (ngSubmit)="save()">
          <div class="field-group full">
            <label for="adUrl">{{ text.ad.url }}</label>
            <input
              id="adUrl"
              name="adUrl"
              type="text"
              class="form-input font-mono"
              placeholder="ldaps://dc01.corp.local:636"
              [(ngModel)]="form.ad_url"
            />
          </div>

          <div class="field-group full">
            <label class="checkbox-label">
              <input type="checkbox" name="adUseTls" [(ngModel)]="form.ad_use_tls" />
              <span>{{ text.ad.useTls }}</span>
            </label>
            @if (!form.ad_use_tls) {
              <div class="alert-box alert-danger">
                {{ text.ad.plaintextWarning }}
              </div>
            }
          </div>

          <div class="field-group">
            <label for="adBindDn">{{ text.ad.bindDn }}</label>
            <input
              id="adBindDn"
              name="adBindDn"
              type="text"
              class="form-input font-mono"
              [(ngModel)]="form.ad_bind_dn"
            />
          </div>

          <div class="field-group">
            <label for="adBindPw">{{ text.ad.bindPassword(hasAdPass()) }}</label>
            <input
              id="adBindPw"
              name="adBindPw"
              type="password"
              class="form-input"
              [(ngModel)]="adBindPassword"
            />
          </div>

          <div class="field-group">
            <label for="adBaseDn">{{ text.ad.baseDn }}</label>
            <input
              id="adBaseDn"
              name="adBaseDn"
              type="text"
              class="form-input font-mono"
              placeholder="dc=corp,dc=local"
              [(ngModel)]="form.ad_base_dn"
            />
          </div>

          <div class="field-group">
            <label for="adGroup">{{ text.ad.allowedGroup }}</label>
            <input
              id="adGroup"
              name="adGroup"
              type="text"
              class="form-input font-mono"
              [(ngModel)]="form.ad_allowed_group"
            />
          </div>

          <div class="actions">
            <button type="submit" class="btn btn-primary" [disabled]="saving()">
              {{ text.ad.save }}
            </button>
          </div>
        </form>
      }

      @if (form.active_source === 'radius') {
        <form class="form-grid" (ngSubmit)="save()">
          <div class="field-group">
            <label for="radHost">{{ text.radius.host }}</label>
            <input
              id="radHost"
              name="radHost"
              type="text"
              class="form-input font-mono"
              [(ngModel)]="form.radius_host"
            />
          </div>

          <div class="field-group">
            <label for="radPort">{{ text.radius.port }}</label>
            <input
              id="radPort"
              name="radPort"
              type="number"
              class="form-input font-mono"
              [(ngModel)]="form.radius_port"
            />
          </div>

          <div class="field-group">
            <label for="radSecret">{{ text.radius.secret(hasRadiusSecret()) }}</label>
            <input
              id="radSecret"
              name="radSecret"
              type="password"
              class="form-input"
              [(ngModel)]="radiusSecret"
            />
          </div>

          <div class="field-group">
            <label for="radTimeout">{{ text.radius.timeout }}</label>
            <input
              id="radTimeout"
              name="radTimeout"
              type="number"
              class="form-input font-mono"
              [(ngModel)]="form.radius_timeout_ms"
            />
          </div>

          <div class="actions">
            <button type="submit" class="btn btn-primary" [disabled]="saving()">
              {{ text.radius.save }}
            </button>
          </div>
        </form>
      }

      @if (form.active_source === 'ad' || form.active_source === 'radius') {
        <div class="test-panel">
          <h3 class="test-title">{{ text.test.title }}</h3>
          <div class="test-row">
            <input
              type="text"
              class="form-input"
              [placeholder]="text.test.usernamePlaceholder"
              [(ngModel)]="testUser"
              name="testUser"
            />
            <input
              type="password"
              class="form-input"
              [placeholder]="text.test.passwordPlaceholder"
              [(ngModel)]="testPass"
              name="testPass"
            />
            <button
              type="button"
              class="btn btn-secondary"
              [disabled]="testing()"
              (click)="testConnection()"
            >
              {{ testing() ? text.test.running : text.test.run }}
            </button>
          </div>
        </div>
      }
    </div>
  `,
  styles: [
    `
      .section-title {
        margin: 0 0 6px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
      }
      .section-desc {
        margin: 0 0 20px 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
      .alert-box {
        display: flex;
        align-items: flex-start;
        gap: 10px;
        padding: 12px;
        border-radius: var(--radius-md);
        margin-bottom: 16px;
        font-size: var(--font-size-meta);
      }
      .alert-warning {
        background-color: var(--color-warning-subtle);
        border: 1px solid var(--color-warning-border);
        color: var(--color-warning-text);
      }
      .alert-danger {
        background-color: var(--color-danger-subtle);
        border: 1px solid var(--color-danger-border);
        color: var(--color-danger-text);
        margin-top: 6px;
      }
      .source-selector {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
        gap: 12px;
        margin-bottom: 24px;
      }
      .radio-card {
        display: flex;
        align-items: flex-start;
        gap: 10px;
        padding: 14px;
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
        cursor: pointer;

        &.selected {
          border-color: var(--color-accent);
          background-color: var(--color-surface-subtle);
        }
      }
      .radio-title {
        font-weight: 600;
        font-size: var(--font-size-body);
      }
      .radio-sub {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .form-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
        gap: 14px;
      }
      .field-group {
        display: flex;
        flex-direction: column;
        gap: 6px;
        &.full {
          grid-column: 1 / -1;
        }
        label {
          font-size: var(--font-size-meta);
          font-weight: 500;
        }
      }
      .checkbox-label {
        display: flex;
        align-items: center;
        gap: 8px;
        cursor: pointer;
      }
      .actions {
        grid-column: 1 / -1;
        display: flex;
        justify-content: flex-end;
      }
      .test-panel {
        margin-top: 24px;
        padding-top: 20px;
        border-top: 1px solid var(--color-border-default);
      }
      .test-title {
        margin: 0 0 12px 0;
        font-size: var(--font-size-meta);
        font-weight: 600;
      }
      .test-row {
        display: grid;
        grid-template-columns: 1fr 1fr auto;
        gap: 10px;
      }
    `,
  ],
})
export class AuthSettingsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly toast = inject(ToastService);
  protected readonly text = AUTH_SETTINGS_TEXT;

  protected readonly initialSource = signal<'none' | 'ad' | 'radius'>('none');
  protected readonly affectedCount = signal(0);
  protected readonly hasAdPass = signal(false);
  protected readonly hasRadiusSecret = signal(false);
  protected readonly saving = signal(false);
  protected readonly testing = signal(false);

  protected adBindPassword = '';
  protected radiusSecret = '';
  protected testUser = '';
  protected testPass = '';

  protected form: AuthConfigResponse = {
    active_source: 'ad',
    affected_registered_count: 0,
    ad_url: '',
    ad_use_tls: true,
    ad_bind_dn: '',
    ad_has_bind_password: false,
    ad_base_dn: '',
    ad_allowed_group: '',
    ad_user_attr: 'sAMAccountName',
    ad_mail_attr: 'mail',
    ad_name_attr: 'displayName',
    radius_host: '',
    radius_port: 1812,
    radius_timeout_ms: 5000,
    radius_has_secret: false,
  };

  ngOnInit(): void {
    this.http.get<AuthConfigResponse>('/api/v1/settings/auth-source/config').subscribe({
      next: (res) => {
        this.initialSource.set(res.active_source);
        this.affectedCount.set(res.affected_registered_count);
        this.hasAdPass.set(res.ad_has_bind_password);
        this.hasRadiusSecret.set(res.radius_has_secret);
        if (res.active_source !== 'none') {
          this.form = { ...this.form, ...res };
        }
      },
      error: () => {},
    });
  }

  save(): void {
    this.saving.set(true);
    const payload = {
      ...this.form,
      ad_bind_password: this.adBindPassword,
      radius_secret: this.radiusSecret,
    };

    this.http
      .put<AuthConfigResponse>('/api/v1/settings/auth-source', payload, {
        headers: { 'X-Requested-With': 'XMLHttpRequest' },
      })
      .subscribe({
        next: (res) => {
          this.saving.set(false);
          this.adBindPassword = '';
          this.radiusSecret = '';
          this.initialSource.set(res.active_source);
          this.hasAdPass.set(res.ad_has_bind_password);
          this.hasRadiusSecret.set(res.radius_has_secret);
          this.toast.show(this.text.saved, 'success');
        },
        error: (err) => {
          this.saving.set(false);
          this.toast.show(
            this.text.saveFailed,
            'danger',
            err?.error?.message ?? this.text.saveFailedHint,
          );
        },
      });
  }

  testConnection(): void {
    if (!this.testUser || !this.testPass) return;
    this.testing.set(true);
    this.http
      .post<{ success: boolean; message: string; mail?: string; display_name?: string }>(
        '/api/v1/settings/auth-source/test',
        { username: this.testUser, password: this.testPass },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          this.testing.set(false);
          if (res.success) {
            const detail = res.mail
              ? `${res.display_name} (${res.mail})`
              : (res.display_name ?? '');
            this.toast.show(this.text.test.success, 'success', detail);
          } else {
            this.toast.show(this.text.test.failure, 'danger', res.message);
          }
        },
        error: (err) => {
          this.testing.set(false);
          this.toast.show(
            this.text.test.error,
            'danger',
            err?.error?.message ?? this.text.test.errorHint,
          );
        },
      });
  }
}
