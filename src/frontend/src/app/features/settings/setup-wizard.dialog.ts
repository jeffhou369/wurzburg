import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { DialogRef } from '@angular/cdk/dialog';
import { LucideCheck, LucideX, LucideWand2, LucidePlay, LucideRefreshCw } from '@lucide/angular';
import { EMPTY, Observable, catchError, expand, last, of, switchMap, tap, timer } from 'rxjs';
import { ToastService } from '../../shared/components/toast.service';
import { SETUP_WIZARD_TEXT } from './setup-wizard.i18n';
import type { components } from '../../core/api.types';
import { isValidPublicBaseUrl } from './public-base-url';

type Subnet = components['schemas']['Subnet'];
type SwitchResponse = components['schemas']['Switch'];
type TestReadResult = components['schemas']['TestReadResult'];
type SyncStatus = components['schemas']['SyncStatus'];

interface AuthFormData {
  active_source: 'ad' | 'radius';
  ad_url?: string;
  ad_use_tls?: boolean;
  ad_bind_dn?: string;
  ad_bind_password?: string;
  ad_base_dn?: string;
  radius_host?: string;
  radius_port?: number;
  radius_secret?: string;
}

interface MailFormData {
  smtp_host?: string;
  smtp_port?: number;
  smtp_tls_mode?: 'starttls' | 'smtps' | 'none';
  smtp_username?: string;
  sender_address?: string;
  admin_recipients?: string[];
}

interface SwitchFormData {
  name?: string;
  mgmt_ip?: string;
  platform?: string;
  snmp_version?: string;
  community?: string;
  v3_user?: string;
  v3_auth_proto?: string;
  v3_auth_pass?: string;
  v3_priv_proto?: string;
  v3_priv_pass?: string;
}

