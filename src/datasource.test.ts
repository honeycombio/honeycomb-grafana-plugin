import { DataSourceInstanceSettings, ScopedVars } from '@grafana/data';

import { HoneycombDataSource } from './datasource';
import { defaultQuery } from './defaults';
import { HoneycombDataSourceOptions, HoneycombQuery } from './types';

// Template variable substitution is provided by Grafana at runtime; here we
// simulate it with a simple $var -> value map.
const templateVars: Record<string, string> = {
  $dataset: 'production',
  $column: 'duration_ms',
  $service: 'checkout',
};

// A jest.fn rather than a plain arrow so tests can assert *how* it was called.
// The datasource has to forward scopedVars as the second argument, and a mock
// that ignored it would keep passing if that ever regressed — variables would
// then resolve against the dashboard instead of the panel's own scope, which is
// wrong in exactly the cases (repeated panels, table row links) that are hardest
// to notice.
const replaceMock = jest.fn((s: string, _scopedVars?: unknown) => templateVars[s] ?? s);

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({
    replace: (...args: unknown[]) => replaceMock(...(args as [string, unknown?])),
  }),
}));

const instanceSettings = {
  id: 1,
  uid: 'honeycomb-test',
  type: 'honeycombio-honeycomb-datasource',
  name: 'Honeycomb',
  jsonData: {},
  access: 'proxy',
  meta: {},
  readOnly: false,
} as unknown as DataSourceInstanceSettings<HoneycombDataSourceOptions>;

function makeQuery(overrides: Partial<HoneycombQuery> = {}): HoneycombQuery {
  return {
    refId: 'A',
    ...defaultQuery(),
    dataset: 'my-dataset',
    ...overrides,
  } as HoneycombQuery;
}

