import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { App } from './app';

describe('App', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [App],
      providers: [provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
  });

  it('應該建立應用程式並向後端查詢健康狀態', () => {
    const fixture = TestBed.createComponent(App);
    const httpTesting = TestBed.inject(HttpTestingController);
    fixture.detectChanges();

    const req = httpTesting.expectOne('/api/v1/health');
    expect(req.request.method).toBe('GET');
    req.flush({
      status: 'ok',
      timestamp: '2026-11-12T12:00:00Z',
      database: 'connected',
      campus: 'Würzburg',
    });

    fixture.detectChanges();
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('h1')?.textContent).toContain('Würzburg');
    expect(compiled.textContent).toContain('正常運作中');
    expect(compiled.textContent).toContain('已連線');

    httpTesting.verify();
  });
});
