import { TestBed, fakeAsync, tick } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { DialogRef } from '@angular/cdk/dialog';
import { SetupWizardDialogComponent } from './setup-wizard.dialog';

describe('SetupWizardDialogComponent', () => {
  let httpTesting: HttpTestingController;
  let dialogCloseSpy: jasmine.Spy;

  beforeEach(async () => {
    dialogCloseSpy = jasmine.createSpy('close');
    await TestBed.configureTestingModule({
      imports: [SetupWizardDialogComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: DialogRef, useValue: { close: dialogCloseSpy } },
      ],
    }).compileComponents();

    httpTesting = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpTesting.verify();
  });

  const syncStatus = (inProgress: boolean) => ({
    in_progress: inProgress,
    round_incomplete: false,
    last_sync_at: inProgress ? null : '2026-11-16T01:00:00Z',
    last_sync_success: !inProgress,
  });

  function flushSyncFinished() {
    httpTesting.expectOne('/api/v1/sync/status').flush(syncStatus(false));
  }

  it('應該完整走完 5 個設定精靈步驟，包含測試按鈕與完成精靈', () => {
    const fixture = TestBed.createComponent(SetupWizardDialogComponent);
    fixture.detectChanges();

    // 初始化讀取進度與園區設定
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 1, unfinished_items: [] });
    httpTesting
      .expectOne('/api/v1/settings/campus')
      .flush({ campus_name: 'Würzburg', timezone: 'Asia/Taipei' });
    fixture.detectChanges();

    const comp = fixture.componentInstance as any;
    expect(fixture.nativeElement.textContent).toContain('步驟 1：園區基本資訊');

    // 步驟 1 儲存並繼續 -> 步驟 2
    comp.campusForm = {
      campus_name: '新竹園區',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com',
    };
    comp.saveAndContinue();
    httpTesting
      .expectOne('/api/v1/settings/campus')
      .flush({ campus_name: '新竹園區', timezone: 'Asia/Taipei' });
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 2, unfinished_items: [] });
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('步驟 2：一般使用者驗證來源');

    // 步驟 2 測試登入並繼續 -> 步驟 3
    comp.authTestUser = 'testuser';
    comp.authTestPass = 'testpass';
    comp.testAuth();
    // 測試的是後端已儲存的設定，所以先儲存畫面上的內容
    httpTesting.expectOne('/api/v1/settings/auth-source').flush({});
    httpTesting
      .expectOne('/api/v1/settings/auth-source/test')
      .flush({ success: true, message: '驗證通過' });
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('驗證通過');

    comp.saveAndContinue();
    httpTesting.expectOne('/api/v1/settings/auth-source').flush({});
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 3, unfinished_items: [] });
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('步驟 3：郵件通知設定');

    // 步驟 3 跳過 -> 步驟 4
    comp.skipCurrentStep();
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 4, unfinished_items: [] });
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('步驟 4：新增第一台 ARP 來源交換器');

    // 步驟 4 新增交換器並進入步驟 5（自動觸發同步）
    comp.switchForm.name = 'Core-01';
    comp.switchForm.mgmt_ip = '10.0.0.1';
    comp.saveAndContinue();
    httpTesting.expectOne('/api/v1/switches').flush({ id: 1 });
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 5, unfinished_items: [] });
    httpTesting.expectOne('/api/v1/sync/trigger').flush({ message: '已觸發立即同步' });
    flushSyncFinished();
    httpTesting.expectOne('/api/v1/subnets').flush({
      pending_count: 1,
      items: [{ id: 10, cidr: '10.10.0.0/24', state: 'pending', ip_count: 5, online_count: 3 }],
    });
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('步驟 5：檢視並設定納管網段');
    expect(fixture.nativeElement.textContent).toContain('10.10.0.0/24');

    // 步驟 5 設為納管並完成精靈
    comp.changeSubnetState(10, 'managed');
    httpTesting.expectOne('/api/v1/subnets/10/state').flush({});
    httpTesting.expectOne('/api/v1/subnets').flush({
      pending_count: 0,
      items: [{ id: 10, cidr: '10.10.0.0/24', state: 'managed', ip_count: 5, online_count: 3 }],
    });

    comp.finishWizard();
    const finishReq = httpTesting.expectOne('/api/v1/settings/wizard');
    expect(finishReq.request.body).toEqual({ current_step: 5, completed: true });
    finishReq.flush({ completed: true, current_step: 5, unfinished_items: [] });

    expect(dialogCloseSpy).toHaveBeenCalledWith(true);
  });

  // 把精靈開到指定的步驟
  function openAtStep(step: number) {
    const fixture = TestBed.createComponent(SetupWizardDialogComponent);
    fixture.detectChanges();
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: step, unfinished_items: [] });
    httpTesting
      .expectOne('/api/v1/settings/campus')
      .flush({ campus_name: 'Würzburg', timezone: 'Asia/Taipei', public_base_url: '' });
    fixture.detectChanges();
    return { fixture, comp: fixture.componentInstance as any };
  }

  it('步驟 3 寄測試信前先儲存畫面上的郵件設定', () => {
    const { fixture, comp } = openAtStep(3);
    comp.mailForm.smtp_host = 'smtp.example.com';
    comp.mailForm.sender_address = 'ipmgmt@example.com';
    comp.mailPassword = 'UnitTestSmtpPass#1';
    comp.adminRcptsText = 'a@example.com, b@example.com';
    comp.testMailRecipient = 'a@example.com';
    comp.testMail();

    const save = httpTesting.expectOne('/api/v1/settings/mail');
    expect(save.request.method).toBe('PUT');
    expect(save.request.body.smtp_host).toBe('smtp.example.com');
    expect(save.request.body.admin_recipients).toEqual(['a@example.com', 'b@example.com']);
    expect(save.request.body.smtp_password).toBe('UnitTestSmtpPass#1');
    httpTesting.expectNone('/api/v1/settings/mail/test');
    save.flush({});

    const test = httpTesting.expectOne('/api/v1/settings/mail/test');
    expect(test.request.body).toEqual({ recipient: 'a@example.com' });
    test.flush({ success: true, message: '測試信已寄出' });
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('測試信已寄出');
  });

  it('步驟 3 儲存失敗時不寄測試信，並顯示後端的錯誤訊息', () => {
    const { fixture, comp } = openAtStep(3);
    comp.testMailRecipient = 'a@example.com';
    comp.testMail();
    httpTesting
      .expectOne('/api/v1/settings/mail')
      .flush(
        { code: 'validation_error', message: 'SMTP 主機不可為空' },
        { status: 422, statusText: 'Unprocessable Entity' },
      );
    httpTesting.expectNone('/api/v1/settings/mail/test');
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('SMTP 主機不可為空');
  });

  it('步驟 4 可以在同一步驟測試讀取：先建立交換器，讀取它的預設 VRF 讀取設定', () => {
    const { fixture, comp } = openAtStep(4);
    comp.switchForm.name = 'Core-01';
    comp.switchForm.mgmt_ip = '10.0.0.1';
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('儲存並測試讀取');

    comp.testReadSwitch();
    const create = httpTesting.expectOne('/api/v1/switches');
    expect(create.request.method).toBe('POST');
    create.flush({ id: 7, vrf_configs: [{ id: 42, vrf_name: 'default' }] });

    const read = httpTesting.expectOne('/api/v1/switches/7/vrfs/42/test-read');
    read.flush({ success: true, arp_count: 12, subnet_count: 2 });
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('讀取成功，共讀到 12 筆 ARP、2 個介面位址');

    // 測試後修改設定再繼續：更新同一台交換器，不重複建立
    comp.switchForm.name = 'Core-01-renamed';
    comp.saveAndContinue();
    const update = httpTesting.expectOne('/api/v1/switches/7');
    expect(update.request.method).toBe('PUT');
    expect(update.request.body.name).toBe('Core-01-renamed');
    update.flush({ id: 7, vrf_configs: [{ id: 42, vrf_name: 'default' }] });
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 5, unfinished_items: [] });
    httpTesting.expectOne('/api/v1/sync/trigger').flush({ message: '已觸發立即同步' });
    flushSyncFinished();
    httpTesting.expectOne('/api/v1/subnets').flush({ pending_count: 0, items: [] });
  });

  it('步驟 4 測試讀取失敗時顯示原因分類與摘要', () => {
    const { fixture, comp } = openAtStep(4);
    comp.switchForm.name = 'Core-01';
    comp.switchForm.mgmt_ip = '10.0.0.1';
    comp.testReadSwitch();
    httpTesting.expectOne('/api/v1/switches').flush({ id: 7, vrf_configs: [{ id: 42 }] });
    httpTesting.expectOne('/api/v1/switches/7/vrfs/42/test-read').flush({
      success: false,
      arp_count: 0,
      subnet_count: 0,
      error_class: 'timeout',
      error_summary: 'SNMP 請求逾時',
    });
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('讀取失敗（timeout）：SNMP 請求逾時');
  });

  it('步驟 5 觸發同步後，等這一輪結束才載入網段清單', fakeAsync(() => {
    const { comp } = openAtStep(5);
    comp.triggerSyncAndReloadSubnets();
    httpTesting.expectOne('/api/v1/sync/trigger').flush({ message: '已觸發立即同步' });

    // 同步還在進行：不可先去讀網段清單
    httpTesting.expectOne('/api/v1/sync/status').flush(syncStatus(true));
    httpTesting.expectNone('/api/v1/subnets');
    expect(comp.syncing()).toBeTrue();

    tick(1000);
    httpTesting.expectOne('/api/v1/sync/status').flush(syncStatus(true));
    httpTesting.expectNone('/api/v1/subnets');

    tick(1000);
    httpTesting.expectOne('/api/v1/sync/status').flush(syncStatus(false));
    httpTesting.expectOne('/api/v1/subnets').flush({
      pending_count: 1,
      items: [{ id: 10, cidr: '10.10.0.0/24', state: 'pending', ip_count: 0, online_count: 0 }],
    });
    expect(comp.syncing()).toBeFalse();
    expect(comp.subnets().length).toBe(1);
  }));

  it('步驟 5 已有同步在執行時（409）一樣等它結束再載入', () => {
    const { comp } = openAtStep(5);
    comp.triggerSyncAndReloadSubnets();
    httpTesting
      .expectOne('/api/v1/sync/trigger')
      .flush(
        { code: 'sync_in_progress', message: '目前已有同步輪次正在執行中' },
        { status: 409, statusText: 'Conflict' },
      );
    flushSyncFinished();
    httpTesting.expectOne('/api/v1/subnets').flush({ pending_count: 0, items: [] });
    expect(comp.syncing()).toBeFalse();
  });
});
