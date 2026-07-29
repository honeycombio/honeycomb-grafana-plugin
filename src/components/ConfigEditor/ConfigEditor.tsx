import React, { ChangeEvent } from 'react';
import { DataSourcePluginOptionsEditorProps, SelectableValue } from '@grafana/data';
import { Field, Input, SecretInput, Select, FieldSet, Alert, CollapsableSection } from '@grafana/ui';

import {
  HoneycombDataSourceOptions,
  HoneycombSecureJsonData,
  DEFAULT_API_URL,
  EU_API_URL,
  DEFAULT_TIME_WINDOW_DAYS,
} from '../../types';

type Props = DataSourcePluginOptionsEditorProps<HoneycombDataSourceOptions, HoneycombSecureJsonData>;

const API_URL_OPTIONS: Array<SelectableValue<string>> = [
  { label: 'US (api.honeycomb.io)', value: DEFAULT_API_URL },
  { label: 'EU (api.eu1.honeycomb.io)', value: EU_API_URL },
  { label: 'Custom', value: 'custom' },
];

/**
 * ConfigEditor is shown on the data source configuration page in Grafana.
 *
 * The API key is stored in secureJsonData and never returned to the browser
 * after being saved. The Grafana backend encrypts it at rest.
 */
export function ConfigEditor({ options, onOptionsChange }: Props) {
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const apiUrl = jsonData.apiUrl || DEFAULT_API_URL;
  const isCustomUrl = apiUrl !== DEFAULT_API_URL && apiUrl !== EU_API_URL;
  const selectedPreset = isCustomUrl ? 'custom' : apiUrl;

  const onApiUrlPresetChange = (selected: SelectableValue<string>) => {
    if (selected.value !== 'custom') {
      onOptionsChange({
        ...options,
        jsonData: { ...jsonData, apiUrl: selected.value },
      });
    }
  };

  const onCustomUrlChange = (e: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, apiUrl: e.target.value },
    });
  };

  const onApiKeyChange = (e: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: { ...secureJsonData, apiKey: e.target.value },
    });
  };

  const onApiKeyReset = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, apiKey: false },
      secureJsonData: { ...secureJsonData, apiKey: '' },
    });
  };

  const onCacheTTLChange = (field: keyof HoneycombDataSourceOptions) => (e: ChangeEvent<HTMLInputElement>) => {
    const val = parseInt(e.target.value, 10);
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, [field]: isNaN(val) ? undefined : val },
    });
  };

  const onTeamChange = (e: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, team: e.target.value || undefined },
    });
  };

  const onEnvironmentChange = (e: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, environment: e.target.value || undefined },
    });
  };

  const onTimeWindowDaysChange = (e: ChangeEvent<HTMLInputElement>) => {
    const n = parseInt(e.target.value, 10);
    onOptionsChange({
      ...options,
      jsonData: {
        ...jsonData,
        timeWindowDays: Number.isFinite(n) && n >= 0 ? n : undefined,
      },
    });
  };

  return (
    <div>
      <FieldSet label="Connection">
        <Field
          label="API Region"
          description="Select your Honeycomb account region, or enter a custom API URL."
        >
          <Select
            options={API_URL_OPTIONS}
            value={selectedPreset}
            onChange={onApiUrlPresetChange}
            width={32}
          />
        </Field>

        {isCustomUrl && (
          <Field label="Custom API URL">
            <Input
              placeholder="https://api.honeycomb.io"
              value={apiUrl}
              onChange={onCustomUrlChange}
              width={40}
            />
          </Field>
        )}

        <Field
          label="API Key"
          description={
            <>
              Your Honeycomb Configuration API Key with{' '}
              <strong>Manage Queries and Columns</strong> and <strong>Run Queries</strong> permissions.{' '}
              The key is stored encrypted and is never returned to the browser.
            </>
          }
        >
          <SecretInput
            isConfigured={Boolean(secureJsonFields?.apiKey)}
            value={secureJsonData?.apiKey || ''}
            placeholder="your-honeycomb-api-key"
            width={40}
            onReset={onApiKeyReset}
            onChange={onApiKeyChange}
          />
        </Field>
      </FieldSet>

      <FieldSet label="Environment">
        <Field
          label="Team"
          description="Honeycomb team slug — used in deep links to ui.honeycomb.io."
        >
          <Input
            placeholder="my-team"
            value={jsonData.team || ''}
            onChange={onTeamChange}
            width={32}
          />
        </Field>

        <Field
          label="Environment"
          description="Honeycomb environment name. Leave blank for Classic accounts."
        >
          <Input
            placeholder="production"
            value={jsonData.environment || ''}
            onChange={onEnvironmentChange}
            width={32}
          />
        </Field>
      </FieldSet>

      <FieldSet label="Advanced">
        <Field
          label="Time Window (days)"
          description="Maximum query time window in days. Longer ranges are clamped before sending to Honeycomb. 0 = unbounded."
        >
          <Input
            type="number"
            min={0}
            placeholder={String(DEFAULT_TIME_WINDOW_DAYS)}
            value={jsonData.timeWindowDays ?? ''}
            onChange={onTimeWindowDaysChange}
            width={12}
          />
        </Field>
      </FieldSet>

      <Alert title="API Rate Limits" severity="info">
        Honeycomb limits query execution to <strong>10 requests per minute per team</strong>. This
        plugin uses multi-level caching and a token-bucket rate limiter to stay within this limit.
        Dashboards with many panels sharing the same query will automatically coalesce requests. See
        the{' '}
        <a
          href="https://github.com/honeycombio/honeycomb-grafana-plugin/blob/main/docs/caching.md"
          target="_blank"
          rel="noopener noreferrer"
        >
          caching documentation
        </a>{' '}
        for details.
      </Alert>

      <CollapsableSection label="Cache Settings" isOpen={false}>
        <Field
          label="L1 TTL — Query ID cache (minutes)"
          description="How long to cache the mapping from query shape to Honeycomb query_id. Honeycomb queries are immutable, so this avoids re-creating the same query definition. Does not count against rate limits."
        >
          <Input
            type="number"
            min={1}
            value={jsonData.cacheTtlL1Minutes ?? 30}
            onChange={onCacheTTLChange('cacheTtlL1Minutes')}
            placeholder="30"
            width={10}
          />
        </Field>

        <Field
          label="L2 TTL — Query Result ID cache (minutes)"
          description="How long to cache the query_result_id for a submitted query. Prevents re-submitting to the rate-limited Create Query Result endpoint (10 req/min per team). Higher values reduce rate-limited calls but may return staler result pointers."
        >
          <Input
            type="number"
            min={1}
            value={jsonData.cacheTtlL2Minutes ?? 10}
            onChange={onCacheTTLChange('cacheTtlL2Minutes')}
            placeholder="10"
            width={10}
          />
        </Field>

        <Field
          label="L3 TTL — Completed Result cache (minutes)"
          description="How long to cache completed query results. Completed results are immutable in Honeycomb (Honeycomb allows up to 24 hours). Higher values mean zero API calls for repeated queries but data won't reflect new events until the cache expires."
        >
          <Input
            type="number"
            min={1}
            value={jsonData.cacheTtlL3Minutes ?? 120}
            onChange={onCacheTTLChange('cacheTtlL3Minutes')}
            placeholder="120"
            width={10}
          />
        </Field>
      </CollapsableSection>
    </div>
  );
}
