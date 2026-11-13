import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { DialogRef } from '@angular/cdk/dialog';
import { SetupWizardDialogComponent } from './setup-wizard.dialog';

describe('SetupWizardDialogComponent 步驟 1 與公開網址（TestBed 元件層級，非瀏覽器 E2E）', () => {
  let httpTesting: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [SetupWizardDialogComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: DialogRef, useValue: { close: jasmine.createSpy('close') } },
      ],
    }).compileComponents();
    httpTesting = TestBed.inject(HttpTestingController);
  });

  afterEach(() => httpTesting.verify());

  function openWizard(campus: Record<string, string>) {
    const fixture = TestBed.createComponent(SetupWizardDialogComponent);
    fixture.detectChanges();
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 1, unfinished_items: [] });
    httpTesting.expectOne('/api/v1/settings/campus').flush(campus);
    fixture.detectChanges();
    return fixture.componentInstance as any;
  }

  function flushWizardProgress() {
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 2, unfinished_items: [] });
  }

  it('已設定的公開網址會帶入精靈，儲存時原樣送回，不會被清除', () => {
    const comp = openWizard({
      campus_name: 'Würzburg',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com',
    });
    comp.campusForm.campus_name = '新竹園區';
    comp.saveAndContinue();

    const put = httpTesting.expectOne('/api/v1/settings/campus');
    expect(put.request.method).toBe('PUT');
    expect(put.request.body).toEqual({
      campus_name: '新竹園區',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com',
    });
    put.flush(put.request.body);
    flushWizardProgress();
  });

  it('尚未設定公開網址時，預先帶入目前瀏覽器使用的網址', () => {
    const comp = openWizard({
      campus_name: 'Würzburg',
      timezone: 'Asia/Taipei',
      public_base_url: '',
    });
    expect(comp.campusForm.public_base_url).toBe(window.location.origin);
    comp.saveAndContinue();

    const put = httpTesting.expectOne('/api/v1/settings/campus');
    expect(put.request.body.public_base_url).toBe(window.location.origin);
    put.flush(put.request.body);
    flushWizardProgress();
  });

  it('公開網址格式不正確時不送出', () => {
    const comp = openWizard({
      campus_name: 'Würzburg',
      timezone: 'Asia/Taipei',
      public_base_url: '',
    });
    comp.campusForm.public_base_url = 'https://ip.example.com/sub/path';
    comp.saveAndContinue();

    httpTesting.expectNone('/api/v1/settings/campus');
    expect(comp.currentStep()).toBe(1);
  });
});