@Component({
  selector: 'app-setup-wizard-dialog',
  standalone: true,
  imports: [FormsModule, LucideCheck, LucideX, LucideWand2, LucidePlay, LucideRefreshCw],
  template: `
    <div class="wizard-modal">
      <header class="wizard-header">
        <div class="wizard-header-title">
          <svg lucideWand2 [size]="20" [strokeWidth]="2"></svg>
          <h2>{{ text.title }}</h2>
        </div>
        <button
          type="button"
          class="close-btn"
          (click)="close()"
          [attr.aria-label]="text.closeAria"
        >
          <svg lucideX [size]="20"></svg>
        </button>
      </header>

      <!-- 步驟指示列 -->
      <nav class="steps-nav" [attr.aria-label]="text.stepsAria">
        @for (s of steps; track s.index) {
          <div
            class="step-item"
            [class.active]="currentStep() === s.index"
            [class.completed]="currentStep() > s.index"
          >
            <div class="step-num">
              @if (currentStep() > s.index) {
                <svg lucideCheck [size]="14"></svg>
              } @else {
                {{ s.index }}
              }
            </div>
            <div class="step-label">{{ s.label }}</div>
          </div>
        }
      </nav>

      <!-- 步驟內容區 -->
      <div class="wizard-body">
        @switch (currentStep()) {
          @case (1) {
            <section class="step-content">
              <h3 class="step-title">{{ text.step1.title }}</h3>
              <p class="step-desc">
                {{ text.step1.description }}
              </p>

              <div class="form-stack">
                <div class="field-group">
                  <label for="wzCampus">{{ text.step1.campusName }}</label>
                  <input
                    id="wzCampus"
                    type="text"
                    class="form-input"
                    [(ngModel)]="campusForm.campus_name"
                    placeholder="Würzburg"
                  />
                </div>
                <div class="field-group">
                  <label for="wzTz">{{ text.step1.timezone }}</label>
                  <input
                    id="wzTz"
                    type="text"
                    class="form-input font-mono"
                    [(ngModel)]="campusForm.timezone"
                    placeholder="Asia/Taipei"
                  />
                  <span class="field-help">{{ text.step1.timezoneHelp }}</span>
                </div>
                <div class="field-group">
                  <label for="wzPublicUrl">{{ text.step1.publicBaseUrl }}</label>
                  <input
                    id="wzPublicUrl"
                    type="url"
                    class="form-input font-mono"
                    [(ngModel)]="campusForm.public_base_url"
                    placeholder="https://ip.example.com"
                  />
                  <span class="field-help">{{ text.step1.publicBaseUrlHelp }}</span>
                </div>
              </div>
            </section>
          }

          @case (2) {
            <section class="step-content">
              <h3 class="step-title">{{ text.step2.title }}</h3>
              <p class="step-desc">
                {{ text.step2.description }}
              </p>

              <div class="source-pills">
                <button
                  type="button"
                  class="pill-btn"
                  [class.active]="authForm.active_source === 'ad'"
                  (click)="authForm.active_source = 'ad'"
                >
                  Active Directory (LDAPS)
                </button>
                <button
                  type="button"
                  class="pill-btn"
                  [class.active]="authForm.active_source === 'radius'"
                  (click)="authForm.active_source = 'radius'"
                >
                  RADIUS (PAP)
                </button>
              </div>

              @if (authForm.active_source === 'ad') {
                <div class="form-stack">
                  <div class="field-group">
                    <label for="wzAdUrl">{{ text.step2.adUrl }}</label>
                    <input
                      id="wzAdUrl"
                      type="text"
                      class="form-input"
                      [(ngModel)]="authForm.ad_url"
                      placeholder="ldaps://ad.example.com:636"
                    />
                  </div>
                  <label class="check-row" for="wzAdUseTls">
                    <input id="wzAdUseTls" type="checkbox" [(ngModel)]="authForm.ad_use_tls" />
                    <span>{{ text.step2.adUseTls }}</span>
                  </label>
                  <div class="field-row">
                    <div class="field-group">
                      <label for="wzAdBindDn">{{ text.step2.adBindDn }}</label>
                      <input
                        id="wzAdBindDn"
                        type="text"
                        class="form-input"
                        [(ngModel)]="authForm.ad_bind_dn"
                        placeholder="cn=bind,dc=example,dc=com"
                      />
                    </div>
                    <div class="field-group">
                      <label for="wzAdBindPass">{{ text.step2.adBindPassword }}</label>
                      <input
                        id="wzAdBindPass"
                        type="password"
                        class="form-input"
                        [(ngModel)]="authForm.ad_bind_password"
                        [placeholder]="text.placeholders.password"
                      />
                    </div>
                  </div>
                  <div class="field-group">
                    <label for="wzAdBaseDn">{{ text.step2.adBaseDn }}</label>
                    <input
                      id="wzAdBaseDn"
                      type="text"
                      class="form-input"
                      [(ngModel)]="authForm.ad_base_dn"
                      placeholder="ou=Users,dc=example,dc=com"
                    />
                  </div>
                </div>
              } @else {
                <div class="form-stack">
                  <div class="field-row">
                    <div class="field-group">
                      <label for="wzRadHost">{{ text.step2.radiusHost }}</label>
                      <input
                        id="wzRadHost"
                        type="text"
                        class="form-input"
                        [(ngModel)]="authForm.radius_host"
                        placeholder="10.0.0.254"
                      />
                    </div>
                    <div class="field-group">
                      <label for="wzRadPort">{{ text.step2.radiusPort }}</label>
                      <input
                        id="wzRadPort"
                        type="number"
                        class="form-input font-mono"
                        [(ngModel)]="authForm.radius_port"
                      />
                    </div>
                  </div>
                  <div class="field-group">
                    <label for="wzRadSecret">{{ text.step2.radiusSecret }}</label>
                    <input
                      id="wzRadSecret"
                      type="password"
                      class="form-input"
                      [(ngModel)]="authForm.radius_secret"
                      [placeholder]="text.step2.radiusSecretPlaceholder"
                    />
                  </div>
                </div>
              }

              <div class="test-box">
                <div class="test-box-title">{{ text.step2.testTitle }}</div>
                <div class="field-row">
                  <input
                    type="text"
                    class="form-input"
                    [placeholder]="text.step2.testUsername"
                    [(ngModel)]="authTestUser"
                  />
                  <input
                    type="password"
                    class="form-input"
                    [placeholder]="text.step2.testPassword"
                    [(ngModel)]="authTestPass"
                  />
                  <button
                    type="button"
                    class="btn btn-secondary"
                    (click)="testAuth()"
                    [disabled]="testingAuth() || !authTestUser"
                  >
                    {{ testingAuth() ? text.step2.testRunning : text.step2.testRun }}
                  </button>
                </div>
                @if (authTestResult()) {
                  <div class="test-feedback" [class.success]="authTestResult()!.success">
                    {{ authTestResult()!.message }}
                  </div>
                }
              </div>
            </section>
          }

          @case (3) {
            <section class="step-content">
              <h3 class="step-title">{{ text.step3.title }}</h3>
              <p class="step-desc">
                {{ text.step3.description }}
              </p>

              <div class="form-stack">
                <div class="field-row">
                  <div class="field-group flex-2">
                    <label for="wzSmtpHost">{{ text.step3.host }}</label>
                    <input
                      id="wzSmtpHost"
                      type="text"
                      class="form-input"
                      [(ngModel)]="mailForm.smtp_host"
                      placeholder="smtp.example.com"
                    />
                  </div>
                  <div class="field-group flex-1">
                    <label for="wzSmtpPort">{{ text.step3.port }}</label>
                    <input
                      id="wzSmtpPort"
                      type="number"
                      class="form-input font-mono"
                      [(ngModel)]="mailForm.smtp_port"
                    />
                  </div>
                  <div class="field-group flex-1">
                    <label for="wzSmtpTls">{{ text.step3.tlsMode }}</label>
                    <select id="wzSmtpTls" class="form-input" [(ngModel)]="mailForm.smtp_tls_mode">
                      <option value="starttls">STARTTLS</option>
                      <option value="smtps">SMTPS</option>
                      <option value="none">{{ text.step3.tlsNone }}</option>
                    </select>
                  </div>
                </div>

                <div class="field-row">
                  <div class="field-group">
                    <label for="wzSmtpUser">{{ text.step3.username }}</label>
                    <input
                      id="wzSmtpUser"
                      type="text"
                      class="form-input"
                      [(ngModel)]="mailForm.smtp_username"
                    />
                  </div>
                  <div class="field-group">
                    <label for="wzSmtpPass">{{ text.step3.password }}</label>
                    <input
                      id="wzSmtpPass"
                      type="password"
                      class="form-input"
                      [(ngModel)]="mailPassword"
                      [placeholder]="text.placeholders.password"
                    />
                  </div>
                </div>

                <div class="field-group">
                  <label for="wzSender">{{ text.step3.sender }}</label>
                  <input
                    id="wzSender"
                    type="email"
                    class="form-input"
                    [(ngModel)]="mailForm.sender_address"
                    placeholder="ipmgmt@example.com"
                  />
                </div>

                <div class="field-group">
                  <label for="wzAdminRcpts">{{ text.step3.adminRecipients }}</label>
                  <input
                    id="wzAdminRcpts"
                    type="text"
                    class="form-input"
                    [(ngModel)]="adminRcptsText"
                    placeholder="admin@example.com, netops@example.com"
                  />
                </div>
              </div>

              <div class="test-box">
                <div class="test-box-title">{{ text.step3.testTitle }}</div>
                <div class="field-row">
                  <input
                    type="email"
                    class="form-input"
                    [placeholder]="text.step3.testRecipient"
                    [(ngModel)]="testMailRecipient"
                  />
                  <button
                    type="button"
                    class="btn btn-secondary"
                    (click)="testMail()"
                    [disabled]="testingMail() || !testMailRecipient"
                  >
                    {{ testingMail() ? text.step3.testRunning : text.step3.testRun }}
                  </button>
                </div>
                @if (mailTestResult()) {
                  <div class="test-feedback" [class.success]="mailTestResult()!.success">
                    {{ mailTestResult()!.message }}
                  </div>
                }
              </div>
            </section>
          }

          @case (4) {
            <section class="step-content">
              <h3 class="step-title">{{ text.step4.title }}</h3>
              <p class="step-desc">
                {{ text.step4.description }}
              </p>

              <div class="form-stack">
                <div class="field-row">
                  <div class="field-group flex-2">
                    <label for="wzSwName">{{ text.step4.name }}</label>
                    <input
                      id="wzSwName"
                      type="text"
                      class="form-input"
                      [(ngModel)]="switchForm.name"
                      placeholder="Core-Switch-01"
                    />
                  </div>
                  <div class="field-group flex-2">
                    <label for="wzSwIP">{{ text.step4.mgmtIp }}</label>
                    <input
                      id="wzSwIP"
                      type="text"
                      class="form-input font-mono"
                      [(ngModel)]="switchForm.mgmt_ip"
                      placeholder="10.0.0.1"
                    />
                  </div>
                  <div class="field-group flex-1">
                    <label for="wzSwPlatform">{{ text.step4.platform }}</label>
                    <select id="wzSwPlatform" class="form-input" [(ngModel)]="switchForm.platform">
                      <option value="ios-xe">IOS-XE</option>
                      <option value="nx-os">NX-OS</option>
                    </select>
                  </div>
                </div>

                <div class="field-row">
                  <div class="field-group flex-1">
                    <label for="wzSnmpVer">{{ text.step4.snmpVersion }}</label>
                    <select id="wzSnmpVer" class="form-input" [(ngModel)]="switchForm.snmp_version">
                      <option value="v2c">SNMP v2c</option>
                      <option value="v3">SNMP v3</option>
                    </select>
                  </div>
                  @if (switchForm.snmp_version === 'v2c') {
                    <div class="field-group flex-2">
                      <label for="wzCommunity">{{ text.step4.community }}</label>
                      <input
                        id="wzCommunity"
                        type="password"
                        class="form-input"
                        [(ngModel)]="switchForm.community"
                        [placeholder]="text.step4.communityPlaceholder"
                      />
                    </div>
                  } @else {
                    <div class="field-group flex-2">
                      <label for="wzV3User">{{ text.step4.v3Username }}</label>
                      <input
                        id="wzV3User"
                        type="text"
                        class="form-input"
                        [(ngModel)]="switchForm.v3_user"
                      />
                    </div>
                  }
                </div>

                @if (switchForm.snmp_version === 'v3') {
                  <div class="field-row">
                    <div class="field-group">
                      <label for="wzAuthProto">{{ text.step4.authProtocol }}</label>
                      <select
                        id="wzAuthProto"
                        class="form-input"
                        [(ngModel)]="switchForm.v3_auth_proto"
                      >
                        <option value="sha">SHA</option>
                        <option value="sha256">SHA-256</option>
                        <option value="md5">MD5</option>
                      </select>
                    </div>
                    <div class="field-group">
                      <label for="wzAuthPass">{{ text.step4.authPassword }}</label>
                      <input
                        id="wzAuthPass"
                        type="password"
                        class="form-input"
                        [(ngModel)]="switchForm.v3_auth_pass"
                      />
                    </div>
                    <div class="field-group">
                      <label for="wzPrivProto">{{ text.step4.privProtocol }}</label>
                      <select
                        id="wzPrivProto"
                        class="form-input"
                        [(ngModel)]="switchForm.v3_priv_proto"
                      >
                        <option value="aes">AES-128</option>
                        <option value="des">DES</option>
                      </select>
                    </div>
                    <div class="field-group">
                      <label for="wzPrivPass">{{ text.step4.privPassword }}</label>
                      <input
                        id="wzPrivPass"
                        type="password"
                        class="form-input"
                        [(ngModel)]="switchForm.v3_priv_pass"
                      />
                    </div>
                  </div>
                }
              </div>

              <div class="test-box">
                <div class="test-box-title">{{ text.step4.testTitle }}</div>
                <button
                  type="button"
                  class="btn btn-secondary"
                  (click)="testReadSwitch()"
                  [disabled]="testingSwitch()"
                >
                  <svg lucidePlay [size]="14"></svg>
                  <span>{{ testingSwitch() ? text.step4.testRunning : text.step4.testRun }}</span>
                </button>
                @if (switchTestResult()) {
                  <div class="test-feedback" [class.success]="switchTestResult()!.success">
                    {{ switchTestResult()!.message }}
                  </div>
                }
              </div>
            </section>
          }

          @case (5) {
            <section class="step-content">
              <h3 class="step-title">{{ text.step5.title }}</h3>
              <p class="step-desc">
                {{ text.step5.description }}
              </p>

              @if (step4Skipped()) {
                <div class="info-banner">
                  <strong>{{ text.step5.skippedNoticeTitle }}</strong>
                  {{ text.step5.skippedNotice }}
                </div>
              } @else {
                <div class="sync-actions-row">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    (click)="triggerSyncAndReloadSubnets()"
                    [disabled]="syncing()"
                  >
                    <svg lucideRefreshCw [size]="14"></svg>
                    <span>{{ syncing() ? text.step5.syncRunning : text.step5.syncRun }}</span>
                  </button>
                </div>

                @if (subnets().length === 0) {
                  <p class="muted-text">
                    {{ text.step5.noSubnets }}
                  </p>
                } @else {
                  <div class="subnets-list">
                    @for (sub of subnets(); track sub.id) {
                      <div class="subnet-row card">
                        <div class="subnet-info">
                          <span class="font-mono subnet-cidr">{{ sub.cidr }}</span>
                          <span class="muted-text font-mono">{{
                            text.step5.subnetCounts(sub.ip_count, sub.online_count)
                          }}</span>
                        </div>
                        <div class="subnet-controls">
                          <select
                            class="form-input form-input-sm"
                            [ngModel]="sub.state"
                            (ngModelChange)="changeSubnetState(sub.id, $event)"
                          >
                            <option value="pending">{{ text.step5.stateOptions.pending }}</option>
                            <option value="managed">{{ text.step5.stateOptions.managed }}</option>
                            <option value="unmanaged">
                              {{ text.step5.stateOptions.unmanaged }}
                            </option>
                          </select>
                        </div>
                      </div>
                    }
                  </div>
                }
              }
            </section>
          }
        }
      </div>

      <!-- 操作按鈕列 -->
      <footer class="wizard-footer">
        <button type="button" class="btn btn-ghost" (click)="close()">
          {{ text.footer.leave }}
        </button>

        <div class="footer-actions">
          @if (currentStep() < 5) {
            <button type="button" class="btn btn-secondary" (click)="skipCurrentStep()">
              {{ text.footer.skip }}
            </button>
            <button
              type="button"
              class="btn btn-primary"
              (click)="saveAndContinue()"
              [disabled]="savingStep()"
            >
              {{ savingStep() ? text.footer.saving : text.footer.saveAndContinue }}
            </button>
          } @else {
            <button type="button" class="btn btn-primary" (click)="finishWizard()">
              {{ text.footer.finish }}
            </button>
          }
        </div>
      </footer>
    </div>
  `,
  styles: [
    `
      .wizard-modal {
        background-color: var(--color-surface);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-lg);
        width: 100vw;
        max-width: 800px;
        box-shadow: var(--shadow-dialog);
        display: flex;
        flex-direction: column;
        max-height: 90vh;
      }
      .wizard-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 18px 24px;
        border-bottom: 1px solid var(--color-border-default);
      }
      .wizard-header-title {
        display: flex;
        align-items: center;
        gap: 10px;
        h2 {
          margin: 0;
          font-size: var(--font-size-dialog-title);
          font-weight: 600;
        }
      }
      .close-btn {
        background: transparent;
        border: none;
        cursor: pointer;
        color: var(--color-text-muted);
        &:hover {
          color: var(--color-text-primary);
        }
      }
      .steps-nav {
        display: flex;
        border-bottom: 1px solid var(--color-border-default);
        background-color: var(--sidebar-bg);
      }
      .step-item {
        flex: 1;
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 12px 14px;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
        border-bottom: 2px solid transparent;
        &.active {
          color: var(--color-text-primary);
          font-weight: 600;
          border-bottom-color: var(--color-accent);
          background-color: var(--color-surface);
        }
        &.completed {
          color: var(--color-text-secondary);
        }
      }
      .step-num {
        width: 22px;
        height: 22px;
        border-radius: var(--radius-full);
        background-color: var(--color-border-default);
        display: flex;
        align-items: center;
        justify-content: center;
        font-size: 11px;
        font-weight: 600;
      }
      .step-item.active .step-num {
        background-color: var(--color-accent);
        color: var(--color-text-on-accent);
      }
      .step-item.completed .step-num {
        background-color: var(--color-success);
        color: var(--color-text-on-success);
      }
      .wizard-body {
        padding: 24px;
        overflow-y: auto;
        flex: 1;
      }
      .step-title {
        margin: 0 0 4px 0;
        font-size: var(--font-size-section);
        font-weight: 600;
      }
      .step-desc {
        margin: 0 0 20px 0;
        font-size: var(--font-size-meta);
        color: var(--color-text-muted);
      }
      .form-stack {
        display: flex;
        flex-direction: column;
        gap: 14px;
      }
      .check-row {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: var(--font-size-meta);
        color: var(--color-text-secondary);
        cursor: pointer;
      }
      .field-row {
        display: flex;
        gap: 12px;
        align-items: flex-start;
      }
      .field-group {
        display: flex;
        flex-direction: column;
        gap: 6px;
        flex: 1;
        &.flex-1 {
          flex: 1;
        }
        &.flex-2 {
          flex: 2;
        }
        label {
          font-size: var(--font-size-meta);
          font-weight: 500;
        }
      }
      .field-help {
        font-size: var(--font-size-caption);
        color: var(--color-text-muted);
      }
      .source-pills {
        display: flex;
        gap: 8px;
        margin-bottom: 16px;
      }
      .pill-btn {
        padding: 6px 14px;
        border-radius: var(--radius-full);
        border: 1px solid var(--color-border-default);
        background: var(--color-surface);
        font-size: var(--font-size-meta);
        cursor: pointer;
        &.active {
          background-color: var(--color-accent);
          color: var(--color-text-on-accent);
          border-color: var(--color-accent);
        }
      }
      .test-box {
        margin-top: 20px;
        padding: 16px;
        background-color: var(--sidebar-bg);
        border: 1px solid var(--color-border-default);
        border-radius: var(--radius-md);
      }
      .test-box-title {
        font-size: var(--font-size-meta);
        font-weight: 600;
        margin-bottom: 10px;
      }
      .test-feedback {
        margin-top: 10px;
        padding: 8px 12px;
        border-radius: var(--radius-sm);
        font-size: var(--font-size-meta);
        background-color: var(--color-danger-subtle);
        color: var(--color-danger-text);
        border: 1px solid var(--color-danger-border);
        &.success {
          background-color: var(--color-success-subtle);
          color: var(--color-success-text);
          border-color: var(--color-success-border);
        }
      }
      .info-banner {
        padding: 14px 16px;
        background-color: var(--color-warning-subtle);
        border: 1px solid var(--color-warning-border);
        color: var(--color-warning-text);
        border-radius: var(--radius-md);
        font-size: var(--font-size-meta);
        line-height: 1.5;
      }
      .subnets-list {
        display: flex;
        flex-direction: column;
        gap: 10px;
        margin-top: 14px;
      }
      .subnet-row {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 12px 16px;
      }
      .subnet-cidr {
        font-weight: 600;
        margin-right: 12px;
      }
      .form-input-sm {
        padding: 4px 8px;
        font-size: var(--font-size-meta);
      }
      .wizard-footer {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 16px 24px;
        border-top: 1px solid var(--color-border-default);
        background-color: var(--sidebar-bg);
      }
      .footer-actions {
        display: flex;
        gap: 10px;
      }
    `,
  ],
})
export class SetupWizardDialogComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly dialogRef = inject(DialogRef);
  private readonly toast = inject(ToastService);

  protected readonly text = SETUP_WIZARD_TEXT;
  readonly steps = SETUP_WIZARD_TEXT.steps;

  protected readonly currentStep = signal(1);
  protected readonly savingStep = signal(false);
  protected readonly step4Skipped = signal(false);

  // Step 1
  protected campusForm = { campus_name: 'Würzburg', timezone: 'Asia/Taipei', public_base_url: '' };

  // Step 2
  protected authForm: AuthFormData = {
    active_source: 'ad',
    ad_url: '',
    ad_use_tls: true,
    ad_bind_dn: '',
    ad_bind_password: '',
    ad_base_dn: '',
    radius_host: '',
    radius_port: 1812,
    radius_secret: '',
  };
  protected authTestUser = '';
  protected authTestPass = '';
  protected readonly testingAuth = signal(false);
  protected readonly authTestResult = signal<{ success: boolean; message: string } | null>(null);

  // Step 3
  protected mailForm: MailFormData = {
    smtp_host: '',
    smtp_port: 587,
    smtp_tls_mode: 'starttls',
    smtp_username: '',
    sender_address: '',
    admin_recipients: [],
  };
  protected mailPassword = '';
  protected adminRcptsText = '';
  protected testMailRecipient = '';
  protected readonly testingMail = signal(false);
  protected readonly mailTestResult = signal<{ success: boolean; message: string } | null>(null);

  // Step 4
  protected switchForm: SwitchFormData = {
    name: '',
    mgmt_ip: '',
    platform: 'ios-xe',
    snmp_version: 'v2c',
    community: 'public',
    v3_user: '',
    v3_auth_proto: 'sha',
    v3_auth_pass: '',
    v3_priv_proto: 'aes',
    v3_priv_pass: '',
  };
  protected readonly switchCreatedId = signal<number | null>(null);
  protected readonly testingSwitch = signal(false);
  protected readonly switchTestResult = signal<{ success: boolean; message: string } | null>(null);

  // Step 5
  protected readonly subnets = signal<Subnet[]>([]);
  protected readonly syncing = signal(false);

  ngOnInit(): void {
    // 讀取精靈當前進度與園區設定
    this.http
      .get<{ current_step: number; completed: boolean }>('/api/v1/settings/wizard')
      .subscribe({
        next: (res) => {
          if (res.current_step >= 1 && res.current_step <= 5) {
            this.currentStep.set(res.current_step);
          }
        },
        error: () => {},
      });

    this.http.get<components['schemas']['CampusSettings']>('/api/v1/settings/campus').subscribe({
      // 公開網址：已設定就沿用，儲存時原樣送回、不會被清除；尚未設定時先帶入目前瀏覽器使用的網址供確認
      next: (res) =>
        (this.campusForm = {
          campus_name: res.campus_name,
          timezone: res.timezone,
          public_base_url: res.public_base_url || window.location.origin,
        }),
      error: () => {},
    });
  }

  saveAndContinue(): void {
    this.savingStep.set(true);
    const step = this.currentStep();

    if (step === 1) {
      const publicBaseUrl = this.campusForm.public_base_url.trim();
      if (!isValidPublicBaseUrl(publicBaseUrl)) {
        this.savingStep.set(false);
        this.toast.show(this.text.toast.saveFailed, 'danger', this.text.step1.publicBaseUrlInvalid);
        return;
      }
      this.http
        .put(
          '/api/v1/settings/campus',
          {
            campus_name: this.campusForm.campus_name,
            timezone: this.campusForm.timezone,
            public_base_url: publicBaseUrl,
          },
          { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
        )
        .subscribe({
          next: () => {
            this.advanceStep(2);
          },
          error: (err) => {
            this.savingStep.set(false);
            this.toast.show(
              this.text.toast.saveFailed,
              'danger',
              err?.error?.message ?? this.text.toast.checkFieldsCorrect,
            );
          },
        });
    } else if (step === 2) {
      this.saveAuthSource().subscribe({
        next: () => this.advanceStep(3),
        error: (err) => this.reportSaveError(err, this.text.toast.checkFields),
      });
    } else if (step === 3) {
      this.saveMailSettings().subscribe({
        next: () => this.advanceStep(4),
        error: (err) => this.reportSaveError(err, this.text.toast.checkFields),
      });
    } else if (step === 4) {
      if (!this.switchForm.name || !this.switchForm.mgmt_ip) {
        this.savingStep.set(false);
        this.toast.show(this.text.toast.nameAndIpRequired, 'danger');
        return;
      }
      this.saveSwitch().subscribe({
        next: () => {
          this.step4Skipped.set(false);
          this.advanceStep(5);
          // 進入步驟 5 自動觸發一次同步讀取網段
          this.triggerSyncAndReloadSubnets();
        },
        error: (err) => {
          this.savingStep.set(false);
          this.toast.show(this.text.toast.switchCreateFailed, 'danger', err?.error?.message);
        },
      });
    }
  }

  private reportSaveError(err: any, fallback: string): void {
    this.savingStep.set(false);
    this.toast.show(this.text.toast.saveFailed, 'danger', err?.error?.message ?? fallback);
  }

  // 各步驟的「測試」按鈕測的是後端已儲存的設定，所以一律先把畫面上的內容存起來再測試；
  // 否則全新安裝時後端還沒有任何設定，測試必定失敗。

  private saveAuthSource(): Observable<unknown> {
    return this.http.put('/api/v1/settings/auth-source', this.authForm, {
      headers: { 'X-Requested-With': 'XMLHttpRequest' },
    });
  }

  private saveMailSettings(): Observable<unknown> {
    const rcpts = this.adminRcptsText
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
    const payload: Record<string, unknown> = {
      ...this.mailForm,
      admin_recipients: rcpts,
    };
    if (this.mailPassword) {
      payload['smtp_password'] = this.mailPassword;
    }
    return this.http.put('/api/v1/settings/mail', payload, {
      headers: { 'X-Requested-With': 'XMLHttpRequest' },
    });
  }

  /** 新增交換器；已在本步驟新增過（先按了測試讀取）則改為更新，避免重複建立。回傳交換器資料。 */
  private saveSwitch(): Observable<SwitchResponse> {
    const headers = { 'X-Requested-With': 'XMLHttpRequest' };
    const existingId = this.switchCreatedId();
    const request = existingId
      ? this.http.put<SwitchResponse>(`/api/v1/switches/${existingId}`, this.switchForm, {
          headers,
        })
      : this.http.post<SwitchResponse>('/api/v1/switches', this.switchForm, { headers });
    return request.pipe(tap((sw) => this.switchCreatedId.set(sw.id)));
  }

  skipCurrentStep(): void {
    if (this.currentStep() === 4) {
      this.step4Skipped.set(true);
    }
    const next = this.currentStep() + 1;
    this.advanceStep(next);
  }

  private advanceStep(next: number): void {
    this.currentStep.set(next);
    this.savingStep.set(false);
    // 儲存進度至後端
    this.http
      .put(
        '/api/v1/settings/wizard',
        { current_step: next, completed: false },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({ error: () => {} });
  }

  testAuth(): void {
    if (!this.authTestUser) return;
    this.testingAuth.set(true);
    this.authTestResult.set(null);
    this.saveAuthSource()
      .pipe(
        switchMap(() =>
          this.http.post<{ success: boolean; message: string }>(
            '/api/v1/settings/auth-source/test',
            { username: this.authTestUser, password: this.authTestPass },
            { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
          ),
        ),
      )
      .subscribe({
        next: (res) => {
          this.testingAuth.set(false);
          this.authTestResult.set(res);
        },
        error: (err) => {
          this.testingAuth.set(false);
          this.authTestResult.set({
            success: false,
            message: err?.error?.message ?? this.text.step2.testFailed,
          });
        },
      });
  }

  testMail(): void {
    if (!this.testMailRecipient) return;
    this.testingMail.set(true);
    this.mailTestResult.set(null);
    this.saveMailSettings()
      .pipe(
        switchMap(() =>
          this.http.post<{ success: boolean; message: string }>(
            '/api/v1/settings/mail/test',
            { recipient: this.testMailRecipient },
            { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
          ),
        ),
      )
      .subscribe({
        next: (res) => {
          this.testingMail.set(false);
          this.mailTestResult.set(res);
        },
        error: (err) => {
          this.testingMail.set(false);
          this.mailTestResult.set({
            success: false,
            message: err?.error?.message ?? this.text.step3.testFailed,
          });
        },
      });
  }

  testReadSwitch(): void {
    if (!this.switchForm.name || !this.switchForm.mgmt_ip) {
      this.toast.show(this.text.toast.nameAndIpRequired, 'danger');
      return;
    }
    this.testingSwitch.set(true);
    this.switchTestResult.set(null);
    this.saveSwitch()
      .pipe(
        switchMap((sw) => {
          // 新交換器一定帶一筆預設 VRF 讀取設定，測試讀取就讀它
          const vrfId = sw.vrf_configs[0]?.id;
          return this.http.post<TestReadResult>(
            `/api/v1/switches/${sw.id}/vrfs/${vrfId}/test-read`,
            {},
            { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
          );
        }),
      )
      .subscribe({
        next: (tr) => {
          this.testingSwitch.set(false);
          this.switchTestResult.set({
            success: tr.success,
            message: tr.success
              ? this.text.step4.testSucceeded(tr.arp_count, tr.subnet_count)
              : this.text.step4.testReadFailed(tr.error_class ?? '', tr.error_summary ?? ''),
          });
        },
        error: (err) => {
          this.testingSwitch.set(false);
          this.switchTestResult.set({
            success: false,
            message: err?.error?.message ?? this.text.step4.testRequestFailed,
          });
        },
      });
  }

  triggerSyncAndReloadSubnets(): void {
    this.syncing.set(true);
    this.http
      .post('/api/v1/sync/trigger', {}, { headers: { 'X-Requested-With': 'XMLHttpRequest' } })
      .pipe(
        // 已有同步在執行（409）也一樣等它結束
        catchError(() => of(null)),
        switchMap(() => this.waitForSyncToFinish()),
      )
      .subscribe({
        next: () => this.loadSubnets(),
        error: () => this.loadSubnets(),
      });
  }

  /**
   * 等目前這一輪同步結束。同步在後端的背景執行，觸發後馬上讀網段清單會讀到同步前的內容。
   * 每秒查一次同步狀態，最多等 3 分鐘（後端一輪的上限）。
   */
  private waitForSyncToFinish(): Observable<SyncStatus> {
    const check = () => this.http.get<SyncStatus>('/api/v1/sync/status');
    return check().pipe(
      expand((st, attempt) =>
        st.in_progress && attempt < 180 ? timer(1000).pipe(switchMap(check)) : EMPTY,
      ),
      last(),
    );
  }

  loadSubnets(): void {
    this.http.get<{ items: Subnet[] }>('/api/v1/subnets').subscribe({
      next: (res) => {
        this.subnets.set(res.items ?? []);
        this.syncing.set(false);
      },
      error: () => this.syncing.set(false),
    });
  }

  changeSubnetState(id: number, newState: 'managed' | 'unmanaged' | 'pending'): void {
    this.http
      .put(
        `/api/v1/subnets/${id}/state`,
        { state: newState, exclude_reason: newState === 'unmanaged' ? 'dhcp' : undefined },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: () => {
          this.toast.show(this.text.toast.subnetStateUpdated, 'success');
          this.loadSubnets();
        },
        error: (err) =>
          this.toast.show(this.text.toast.subnetStateUpdateFailed, 'danger', err?.error?.message),
      });
  }

  finishWizard(): void {
    this.http
      .put(
        '/api/v1/settings/wizard',
        { current_step: 5, completed: true },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: () => {
          this.toast.show(this.text.toast.wizardCompleted, 'success');
          this.dialogRef.close(true);
        },
        error: () => this.dialogRef.close(true),
      });
  }

  close(): void {
    this.dialogRef.close(false);
  }
}
