import { DataQuery, DataSourceJsonData, SelectableValue } from '@grafana/data';

// ---------------------------------------------------------------------------
// Query model
// ---------------------------------------------------------------------------

export type QueryMode = 'timeseries' | 'table' | 'stat' | 'logs';

/**
 * QueryResultType controls which Honeycomb result fields are populated:
 *   - 'series':  timeseries data only (disable_series=false)
 *   - 'result':  summary rows only (disable_series=true) — higher row limit
 *   - 'both':    return both (default for advanced raw-mode users)
 *   - 'auto':    pick based on QueryMode — default for visual-editor users
 */
export type QueryResultType = 'auto' | 'series' | 'result' | 'both';

export const QUERY_RESULT_TYPE_OPTIONS: Array<SelectableValue<QueryResultType>> = [
  { label: 'auto', value: 'auto', description: 'Pick based on Query Mode (recommended)' },
  { label: 'series', value: 'series', description: 'Timeseries data only' },
  { label: 'result', value: 'result', description: 'Summary rows only (higher row limit)' },
  { label: 'both', value: 'both', description: 'Return both series and summary rows' },
];
export type FilterCombination = 'AND' | 'OR';

export type CalculationOp =
  | 'COUNT'
  | 'CONCURRENCY'
  | 'SUM'
  | 'AVG'
  | 'COUNT_DISTINCT'
  | 'HEATMAP'
  | 'MAX'
  | 'MIN'
  | 'P001'
  | 'P01'
  | 'P05'
  | 'P10'
  | 'P20'
  | 'P25'
  | 'P50'
  | 'P75'
  | 'P80'
  | 'P90'
  | 'P95'
  | 'P99'
  | 'P999'
  | 'RATE_AVG'
  | 'RATE_SUM'
  | 'RATE_MAX';

export type FilterOp =
  | '='
  | '!='
  | '>'
  | '>='
  | '<'
  | '<='
  | 'starts-with'
  | 'does-not-start-with'
  | 'ends-with'
  | 'does-not-end-with'
  | 'exists'
  | 'does-not-exist'
  | 'contains'
  | 'does-not-contain'
  | 'in'
  | 'not-in';

export interface Calculation {
  op: CalculationOp;
  column?: string;
  alias?: string;
  /** Per-calculation filters (Metrics Beta only). Honeycomb rejects these
   *  with a 4xx if the team isn't in Metrics Beta. */
  filters?: Filter[];
  filterCombination?: FilterCombination;
}

export interface Filter {
  column: string;
  op: FilterOp;
  value?: string | number | boolean;
}

export interface Order {
  op?: CalculationOp;
  column?: string;
  order: 'ascending' | 'descending';
}

/**
 * Having is a post-aggregation filter. It references a calculation (by op +
 * optional column) and compares the calculated value with a threshold.
 *
 * Honeycomb supports the following havingOps: '<', '<=', '=', '!=', '>=', '>'.
 */
export type HavingOp = '<' | '<=' | '=' | '!=' | '>=' | '>';

export interface Having {
  calculateOp?: CalculationOp;
  column?: string;
  op: HavingOp;
  value?: number | string;
}

export const HAVING_OPS: Array<SelectableValue<HavingOp>> = [
  { label: '<', value: '<' },
  { label: '≤', value: '<=' },
  { label: '=', value: '=' },
  { label: '≠', value: '!=' },
  { label: '≥', value: '>=' },
  { label: '>', value: '>' },
];

/**
 * QueryType selects the top-level Honeycomb query kind:
 *   - 'metrics' (default): events / aggregations via Query Data API
 *   - 'slo':     Honeycomb SLO list or single-SLO compliance
 *   - 'logs':    raw events rendered as Grafana log lines
 *   - 'traces':  spans rendered as Grafana trace frames (single-trace + search)
 *   - 'raw':     pass rawJson straight to the Query Data API
 */
export type QueryType = 'metrics' | 'slo' | 'logs' | 'traces' | 'raw';

/** TracesResultType picks between fetching a single trace by ID and searching. */
export type TracesResultType = 'single' | 'search';

