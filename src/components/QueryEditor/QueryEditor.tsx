import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { GrafanaTheme2, PanelData, QueryEditorProps, SelectableValue } from '@grafana/data';
import {
  CollapsableSection,
  Field,
  InlineField,
  InlineFieldRow,
  Input,
  RadioButtonGroup,
  Select,
  Spinner,
  TextArea,
  ToolbarButton,
  useTheme2,
} from '@grafana/ui';
import { css } from '@emotion/css';

import { HoneycombDataSource } from '../../datasource';
import {
  ALL_DATASETS_SLUG,
  ColumnMeta,
  COMPARE_TIME_OFFSET_OPTIONS,
  HoneycombDataSourceOptions,
  HoneycombQuery,
  QUERY_MODE_OPTIONS,
  QUERY_RESULT_TYPE_OPTIONS,
  QueryMode,
  QueryResultType,
  QueryType,
} from '../../types';
import { defaultQuery } from '../../defaults';
import { CalculationsEditor } from './CalculationsEditor';
import { FiltersEditor } from './FiltersEditor';
import { GroupByEditor } from './GroupByEditor';
import { HavingsEditor } from './HavingsEditor';
import { LogsEditor } from './LogsEditor';
import { OrderByEditor } from './OrderByEditor';
import { SLOEditor } from './SLOEditor';
import { TracesEditor } from './TracesEditor';

type Props = QueryEditorProps<HoneycombDataSource, HoneycombQuery, HoneycombDataSourceOptions>;

/**
 * extractHoneycombQueryURL reads the Honeycomb result URL that the backend
 * attaches as frame meta (custom.honeycombQueryURL) after a successful query.
 * See ADR-004.
 */
function extractHoneycombQueryURL(data?: PanelData): string | undefined {
  for (const frame of data?.series ?? []) {
    const custom = frame.meta?.custom as Record<string, unknown> | undefined;
    const url = custom?.honeycombQueryURL;
    if (typeof url === 'string' && url.trim() !== '') {
      return url;
    }
  }
  return undefined;
}

/**
 * QueryEditor is the main query builder UI for the Honeycomb datasource.
 *
 * It provides two modes:
 * 1. Visual builder: dataset picker, calculations, filters, group-by, order-by, limit, granularity.
 * 2. Raw JSON mode: a textarea for pasting raw Honeycomb Query API JSON.
 *
 * Template variables (${var_name}) are supported in any string input.
 */
