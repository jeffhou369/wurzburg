import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';
import { DialogRef } from '@angular/cdk/dialog';
import { LucideCheck, LucideX, LucideWand2, LucidePlay, LucideRefreshCw } from '@lucide/angular';
import { ToastService } from '../../shared/components/toast.service';
import type { components } from '../../core/api.types';

type Subnet = components['schemas']['Subnet'];

interface AuthFormData {
  active_source: 'ad' | 'radius';
  ad_url?: string;
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
          <h2>初始設定精靈</h2>
        </div>
        <button
          type="button"
          class="close-btn"
          (click)="close()"
          aria-label="離開精靈（進度自動保存）"
        >
          <svg lucideX [size]="20"></svg>
        </button>
      </header>

      <!-- 步驟指示列 -->
      <nav class="steps-nav" aria-label="設定步驟">
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
              <h3 class="step-title">步驟 1：園區基本資訊</h3>
              <p class="step-desc">
                設定系統識別名稱與顯示時區。名稱用於側欄與信件，時區用於全系統介面顯示。
              </p>

              <div class="form-stack">
                <div class="field-group">
                  <label for="wzCampus">園區顯示名稱 *</label>
                  <input
                    id="wzCampus"
                    type="text"
                    class="form-input"
                    [(ngModel)]="campusForm.campus_name"
                    placeholder="Würzburg"
                  />
                </div>
                <div class="field-group">
                  <label for="wzTz">顯示時區 (IANA Timezone) *</label>
                  <input
                    id="wzTz"
                    type="text"
                    class="form-input font-mono"
                    [(ngModel)]="campusForm.timezone"
                    placeholder="Asia/Taipei"
                  />
                  <span class="field-help"
                    >例如 Asia/Taipei、UTC 或 America/New_York。內部存儲一律使用 UTC。</span
                  >
                </div>
              </div>
            </section>
          }

          @case (2) {
            <section class="step-content">
              <h3 class="step-title">步驟 2：一般使用者驗證來源</h3>
              <p class="step-desc">
                選擇並設定組織驗證來源（AD 或 RADIUS）。使用者登入後登記自己負責的位址。
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
                    <label for="wzAdUrl">AD 伺服器網址 *</label>
                    <input
                      id="wzAdUrl"
                      type="text"
                      class="form-input"
                      [(ngModel)]="authForm.ad_url"
                      placeholder="ldaps://ad.example.com:636"
                    />
                  </div>
                  <div class="field-row">
                    <div class="field-group">
                      <label for="wzAdBindDn">查詢帳號 DN</label>
                      <input
                        id="wzAdBindDn"
                        type="text"
                        class="form-input"
                        [(ngModel)]="authForm.ad_bind_dn"
                        placeholder="cn=bind,dc=example,dc=com"
                      />
                    </div>
                    <div class="field-group">
                      <label for="wzAdBindPass">查詢密碼</label>
                      <input
                        id="wzAdBindPass"
                        type="password"
                        class="form-input"
                        [(ngModel)]="authForm.ad_bind_password"
                        placeholder="密碼"
                      />
                    </div>
                  </div>
                  <div class="field-group">
                    <label for="wzAdBaseDn">搜尋 Base DN *</label>
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
                      <label for="wzRadHost">RADIUS 主機 *</label>
                      <input
                        id="wzRadHost"
                        type="text"
                        class="form-input"
                        [(ngModel)]="authForm.radius_host"
                        placeholder="10.0.0.254"
                      />
                    </div>
                    <div class="field-group">
                      <label for="wzRadPort">連接埠</label>
                      <input
                        id="wzRadPort"
                        type="number"
                        class="form-input font-mono"
                        [(ngModel)]="authForm.radius_port"
                      />
                    </div>
                  </div>
                  <div class="field-group">
                    <label for="wzRadSecret">共用密碼 (Shared Secret) *</label>
                    <input
                      id="wzRadSecret"
                      type="password"
                      class="form-input"
                      [(ngModel)]="authForm.radius_secret"
                      placeholder="共用金鑰"
                    />
                  </div>
                </div>
              }

              <div class="test-box">
                <div class="test-box-title">連線測試（以一組測試帳密驗證登入）</div>
                <div class="field-row">
                  <input
                    type="text"
                    class="form-input"
                    placeholder="測試帳號"
                    [(ngModel)]="authTestUser"
                  />
                  <input
                    type="password"
                    class="form-input"
                    placeholder="測試密碼"
                    [(ngModel)]="authTestPass"
                  />
                  <button
                    type="button"
                    class="btn btn-secondary"
                    (click)="testAuth()"
                    [disabled]="testingAuth() || !authTestUser"
                  >
                    {{ testingAuth() ? '驗證中...' : '測試登入' }}
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
              <h3 class="step-title">步驟 3：郵件通知設定 (SMTP)</h3>
              <p class="step-desc">
                設定系統發送未登記位址彙整、每週總表與到期催認信件的 SMTP 伺服器與管理員收件清單。
              </p>

              <div class="form-stack">
                <div class="field-row">
                  <div class="field-group" style="flex: 2;">
                    <label for="wzSmtpHost">SMTP 伺服器主機 *</label>
                    <input
                      id="wzSmtpHost"
                      type="text"
                      class="form-input"
                      [(ngModel)]="mailForm.smtp_host"
                      placeholder="smtp.example.com"
                    />
                  </div>
                  <div class="field-group" style="flex: 1;">
                    <label for="wzSmtpPort">埠號</label>
                    <input
                      id="wzSmtpPort"
                      type="number"
                      class="form-input font-mono"
                      [(ngModel)]="mailForm.smtp_port"
                    />
                  </div>
                  <div class="field-group" style="flex: 1;">
                    <label for="wzSmtpTls">加密模式</label>
                    <select id="wzSmtpTls" class="form-input" [(ngModel)]="mailForm.smtp_tls_mode">
                      <option value="starttls">STARTTLS</option>
                      <option value="smtps">SMTPS</option>
                      <option value="none">無加密</option>
                    </select>
                  </div>
                </div>

                <div class="field-row">
                  <div class="field-group">
                    <label for="wzSmtpUser">認證帳號</label>
                    <input
                      id="wzSmtpUser"
                      type="text"
                      class="form-input"
                      [(ngModel)]="mailForm.smtp_username"
                    />
                  </div>
                  <div class="field-group">
                    <label for="wzSmtpPass">認證密碼</label>
                    <input
                      id="wzSmtpPass"
                      type="password"
                      class="form-input"
                      [(ngModel)]="mailPassword"
                      placeholder="密碼"
                    />
                  </div>
                </div>

                <div class="field-group">
                  <label for="wzSender">寄件人信箱 (From) *</label>
                  <input
                    id="wzSender"
                    type="email"
                    class="form-input"
                    [(ngModel)]="mailForm.sender_address"
                    placeholder="ipmgmt@example.com"
                  />
                </div>

                <div class="field-group">
                  <label for="wzAdminRcpts">系統管理員收件清單（多筆請用逗號隔開）*</label>
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
                <div class="test-box-title">寄送測試信</div>
                <div class="field-row">
                  <input
                    type="email"
                    class="form-input"
                    placeholder="收件信箱"
                    [(ngModel)]="testMailRecipient"
                  />
                  <button
                    type="button"
                    class="btn btn-secondary"
                    (click)="testMail()"
                    [disabled]="testingMail() || !testMailRecipient"
                  >
                    {{ testingMail() ? '寄送中...' : '寄出測試信' }}
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
              <h3 class="step-title">步驟 4：新增第一台 ARP 來源交換器</h3>
              <p class="step-desc">
                新增擔任 L3 閘道的 Cisco 交換器（IOS-XE 或 NX-OS），系統將定時透過 SNMP 讀取其 ARP
                表與介面位址。
              </p>

              <div class="form-stack">
                <div class="field-row">
                  <div class="field-group" style="flex: 2;">
                    <label for="wzSwName">交換器名稱 *</label>
                    <input
                      id="wzSwName"
                      type="text"
                      class="form-input"
                      [(ngModel)]="switchForm.name"
                      placeholder="Core-Switch-01"
                    />
                  </div>
                  <div class="field-group" style="flex: 2;">
                    <label for="wzSwIP">管理 IP 位址 *</label>
                    <input
                      id="wzSwIP"
                      type="text"
                      class="form-input font-mono"
                      [(ngModel)]="switchForm.mgmt_ip"
                      placeholder="10.0.0.1"
                    />
                  </div>
                  <div class="field-group" style="flex: 1;">
                    <label for="wzSwPlatform">作業平台</label>
                    <select id="wzSwPlatform" class="form-input" [(ngModel)]="switchForm.platform">
                      <option value="ios-xe">IOS-XE</option>
                      <option value="nx-os">NX-OS</option>
                    </select>
                  </div>
                </div>

                <div class="field-row">
                  <div class="field-group" style="flex: 1;">
                    <label for="wzSnmpVer">SNMP 版本</label>
                    <select id="wzSnmpVer" class="form-input" [(ngModel)]="switchForm.snmp_version">
                      <option value="v2c">SNMP v2c</option>
                      <option value="v3">SNMP v3</option>
                    </select>
                  </div>
                  @if (switchForm.snmp_version === 'v2c') {
                    <div class="field-group" style="flex: 2;">
                      <label for="wzCommunity">Community 字串 *</label>
                      <input
                        id="wzCommunity"
                        type="password"
                        class="form-input"
                        [(ngModel)]="switchForm.community"
                        placeholder="例如 public"
                      />
                    </div>
                  } @else {
                    <div class="field-group" style="flex: 2;">
                      <label for="wzV3User">v3 帳號 (Username) *</label>
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
                      <label for="wzAuthProto">認證協定</label>
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
                      <label for="wzAuthPass">認證密碼</label>
                      <input
                        id="wzAuthPass"
                        type="password"
                        class="form-input"
                        [(ngModel)]="switchForm.v3_auth_pass"
                      />
                    </div>
                    <div class="field-group">
                      <label for="wzPrivProto">加密協定</label>
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
                      <label for="wzPrivPass">加密密碼</label>
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

              @if (switchCreatedId()) {
                <div class="test-box">
                  <div class="test-box-title">測試讀取（確認 SNMP 憑證與連線）</div>
                  <button
                    type="button"
                    class="btn btn-secondary"
                    (click)="testReadSwitch()"
                    [disabled]="testingSwitch()"
                  >
                    <svg lucidePlay [size]="14"></svg>
                    <span>{{ testingSwitch() ? '讀取中...' : '立即測試讀取' }}</span>
                  </button>
                  @if (switchTestResult()) {
                    <div class="test-feedback" [class.success]="switchTestResult()!.success">
                      {{ switchTestResult()!.message }}
                    </div>
                  }
                </div>
              }
            </section>
          }

          @case (5) {
            <section class="step-content">
              <h3 class="step-title">步驟 5：檢視並設定納管網段</h3>
              <p class="step-desc">
                檢視由交換器介面探索之網段，將需要追蹤的網段設為「納管」，非管理網段（如 DHCP /
                訪客無線）設為「不納管」。
              </p>

              @if (step4Skipped()) {
                <div class="info-banner">
                  <strong>提醒：</strong>
                  因為步驟 4
                  略過了交換器設定，系統目前尚未探索到任何交換器介面位址。您可以直接完成精靈，稍後再於系統中新增。
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
                    <span>{{ syncing() ? '同步讀取中...' : '觸發同步探索網段' }}</span>
                  </button>
                </div>

                @if (subnets().length === 0) {
                  <p class="muted-text">
                    目前尚未發現任何網段。您可以點擊「觸發同步探索網段」讀取交換器，或直接結束精靈。
                  </p>
                } @else {
                  <div class="subnets-list">
                    @for (sub of subnets(); track sub.id) {
                      <div class="subnet-row card">
                        <div class="subnet-info">
                          <span class="font-mono subnet-cidr">{{ sub.cidr }}</span>
                          <span class="muted-text font-mono"
                            >IP 數：{{ sub.ip_count }}，在線：{{ sub.online_count }}</span
                          >
                        </div>
                        <div class="subnet-controls">
                          <select
                            class="form-input form-input-sm"
                            [ngModel]="sub.state"
                            (ngModelChange)="changeSubnetState(sub.id, $event)"
                          >
                            <option value="pending">待檢視 (pending)</option>
                            <option value="managed">納管 (managed)</option>
                            <option value="unmanaged">不納管 (unmanaged)</option>
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
        <button type="button" class="btn btn-ghost" (click)="close()">離開（保留目前進度）</button>

        <div class="footer-actions">
          @if (currentStep() < 5) {
            <button type="button" class="btn btn-secondary" (click)="skipCurrentStep()">
              稍後再設定（跳過此步）
            </button>
            <button
              type="button"
              class="btn btn-primary"
              (click)="saveAndContinue()"
              [disabled]="savingStep()"
            >
              {{ savingStep() ? '處理中...' : '儲存並繼續' }}
            </button>
          } @else {
            <button type="button" class="btn btn-primary" (click)="finishWizard()">
              完成精靈，進入系統
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
        box-shadow: var(--shadow-xl);
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
        background-color: var(--color-online);
        color: #ffffff;
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
        background-color: var(--color-conflict-bg);
        color: var(--color-conflict);
        border: 1px solid var(--color-conflict-border);
        &.success {
          background-color: var(--color-online-bg);
          color: var(--color-online);
          border-color: var(--color-online-border);
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

  readonly steps = [
    { index: 1, label: '園區資訊' },
    { index: 2, label: '驗證來源' },
    { index: 3, label: '郵件通知' },
    { index: 4, label: '第一台交換器' },
    { index: 5, label: '檢視網段' },
  ];

  protected readonly currentStep = signal(1);
  protected readonly savingStep = signal(false);
  protected readonly step4Skipped = signal(false);

  // Step 1
  protected campusForm = { campus_name: 'Würzburg', timezone: 'Asia/Taipei' };

  // Step 2
  protected authForm: AuthFormData = {
    active_source: 'ad',
    ad_url: '',
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

    this.http.get<{ campus_name: string; timezone: string }>('/api/v1/settings/campus').subscribe({
      next: (res) => (this.campusForm = res),
      error: () => {},
    });
  }

  saveAndContinue(): void {
    this.savingStep.set(true);
    const step = this.currentStep();

    if (step === 1) {
      this.http
        .put('/api/v1/settings/campus', this.campusForm, {
          headers: { 'X-Requested-With': 'XMLHttpRequest' },
        })
        .subscribe({
          next: () => {
            this.advanceStep(2);
          },
          error: (err) => {
            this.savingStep.set(false);
            this.toast.show('儲存失敗', 'danger', err?.error?.message ?? '請確認欄位正確');
          },
        });
    } else if (step === 2) {
      this.http
        .put('/api/v1/settings/auth-source', this.authForm, {
          headers: { 'X-Requested-With': 'XMLHttpRequest' },
        })
        .subscribe({
          next: () => this.advanceStep(3),
          error: (err) => {
            this.savingStep.set(false);
            this.toast.show('儲存失敗', 'danger', err?.error?.message ?? '請確認欄位');
          },
        });
    } else if (step === 3) {
      const rcpts = this.adminRcptsText
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      const payload: Record<string, any> = {
        ...this.mailForm,
        admin_recipients: rcpts,
      };
      if (this.mailPassword) {
        payload['smtp_password'] = this.mailPassword;
      }
      this.http
        .put('/api/v1/settings/mail', payload, {
          headers: { 'X-Requested-With': 'XMLHttpRequest' },
        })
        .subscribe({
          next: () => this.advanceStep(4),
          error: (err) => {
            this.savingStep.set(false);
            this.toast.show('儲存失敗', 'danger', err?.error?.message ?? '請確認欄位');
          },
        });
    } else if (step === 4) {
      if (!this.switchForm.name || !this.switchForm.mgmt_ip) {
        this.savingStep.set(false);
        this.toast.show('請輸入名稱與 IP', 'danger');
        return;
      }
      this.http
        .post<{ id: number }>('/api/v1/switches', this.switchForm, {
          headers: { 'X-Requested-With': 'XMLHttpRequest' },
        })
        .subscribe({
          next: (res) => {
            this.switchCreatedId.set(res.id);
            this.step4Skipped.set(false);
            this.advanceStep(5);
            // 進入步驟 5 自動觸發一次同步讀取網段
            this.triggerSyncAndReloadSubnets();
          },
          error: (err) => {
            this.savingStep.set(false);
            this.toast.show('新增交換器失敗', 'danger', err?.error?.message);
          },
        });
    }
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
    this.http
      .post<{ success: boolean; message: string }>(
        '/api/v1/settings/auth-source/test',
        { username: this.authTestUser, password: this.authTestPass },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          this.testingAuth.set(false);
          this.authTestResult.set(res);
        },
        error: (err) => {
          this.testingAuth.set(false);
          this.authTestResult.set({ success: false, message: err?.error?.message ?? '測試失敗' });
        },
      });
  }

  testMail(): void {
    if (!this.testMailRecipient) return;
    this.testingMail.set(true);
    this.mailTestResult.set(null);
    this.http
      .post<{ success: boolean; message: string }>(
        '/api/v1/settings/mail/test',
        { recipient: this.testMailRecipient },
        { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
      )
      .subscribe({
        next: (res) => {
          this.testingMail.set(false);
          this.mailTestResult.set(res);
        },
        error: (err) => {
          this.testingMail.set(false);
          this.mailTestResult.set({ success: false, message: err?.error?.message ?? '測試失敗' });
        },
      });
  }

  testReadSwitch(): void {
    const swId = this.switchCreatedId();
    if (!swId) return;
    this.testingSwitch.set(true);
    this.switchTestResult.set(null);
    // 取得預設 vrf id 執行 test-read
    this.http.get<any>(`/api/v1/switches/${swId}`).subscribe({
      next: (sw) => {
        const vrfId = sw.vrfs?.[0]?.id ?? 1;
        this.http
          .post<any>(
            `/api/v1/switches/${swId}/vrfs/${vrfId}/test-read`,
            {},
            { headers: { 'X-Requested-With': 'XMLHttpRequest' } },
          )
          .subscribe({
            next: (tr) => {
              this.testingSwitch.set(false);
              this.switchTestResult.set({
                success: tr.is_success,
                message: tr.is_success
                  ? `讀取成功，共學得 ${tr.learned_count} 筆 ARP`
                  : `讀取失敗：${tr.error_class}`,
              });
            },
            error: (err) => {
              this.testingSwitch.set(false);
              this.switchTestResult.set({
                success: false,
                message: err?.error?.message ?? '讀取測試失敗',
              });
            },
          });
      },
      error: () => this.testingSwitch.set(false),
    });
  }

  triggerSyncAndReloadSubnets(): void {
    this.syncing.set(true);
    this.http
      .post('/api/v1/sync/trigger', {}, { headers: { 'X-Requested-With': 'XMLHttpRequest' } })
      .subscribe({
        next: () => {
          this.loadSubnets();
        },
        error: () => {
          this.loadSubnets();
        },
      });
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
          this.toast.show('網段狀態已更新', 'success');
          this.loadSubnets();
        },
        error: (err) => this.toast.show('更新失敗', 'danger', err?.error?.message),
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
          this.toast.show('初始設定精靈已完成！', 'success');
          this.dialogRef.close(true);
        },
        error: () => this.dialogRef.close(true),
      });
  }

  close(): void {
    this.dialogRef.close(false);
  }
}