export interface HoneycombQuery extends DataQuery {
  /** Top-level query kind. Defaults to 'metrics'. */
  queryType?: QueryType;
  dataset: string;
  queryMode: QueryMode;
  /** Override of the Honeycomb result-data shape; defaults to 'auto' (driven by queryMode). */
  queryResultType?: QueryResultType;

  /** SLO-specific: list all SLOs or fetch one by ID. */
  sloResultType?: 'list' | 'single';
  /** SLO-specific: the SLO ID to query (required when sloResultType='single'). */
  sloId?: string;

  /** Traces-specific: 'single' fetches one trace by ID; 'search' lists matching traces. */
  tracesResultType?: TracesResultType;
  /** Traces-specific: the trace ID to fetch (required when tracesResultType='single'). */
  traceId?: string;
  /**
   * Logs-specific: optional list of attribute columns to include in the log
   * line body. When empty, all available columns are included as `key=value`
   * attributes. A non-empty list lets users keep log lines tidy.
   */
  logsAttributes?: string[];

  calculations: Calculation[];
  filters: Filter[];
  filterCombination: FilterCombination;
  breakdowns: string[];
  orders: Order[];
  havings: Having[];
  limit: number;
  granularity: number; // 0 = auto-derive
  compareTimeOffset: number; // 0 = disabled
  rawMode: boolean;
  rawJson?: string;
}

// ---------------------------------------------------------------------------
// Variable query model
// ---------------------------------------------------------------------------

export type VariableQueryType = 'datasets' | 'columns';

export interface VariableQuery {
  queryType: VariableQueryType;
  dataset?: string;
}

// ---------------------------------------------------------------------------
// Data source configuration
// ---------------------------------------------------------------------------

/**
 * Options stored in jsonData (non-secret). These are visible to the browser.
 */
export interface HoneycombDataSourceOptions extends DataSourceJsonData {
  /**
   * Honeycomb API base URL.
   * Defaults to https://api.honeycomb.io
   * Use https://api.eu1.honeycomb.io for EU accounts.
   */
  apiUrl?: string;

  /**
   * Honeycomb team slug. Used to build trace and query deep links to ui.honeycomb.io.
   * Without it, deep links will fall back to the Honeycomb-provided URL only.
   */
  team?: string;

  /**
   * Honeycomb environment name (Classic accounts can leave blank).
   * Used to build deep links: https://ui.honeycomb.io/<team>/environments/<environment>/...
   */
  environment?: string;

  /**
   * Maximum query time window in days. Queries spanning a longer range are clamped
   * by the backend before being sent to Honeycomb. 0 = unbounded. Default 7.
   */
  timeWindowDays?: number;

  /** L1 cache TTL in minutes (query shape → query_id). Default: 30. */
  cacheTtlL1Minutes?: number;

  /** L2 cache TTL in minutes (execution key → query_result_id). Default: 10. */
  cacheTtlL2Minutes?: number;

  /** L3 cache TTL in minutes (completed query results). Default: 120. */
  cacheTtlL3Minutes?: number;

  /**
   * Maximum Create Query Result calls per minute made by this data source.
   * Honeycomb's default limit is 10/min per team; set this higher if Honeycomb
   * has raised yours. Default 8.
   */
  rateLimitPerMinute?: number;
}

/**
 * Options stored in secureJsonData (encrypted). NEVER sent to the browser.
 */
export interface HoneycombSecureJsonData {
  apiKey?: string;
}

// ---------------------------------------------------------------------------
// Resource API responses (from backend resource handlers)
// ---------------------------------------------------------------------------

export const ALL_DATASETS_SLUG = '__all__';

export interface DatasetMeta {
  name: string;
  slug: string;
  description?: string;
  created_at?: string;
}

export interface ColumnMeta {
  id: string;
  key_name: string;
  type: 'string' | 'float' | 'integer' | 'boolean';
  hidden: boolean;
  description?: string;
}

// ---------------------------------------------------------------------------
// UI helpers
// ---------------------------------------------------------------------------

