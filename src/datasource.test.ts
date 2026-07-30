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

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({
    replace: (s: string) => templateVars[s] ?? s,
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
  });

  describe('applyTemplateVariables', () => {
    const scopedVars: ScopedVars = {};

    it('substitutes variables in the dataset', () => {
      const result = ds.applyTemplateVariables(makeQuery({ dataset: '$dataset' }), scopedVars);
      expect(result.dataset).toBe('production');
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
