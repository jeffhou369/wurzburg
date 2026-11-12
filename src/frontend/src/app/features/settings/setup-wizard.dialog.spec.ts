import { TestBed } from '@angular/core/testing';
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
    comp.campusForm = { campus_name: '新竹園區', timezone: 'Asia/Taipei' };
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
    httpTesting.expectOne('/api/v1/sync/trigger').flush({ status: 'ok' });
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
});