export const CALCULATION_OPS: Array<SelectableValue<CalculationOp>> = [
  { label: 'COUNT', value: 'COUNT', description: 'Number of matching events' },
  { label: 'CONCURRENCY', value: 'CONCURRENCY' },
  { label: 'SUM', value: 'SUM', description: 'Sum of a numeric column' },
  { label: 'AVG', value: 'AVG', description: 'Average of a numeric column' },
  { label: 'COUNT_DISTINCT', value: 'COUNT_DISTINCT', description: 'Distinct values of a column' },
  { label: 'MAX', value: 'MAX' },
  { label: 'MIN', value: 'MIN' },
  { label: 'P50', value: 'P50', description: 'Median' },
  { label: 'P75', value: 'P75' },
  { label: 'P90', value: 'P90' },
  { label: 'P95', value: 'P95' },
  { label: 'P99', value: 'P99' },
  { label: 'P99.9', value: 'P999' },
  { label: 'HEATMAP', value: 'HEATMAP' },
  { label: 'RATE_AVG', value: 'RATE_AVG' },
  { label: 'RATE_SUM', value: 'RATE_SUM' },
  { label: 'RATE_MAX', value: 'RATE_MAX' },
  { label: 'P0.01', value: 'P001' },
  { label: 'P0.1', value: 'P01' },
  { label: 'P5', value: 'P05' },
  { label: 'P10', value: 'P10' },
  { label: 'P20', value: 'P20' },
  { label: 'P25', value: 'P25' },
  { label: 'P80', value: 'P80' },
];

/** Operations that require a column parameter. */
export const COLUMN_REQUIRED_OPS = new Set<CalculationOp>([
  'SUM', 'AVG', 'COUNT_DISTINCT', 'HEATMAP', 'MAX', 'MIN',
  'P001', 'P01', 'P05', 'P10', 'P20', 'P25', 'P50', 'P75',
  'P80', 'P90', 'P95', 'P99', 'P999', 'RATE_AVG', 'RATE_SUM', 'RATE_MAX',
]);

export const FILTER_OPS: Array<SelectableValue<FilterOp>> = [
  { label: '=', value: '=' },
  { label: '≠', value: '!=' },
  { label: '>', value: '>' },
  { label: '≥', value: '>=' },
  { label: '<', value: '<' },
  { label: '≤', value: '<=' },
  { label: 'starts-with', value: 'starts-with' },
  { label: 'does-not-start-with', value: 'does-not-start-with' },
  { label: 'ends-with', value: 'ends-with' },
  { label: 'does-not-end-with', value: 'does-not-end-with' },
  { label: 'exists', value: 'exists' },
  { label: 'does-not-exist', value: 'does-not-exist' },
  { label: 'contains', value: 'contains' },
  { label: 'does-not-contain', value: 'does-not-contain' },
  { label: 'in', value: 'in' },
  { label: 'not-in', value: 'not-in' },
];

/** Operations that do not take a value argument. */
export const NO_VALUE_FILTER_OPS = new Set<FilterOp>(['exists', 'does-not-exist']);

export const QUERY_MODE_OPTIONS: Array<SelectableValue<QueryMode>> = [
  { label: 'Time series', value: 'timeseries', description: 'One series per breakdown group' },
  { label: 'Table', value: 'table', description: 'Summary results as a table' },
  { label: 'Stat', value: 'stat', description: 'Single aggregated value' },
  { label: 'Logs', value: 'logs', description: 'Result rows rendered as log lines' },
];

export const DEFAULT_API_URL = 'https://api.honeycomb.io';
export const EU_API_URL = 'https://api.eu1.honeycomb.io';
export const DEFAULT_TIME_WINDOW_DAYS = 7;
/** Mirrors ratelimit.DefaultTokensPerMinute in the backend. */
export const DEFAULT_RATE_LIMIT_PER_MINUTE = 8;

/**
 * Honeycomb's allowed compare_time_offset_seconds values. The API rejects
 * any value not in this set; we offer a dropdown to keep users on the path.
 */
export const COMPARE_TIME_OFFSET_OPTIONS: Array<SelectableValue<number>> = [
  { label: 'None', value: 0 },
  { label: '30 minutes ago', value: 1800 },
  { label: '1 hour ago', value: 3600 },
  { label: '2 hours ago', value: 7200 },
  { label: '8 hours ago', value: 28800 },
  { label: '1 day ago', value: 86400 },
  { label: '1 week ago', value: 604800 },
  { label: '4 weeks ago', value: 2419200 },
  { label: '26 weeks ago', value: 15724800 },
];
