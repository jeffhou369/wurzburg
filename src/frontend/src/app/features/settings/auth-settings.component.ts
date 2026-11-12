import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { LucideTriangleAlert } from '@lucide/angular';
import { ToastService } from '../../shared/components/toast.service';

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
      <h2 class="section-title">一般使用者驗證來源設定</h2>
      <p class="section-desc">
        一個園區同時僅啟用一種外部驗證來源（AD 或 RADIUS），系統管理員帳號不經此路徑驗證。
      </p>

      @if (
        affectedCount() > 0 && initialSource() !== 'none' && form.active_source !== initialSource()
      ) {
        <div class="alert-box alert-warning" role="alert">
          <svg lucideTriangleAlert [size]="18" [strokeWidth]="2"></svg>
          <div>
            <strong>切換驗證來源警告：</strong>
            目前共有
            {{ affectedCount() }}
            筆登記屬於原驗證來源帳號；更換後原擁有者將無法登入，名下登記需由系統管理員改派。
          </div>
        </div>
      }

      <div class="source-selector">
        <label class="radio-card" [class.selected]="form.active_source === 'ad'">
          <input type="radio" name="active_source" value="ad" [(ngModel)]="form.active_source" />
          <div>
            <div class="radio-title">Active Directory (LDAP / LDAPS)</div>
            <div class="radio-sub">支援群組限制，登入自動帶入信箱與顯示名稱</div>
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
            <div class="radio-sub">僅驗證帳密，使用者登記時自行填寫信箱</div>
          </div>
        </label>
      </div>

      @if (form.active_source === 'ad') {
        <form class="form-grid" (ngSubmit)="save()">
          <div class="field-group full">
            <label for="adUrl">伺服器網址（建議 ldaps://）</label>
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
              <span>啟用 TLS 加密連線（強烈建議生產環境保持勾選）</span>
            </label>
            @if (!form.ad_use_tls) {
              <div class="alert-box alert-danger">
                未加密的目錄綁定將以明文傳輸組織密碼，僅供受控實驗室使用。
              </div>
            }
          </div>

          <div class="field-group">
            <label for="adBindDn">查詢帳號 Bind DN</label>
            <input
              id="adBindDn"
              name="adBindDn"
              type="text"
              class="form-input font-mono"
              [(ngModel)]="form.ad_bind_dn"
            />
          </div>

          <div class="field-group">
            <label for="adBindPw"
              >查詢密碼 {{ hasAdPass() ? '（已設定，留空則保持不變）' : '' }}</label
            >
            <input
              id="adBindPw"
              name="adBindPw"
              type="password"
              class="form-input"
              [(ngModel)]="adBindPassword"
            />
          </div>

          <div class="field-group">
            <label for="adBaseDn">搜尋起點 Base DN</label>
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
            <label for="adGroup">允許登入群組 DN（選填，留空則全部允許）</label>
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
              儲存 AD 設定
            </button>
          </div>
        </form>
      }

      @if (form.active_source === 'radius') {
        <form class="form-grid" (ngSubmit)="save()">
          <div class="field-group">
            <label for="radHost">RADIUS 伺服器 IP / 主機名稱</label>
            <input
              id="radHost"
              name="radHost"
              type="text"
              class="form-input font-mono"
              [(ngModel)]="form.radius_host"
            />
          </div>

          <div class="field-group">
            <label for="radPort">驗證埠（預設 1812）</label>
            <input
              id="radPort"
              name="radPort"
              type="number"
              class="form-input font-mono"
              [(ngModel)]="form.radius_port"
            />
          </div>

          <div class="field-group">
            <label for="radSecret"
              >Shared Secret {{ hasRadiusSecret() ? '（已設定，留空則保持不變）' : '' }}</label
            >
            <input
              id="radSecret"
              name="radSecret"
              type="password"
              class="form-input"
              [(ngModel)]="radiusSecret"
            />
          </div>

          <div class="field-group">
            <label for="radTimeout">逾時（毫秒）</label>
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
              儲存 RADIUS 設定
            </button>
          </div>
        </form>
      }

      @if (form.active_source === 'ad' || form.active_source === 'radius') {
        <div class="test-panel">
          <h3 class="test-title">測試外部驗證連線</h3>
          <div class="test-row">
            <input
              type="text"
              class="form-input"
              placeholder="測試帳號"
              [(ngModel)]="testUser"
              name="testUser"
            />
            <input
              type="password"
              class="form-input"
              placeholder="測試密碼"
              [(ngModel)]="testPass"
              name="testPass"
            />
            <button
              type="button"
              class="btn btn-secondary"
              [disabled]="testing()"
              (click)="testConnection()"
            >
              {{ testing() ? '測試中...' : '測試登入' }}
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
          this.toast.show('驗證來源設定已儲存', 'success');
        },
        error: (err) => {
          this.saving.set(false);
          this.toast.show('儲存失敗', 'danger', err?.error?.message ?? '請檢查欄位');
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
            this.toast.show('測試登入成功', 'success', detail);
          } else {
            this.toast.show('測試登入失敗', 'danger', res.message);
          }
        },
        error: (err) => {
          this.testing.set(false);
          this.toast.show('測試失敗', 'danger', err?.error?.message ?? '連線異常');
        },
      });
  }
}