describe('HoneycombDataSource', () => {
  let ds: HoneycombDataSource;

  beforeEach(() => {
    ds = new HoneycombDataSource(instanceSettings);
  });

  describe('filterQuery', () => {
    it('runs a well-formed query', () => {
      expect(ds.filterQuery(makeQuery())).toBe(true);
    });

    it('skips queries without a dataset', () => {
      expect(ds.filterQuery(makeQuery({ dataset: '' }))).toBe(false);
      expect(ds.filterQuery(makeQuery({ dataset: '   ' }))).toBe(false);
    });

    it('skips builder-mode queries without calculations', () => {
      expect(ds.filterQuery(makeQuery({ calculations: [] }))).toBe(false);
      expect(ds.filterQuery(makeQuery({ calculations: undefined }))).toBe(false);
    });

    it('skips raw-mode queries without JSON', () => {
      expect(ds.filterQuery(makeQuery({ rawMode: true, rawJson: '' }))).toBe(false);
      expect(ds.filterQuery(makeQuery({ rawMode: true, rawJson: '  ' }))).toBe(false);
    });

    it('runs raw-mode queries with JSON even without calculations', () => {
      expect(ds.filterQuery(makeQuery({ rawMode: true, rawJson: '{"calculations":[]}', calculations: [] }))).toBe(
        true
      );
    });

    // The queryType branches below short-circuit before the calculations check,
    // so each needs its own case. Their guards are the ones that make a panel
    // silently render nothing when an id field is blank.
    it('skips single-SLO queries without an SLO id', () => {
      expect(ds.filterQuery(makeQuery({ queryType: 'slo', sloResultType: 'single' }))).toBe(false);
      expect(ds.filterQuery(makeQuery({ queryType: 'slo', sloResultType: 'single', sloId: '  ' }))).toBe(false);
    });

    it('runs single-SLO queries with an SLO id', () => {
      expect(ds.filterQuery(makeQuery({ queryType: 'slo', sloResultType: 'single', sloId: 'slo-123' }))).toBe(true);
    });

    it('runs SLO list queries with only a dataset', () => {
      expect(ds.filterQuery(makeQuery({ queryType: 'slo', sloResultType: 'list', calculations: [] }))).toBe(true);
    });

    it('runs logs queries with only a dataset', () => {
      expect(ds.filterQuery(makeQuery({ queryType: 'logs', calculations: [] }))).toBe(true);
    });

    it('skips single-trace queries without a trace id', () => {
      expect(ds.filterQuery(makeQuery({ queryType: 'traces', tracesResultType: 'single' }))).toBe(false);
      expect(ds.filterQuery(makeQuery({ queryType: 'traces', tracesResultType: 'single', traceId: ' ' }))).toBe(false);
    });

    // tracesResultType defaults to 'single' via `?? 'single'`, so a traces query
    // with the field unset must still demand a trace id. Easy to regress into
    // treating undefined as 'search' and firing an unbounded query.
    it('treats traces queries with no result type as single, so a trace id is required', () => {
      expect(ds.filterQuery(makeQuery({ queryType: 'traces' }))).toBe(false);
      expect(ds.filterQuery(makeQuery({ queryType: 'traces', traceId: 'abc123' }))).toBe(true);
    });

    it('runs trace search queries with only a dataset', () => {
      expect(ds.filterQuery(makeQuery({ queryType: 'traces', tracesResultType: 'search', calculations: [] }))).toBe(
        true
      );
    });

    it('skips queries of any type without a dataset', () => {
      for (const queryType of ['metrics', 'slo', 'logs', 'traces', 'raw'] as const) {
        expect(ds.filterQuery(makeQuery({ queryType, dataset: '' }))).toBe(false);
      }
    });
  });

  describe('applyTemplateVariables', () => {
    const scopedVars: ScopedVars = { __interval: { text: '1m', value: '1m' } };

    beforeEach(() => {
      replaceMock.mockClear();
    });

    it('substitutes variables in the dataset', () => {
      const result = ds.applyTemplateVariables(makeQuery({ dataset: '$dataset' }), scopedVars);
      expect(result.dataset).toBe('production');
    });

    // Guards the panel's own variable scope: without scopedVars, Grafana resolves
    // against the dashboard, which silently returns the wrong value for repeated
    // panels and data links.
    it('forwards scopedVars to the template service', () => {
      ds.applyTemplateVariables(makeQuery({ dataset: '$dataset' }), scopedVars);
      expect(replaceMock).toHaveBeenCalledWith('$dataset', scopedVars);
      for (const call of replaceMock.mock.calls) {
        expect(call[1]).toBe(scopedVars);
      }
    });

    it('substitutes variables in breakdowns and filters', () => {
      const query = makeQuery({
        breakdowns: ['$column', 'static-column'],
        filters: [{ column: '$column', op: '=', value: '$service' }],
      });
      const result = ds.applyTemplateVariables(query, scopedVars);
      expect(result.breakdowns).toEqual(['duration_ms', 'static-column']);
      expect(result.filters?.[0]).toEqual({ column: 'duration_ms', op: '=', value: 'checkout' });
    });

    it('leaves non-string filter values untouched', () => {
      const query = makeQuery({ filters: [{ column: 'status', op: '>', value: 500 }] });
      const result = ds.applyTemplateVariables(query, scopedVars);
      expect(result.filters?.[0].value).toBe(500);
    });

    // havings is the only substituted field whose column is optional, so it is
    // the only one guarding against replace(undefined).
    it('substitutes variables in havings', () => {
      const query = makeQuery({
        havings: [{ calculateOp: 'P95', column: '$column', op: '>', value: '$service' }],
      });
      const result = ds.applyTemplateVariables(query, scopedVars);
      expect(result.havings?.[0]).toEqual({
        calculateOp: 'P95',
        column: 'duration_ms',
        op: '>',
        value: 'checkout',
      });
    });

    it('leaves an omitted having column and numeric value alone', () => {
      const query = makeQuery({ havings: [{ op: '>', value: 100 }] });
      const result = ds.applyTemplateVariables(query, scopedVars);
      expect(result.havings?.[0]).toEqual({ op: '>', value: 100 });
    });

    it('substitutes variables in raw JSON when present', () => {
      const result = ds.applyTemplateVariables(makeQuery({ rawJson: '$dataset' }), scopedVars);
      expect(result.rawJson).toBe('production');
    });

    it('does not mutate the original query', () => {
      const query = makeQuery({ dataset: '$dataset' });
      ds.applyTemplateVariables(query, scopedVars);
      expect(query.dataset).toBe('$dataset');
    });
  });

  describe('metricFindQuery', () => {
    const datasets = [
      { name: 'Production', slug: 'production' },
      { name: 'Staging', slug: 'staging' },
    ];
    const columns = [
      { key_name: 'duration_ms', hidden: false },
      { key_name: 'internal_field', hidden: true },
    ];

    beforeEach(() => {
      jest.spyOn(ds, 'getResource').mockImplementation(async (path: string) => {
        if (path === 'datasets') {
          return datasets;
        }
        if (path.startsWith('columns?')) {
          return columns;
        }
        throw new Error(`unexpected resource path: ${path}`);
      });
    });

    it('lists datasets', async () => {
      const result = await ds.metricFindQuery({ queryType: 'datasets' });
      expect(result).toEqual([
        { text: 'Production', value: 'production' },
        { text: 'Staging', value: 'staging' },
      ]);
    });

    it('lists visible columns for a dataset', async () => {
      const result = await ds.metricFindQuery({ queryType: 'columns', dataset: 'production' });
      expect(result).toEqual([{ text: 'duration_ms', value: 'duration_ms' }]);
      expect(ds.getResource).toHaveBeenCalledWith('columns?dataset=production');
    });

    it('returns nothing for a columns query without a dataset', async () => {
      const result = await ds.metricFindQuery({ queryType: 'columns' });
      expect(result).toEqual([]);
    });

    it('parses legacy string queries', async () => {
      expect(await ds.metricFindQuery('datasets')).toHaveLength(2);
      expect(await ds.metricFindQuery('columns:production')).toEqual([
        { text: 'duration_ms', value: 'duration_ms' },
      ]);
      // Unrecognized strings fall back to listing datasets.
      expect(await ds.metricFindQuery('bogus')).toHaveLength(2);
    });

    it('URL-encodes dataset slugs in resource paths', async () => {
      await ds.metricFindQuery({ queryType: 'columns', dataset: 'my dataset/prod' });
      expect(ds.getResource).toHaveBeenCalledWith('columns?dataset=my%20dataset%2Fprod');
    });
  });

  describe('getDefaultQuery', () => {
    it('starts with a COUNT over the whole dataset', () => {
      const q = ds.getDefaultQuery();
      expect(q.calculations).toEqual([{ op: 'COUNT' }]);
      expect(q.rawMode).toBe(false);
      expect(q.dataset).toBe('');
    });
  });
});
