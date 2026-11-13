import {
  defaultWorkbenchFilters,
  filtersToBatchFilter,
  filtersToHttpParams,
  filtersToRouteQuery,
  readFiltersFromQuery,
} from './workbench.filters';

describe('工作臺篩選參數', () => {
  it('郵件連結的查詢字串可還原為篩選條件，未出現的條件回到預設值', () => {
    const f = defaultWorkbenchFilters();
    f.search = '舊值';
    readFiltersFromQuery({ owner_me: 'true', pending_confirmation: 'true', page: '3' }, f);
    expect(f.owner_me).toBeTrue();
    expect(f.pending_confirmation).toBeTrue();
    expect(f.has_conflict).toBeFalse();
    expect(f.search).toBe('');
    expect(f.page).toBe(3);
  });

  it('網址查詢參數：未設定的條件為 null，第 1 頁不寫入網址', () => {
    const f = defaultWorkbenchFilters();
    f.presence = 'online';
    f.registration = 'unregistered';
    const q = filtersToRouteQuery(f, 1);
    expect(q['presence']).toBe('online');
    expect(q['registration']).toBe('unregistered');
    expect(q['search']).toBeNull();
    expect(q['owner_me']).toBeNull();
    expect(q['page']).toBeNull();
  });

  it('API 查詢參數依固定順序送出，只帶有值的條件', () => {
    const f = defaultWorkbenchFilters();
    f.subnet_id = '7';
    f.has_conflict = true;
    expect(filtersToHttpParams(f, 50).toString()).toBe(
      'page=1&page_size=50&subnet_id=7&has_conflict=true&sort_by=ip_address&sort_dir=asc',
    );
  });

  it('批次條件的 subnet_id 以數字傳送，未選為 0', () => {
    const f = defaultWorkbenchFilters();
    expect(filtersToBatchFilter(f)['subnet_id']).toBe(0);
    f.subnet_id = '12';
    expect(filtersToBatchFilter(f)).toEqual({
      search: '',
      subnet_id: 12,
      presence: '',
      registration: '',
      expiring_soon: false,
      has_conflict: false,
      owner_me: false,
      pending_confirmation: false,
    });
  });
});
