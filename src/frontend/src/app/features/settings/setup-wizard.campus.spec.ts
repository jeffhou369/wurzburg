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

  it('精靈儲存園區資訊時不得帶 public_base_url，避免清除已設定的公開網址', () => {
    const fixture = TestBed.createComponent(SetupWizardDialogComponent);
    fixture.detectChanges();
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 1, unfinished_items: [] });
    // 後端 GET 會回傳既有的公開網址；精靈沒有這個欄位，PUT 就必須省略（省略 = 保留）
    httpTesting.expectOne('/api/v1/settings/campus').flush({
      campus_name: 'Würzburg',
      timezone: 'Asia/Taipei',
      public_base_url: 'https://ip.example.com',
    });
    fixture.detectChanges();

    const comp = fixture.componentInstance as any;
    comp.campusForm.campus_name = '新竹園區';
    comp.saveAndContinue();

    const put = httpTesting.expectOne('/api/v1/settings/campus');
    expect(put.request.method).toBe('PUT');
    expect(put.request.body).toEqual({ campus_name: '新竹園區', timezone: 'Asia/Taipei' });
    expect('public_base_url' in put.request.body).toBeFalse();
    put.flush({ campus_name: '新竹園區', timezone: 'Asia/Taipei' });
    httpTesting
      .expectOne('/api/v1/settings/wizard')
      .flush({ completed: false, current_step: 2, unfinished_items: [] });
  });
});
