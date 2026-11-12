import { HttpParams } from '@angular/common/http';
import type { Params } from '@angular/router';

/**
 * 工作臺 IP 表的篩選參數，集中定義名稱與轉換規則。
 *
 * 同一組參數同時用在網址查詢字串（可分享、郵件連結）、IP 紀錄 API 與「全選篩選結果」的批次條件，
 * 新增或改名篩選時只需改這裡。參數名稱與後端 `docs/api/resources/ip_records.yaml` 一致。
 * 陣列順序即送出順序，維持穩定以利快取與測試比對。
 */
export const TEXT_FILTER_KEYS = ['search', 'subnet_id', 'presence', 'registration'] as const;
export const FLAG_FILTER_KEYS = [
  'expiring_soon',
  'has_conflict',
  'owner_me',
  'pending_confirmation',
] as const;

type TextFilterKey = (typeof TEXT_FILTER_KEYS)[number];
type FlagFilterKey = (typeof FLAG_FILTER_KEYS)[number];

export type WorkbenchFilters = Record<TextFilterKey, string> &
  Record<FlagFilterKey, boolean> & {
    sort_by: string;
    sort_dir: string;
    page: number;
  };

export function defaultWorkbenchFilters(): WorkbenchFilters {
  return {
    search: '',
    subnet_id: '',
    presence: '',
    registration: '',
    expiring_soon: false,
    has_conflict: false,
    owner_me: false,
    pending_confirmation: false,
    sort_by: 'ip_address',
    sort_dir: 'asc',
    page: 1,
  };
}

/** 由網址查詢字串還原篩選條件（排序不放在網址中，維持原值）。 */
export function readFiltersFromQuery(params: Params, filters: WorkbenchFilters): void {
  for (const key of TEXT_FILTER_KEYS) {
    filters[key] = params[key] ?? '';
  }
  for (const key of FLAG_FILTER_KEYS) {
    filters[key] = params[key] === 'true';
  }
  filters.page = Number(params['page'] ?? 1);
}

/** 轉為網址查詢參數；未設定的條件為 null，讓 Router 從網址移除。 */
export function filtersToRouteQuery(filters: WorkbenchFilters, page: number): Params {
  const query: Params = {};
  for (const key of TEXT_FILTER_KEYS) {
    query[key] = filters[key] || null;
  }
  for (const key of FLAG_FILTER_KEYS) {
    query[key] = filters[key] ? 'true' : null;
  }
  query['page'] = page > 1 ? page : null;
  return query;
}

/** 轉為 IP 紀錄列表 API 的查詢參數。 */
export function filtersToHttpParams(filters: WorkbenchFilters, pageSize: number): HttpParams {
  let p = new HttpParams().set('page', String(filters.page)).set('page_size', String(pageSize));
  for (const key of TEXT_FILTER_KEYS) {
    if (filters[key]) p = p.set(key, filters[key]);
  }
  for (const key of FLAG_FILTER_KEYS) {
    if (filters[key]) p = p.set(key, 'true');
  }
  return p.set('sort_by', filters.sort_by).set('sort_dir', filters.sort_dir);
}

/** 轉為批次 API「全選篩選結果」的 filter 物件；subnet_id 以數字傳送，未選為 0。 */
export function filtersToBatchFilter(filters: WorkbenchFilters): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of TEXT_FILTER_KEYS) {
    out[key] = key === 'subnet_id' ? Number(filters.subnet_id) || 0 : filters[key];
  }
  for (const key of FLAG_FILTER_KEYS) {
    out[key] = filters[key];
  }
  return out;
}