export function QueryEditor({ datasource, query, onChange, onRunQuery, data }: Props) {
  const theme = useTheme2();
  const styles = getStyles(theme);

  const q = useMemo<HoneycombQuery>(
    () => ({ ...defaultQuery(), ...query }) as HoneycombQuery,
    [query]
  );

  const honeycombQueryURL = useMemo(() => extractHoneycombQueryURL(data), [data]);

  const allDatasetsOption: SelectableValue<string> = { label: 'All Datasets', value: ALL_DATASETS_SLUG, description: 'Query across all datasets in the environment' };

  const [datasets, setDatasets] = useState<Array<SelectableValue<string>>>([allDatasetsOption]);
  const [columns, setColumns] = useState<ColumnMeta[]>([]);
  // Start in the loading state so the first paint shows a spinner without
  // calling setState synchronously inside the effect body.
  const [loadingDatasets, setLoadingDatasets] = useState(true);
  const [loadingColumns, setLoadingColumns] = useState(false);

  // Load datasets on mount.
  useEffect(() => {
    let cancelled = false;
    datasource
      .listDatasets()
      .then((ds) => {
        if (cancelled) {
          return;
        }
        setDatasets([allDatasetsOption, ...ds.map((d) => ({ label: d.name, value: d.slug, description: d.description }))]);
      })
      .catch(() => {
        if (!cancelled) {
          setDatasets([allDatasetsOption]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingDatasets(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Load columns when dataset changes. __all__ has no columns endpoint —
  // fall back to free-text input via allowCustomValue on the column selects.
  useEffect(() => {
    if (!q.dataset || q.dataset === ALL_DATASETS_SLUG) {
      const handle = setTimeout(() => setColumns([]), 0);
      return () => clearTimeout(handle);
    }
    let cancelled = false;
    const handle = setTimeout(() => {
      if (!cancelled) {
        setLoadingColumns(true);
      }
    }, 0);
    datasource
      .listColumns(q.dataset)
      .then((cols) => {
        if (!cancelled) {
          setColumns(cols);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setColumns([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingColumns(false);
        }
      });
    return () => {
      cancelled = true;
      clearTimeout(handle);
    };
  }, [q.dataset]); // eslint-disable-line react-hooks/exhaustive-deps

  const update = useCallback(
    (partial: Partial<HoneycombQuery>) => {
      onChange({ ...q, ...partial });
    },
    [q, onChange]
  );

  const handleRunQuery = () => onRunQuery();

  const handleOpenInHoneycomb = () => {
    if (honeycombQueryURL) {
      window.open(honeycombQueryURL, '_blank', 'noopener,noreferrer');
    }
  };

  const runActions = (
    <div className={styles.runRow}>
      <ToolbarButton variant="primary" onClick={handleRunQuery} icon="play">
        Run query
      </ToolbarButton>
      <ToolbarButton
        variant="canvas"
        icon="external-link-alt"
        disabled={!honeycombQueryURL}
        tooltip={
          honeycombQueryURL
            ? 'Open this query result in Honeycomb'
            : 'Run the query first to enable Open in Honeycomb'
        }
        onClick={handleOpenInHoneycomb}
      >
        Open in Honeycomb
      </ToolbarButton>
    </div>
  );

  // Build column options for selects from loaded column metadata.
  const columnOptions: Array<SelectableValue<string>> = columns
    .filter((c) => !c.hidden)
    .map((c) => ({
      label: c.key_name,
      value: c.key_name,
      description: c.type,
    }));

  // Resolve effective queryType: queryType field takes precedence, but legacy
  // dashboards may set rawMode=true without queryType — treat that as 'raw'.
  const effectiveQueryType: QueryType =
    (q.queryType as QueryType | undefined) ?? (q.rawMode ? 'raw' : 'metrics');

  const queryTypeOptions: Array<SelectableValue<QueryType>> = [
    { label: 'Metrics', value: 'metrics' },
    { label: 'Logs', value: 'logs' },
    { label: 'Traces', value: 'traces' },
    { label: 'SLO', value: 'slo' },
    { label: 'Raw Query', value: 'raw' },
  ];

  // Top header — Query Type and Dataset on a single row to keep the editor
  // compact (the labelled controls fit comfortably side-by-side).
  const header = (
    <InlineFieldRow>
      <InlineField label="Query Type" labelWidth={LABEL_WIDTH}>
        <RadioButtonGroup
          options={queryTypeOptions}
          value={effectiveQueryType}
          onChange={(v) => {
            update({ queryType: v, rawMode: v === 'raw' });
          }}
        />
      </InlineField>
      <InlineField label="Dataset" labelWidth={12} grow>
        {loadingDatasets ? (
          <Spinner />
        ) : (
          <Select
            options={datasets}
            value={q.dataset}
            onChange={(v) => update({ dataset: v.value ?? '' })}
            allowCustomValue
            placeholder="Choose dataset"
            width={32}
          />
        )}
      </InlineField>
    </InlineFieldRow>
  );

  // SLO editor branch.
  if (effectiveQueryType === 'slo') {
    return (
      <div className={styles.wrapper}>
        {header}
        <SLOEditor query={q} onChange={update} />
        {runActions}
      </div>
    );
  }

  // Logs editor branch — dedicated UX without calculations / breakdowns.
  if (effectiveQueryType === 'logs') {
    return (
      <div className={styles.wrapper}>
        {header}
        <LogsEditor query={q} columns={columns} onChange={update} onRunQuery={handleRunQuery} />
        {runActions}
      </div>
    );
  }

  // Traces editor branch — single-trace fetch or search-and-link.
  if (effectiveQueryType === 'traces') {
    return (
      <div className={styles.wrapper}>
        {header}
        <TracesEditor query={q} columns={columns} onChange={update} />
        {runActions}
      </div>
    );
  }

  // Raw query branch.
  if (effectiveQueryType === 'raw') {
    return (
      <div className={styles.wrapper}>
        {header}
        <Field
          label="Honeycomb Query JSON"
          description="Paste a raw Honeycomb Query API JSON object. Template variables are supported."
        >
          <TextArea
            rows={12}
            value={q.rawJson || ''}
            onChange={(e) => update({ rawJson: e.currentTarget.value })}
            onBlur={handleRunQuery}
            placeholder='{"calculations":[{"op":"COUNT"}],"breakdowns":["service.name"]}'
          />
        </Field>
        {runActions}
      </div>
    );
  }

  // Default: metrics (events) editor.
  return (
    <div className={styles.wrapper}>
      {header}

      {/* Query mode + Returned data on a single compact row */}
      <InlineFieldRow>
        <InlineField label="Query mode" labelWidth={LABEL_WIDTH}>
          <Select
            options={QUERY_MODE_OPTIONS}
            value={q.queryMode}
            onChange={(v) => update({ queryMode: (v.value as QueryMode) ?? 'timeseries' })}
            width={20}
          />
        </InlineField>
        <InlineField
          label="Returned data"
          labelWidth={16}
          tooltip="Which Honeycomb result fields to populate. 'auto' picks based on Query Mode."
        >
          <Select
            options={QUERY_RESULT_TYPE_OPTIONS}
            value={q.queryResultType ?? 'auto'}
            onChange={(v) => update({ queryResultType: (v.value as QueryResultType) ?? 'auto' })}
            width={20}
          />
        </InlineField>
      </InlineFieldRow>

      <Section label="Calculations" styles={styles}>
        <CalculationsEditor
          calculations={q.calculations ?? []}
          columnOptions={columnOptions}
          loadingColumns={loadingColumns}
          onChange={(calculations) => update({ calculations })}
        />
      </Section>

      <Section label="Filters" styles={styles}>
        <FiltersEditor
          filters={q.filters ?? []}
          filterCombination={q.filterCombination ?? 'AND'}
          columnOptions={columnOptions}
          onChange={(filters, filterCombination) => update({ filters, filterCombination })}
        />
      </Section>

      <Section label="Group by (Breakdowns)" styles={styles}>
        <GroupByEditor
          breakdowns={q.breakdowns ?? []}
          columnOptions={columnOptions}
          onChange={(breakdowns) => update({ breakdowns })}
        />
      </Section>

      <Section label="Order by" styles={styles}>
        <OrderByEditor
          orders={q.orders ?? []}
          calculations={q.calculations ?? []}
          breakdowns={q.breakdowns ?? []}
          onChange={(orders) => update({ orders })}
        />
      </Section>

      <Section label="Having (post-aggregation filters)" styles={styles}>
        <HavingsEditor
          havings={q.havings ?? []}
          calculations={q.calculations ?? []}
          onChange={(havings) => update({ havings })}
        />
      </Section>

      {/* Less-used options in a single collapsable row, mirroring Prometheus. */}
      <div className={styles.optionsWrapper}>
        <CollapsableSection
          label={
            <span className={styles.optionsLabel}>
              Options{' '}
              <span className={styles.optionsSummary}>
                Limit: {q.limit ?? 100}, Granularity: {q.granularity ? `${q.granularity}s` : 'auto'},
                Compare to: {compareLabel(q.compareTimeOffset)}
              </span>
            </span>
          }
          isOpen={false}
        >
          <InlineFieldRow>
            <InlineField label="Limit" labelWidth={LABEL_WIDTH} tooltip="Maximum number of result groups (1–10000)">
              <Input
                type="number"
                min={1}
                max={10000}
                value={q.limit ?? 100}
                onChange={(e) => update({ limit: parseInt(e.currentTarget.value, 10) || 100 })}
                onBlur={handleRunQuery}
                width={12}
              />
            </InlineField>
            <InlineField
              label="Granularity (s)"
              labelWidth={16}
              tooltip="Time resolution in seconds. 0 = auto-derive from panel time range."
            >
              <Input
                type="number"
                min={0}
                value={q.granularity ?? 0}
                onChange={(e) => update({ granularity: parseInt(e.currentTarget.value, 10) || 0 })}
                onBlur={handleRunQuery}
                placeholder="0 (auto)"
                width={12}
              />
            </InlineField>
            <InlineField
              label="Compare to"
              labelWidth={14}
              tooltip="Compare current time range to the same window N seconds ago. Honeycomb returns the comparison series alongside the main result."
            >
              <Select
                options={COMPARE_TIME_OFFSET_OPTIONS}
                value={q.compareTimeOffset ?? 0}
                onChange={(v) => update({ compareTimeOffset: v.value ?? 0 })}
                width={20}
              />
            </InlineField>
          </InlineFieldRow>
        </CollapsableSection>
      </div>

      {runActions}
    </div>
  );
}

const LABEL_WIDTH = 18;

function compareLabel(offset: number | undefined): string {
  if (!offset) {
    return 'None';
  }
  const opt = COMPARE_TIME_OFFSET_OPTIONS.find((o) => o.value === offset);
  return opt?.label ?? `${offset}s`;
}

interface SectionProps {
  label: string;
  styles: ReturnType<typeof getStyles>;
  children: React.ReactNode;
}

function Section({ label, styles, children }: SectionProps) {
  return (
    <div className={styles.section}>
      <div className={styles.sectionHeader}>{label}</div>
      {children}
    </div>
  );
}

function getStyles(theme: GrafanaTheme2) {
  return {
    wrapper: css`
      display: flex;
      flex-direction: column;
      gap: ${theme.spacing(0.5)};
    `,
    section: css`
      display: flex;
      flex-direction: column;
      gap: ${theme.spacing(0.25)};
      margin-top: ${theme.spacing(0.5)};
    `,
    sectionHeader: css`
      font-size: ${theme.typography.bodySmall.fontSize};
      font-weight: ${theme.typography.fontWeightMedium};
      color: ${theme.colors.text.secondary};
      text-transform: uppercase;
      letter-spacing: 0.04em;
    `,
    optionsWrapper: css`
      margin-top: ${theme.spacing(0.5)};
      border-top: 1px solid ${theme.colors.border.weak};
      padding-top: ${theme.spacing(0.5)};
    `,
    optionsLabel: css`
      font-size: ${theme.typography.body.fontSize};
    `,
    optionsSummary: css`
      color: ${theme.colors.text.secondary};
      font-weight: ${theme.typography.fontWeightRegular};
      margin-left: ${theme.spacing(1)};
    `,
    runRow: css`
      display: flex;
      justify-content: flex-end;
      gap: ${theme.spacing(1)};
      margin-top: ${theme.spacing(0.5)};
    `,
  };
}
