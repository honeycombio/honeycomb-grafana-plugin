import React from 'react';
import { SelectableValue } from '@grafana/data';
import { Button, IconButton, InlineField, InlineFieldRow, Input, RadioButtonGroup, Select } from '@grafana/ui';

import { Filter, FilterCombination, FilterOp, FILTER_OPS, NO_VALUE_FILTER_OPS } from '../../types';

interface Props {
  filters: Filter[];
  filterCombination: FilterCombination;
  columnOptions: Array<SelectableValue<string>>;
  onChange: (filters: Filter[], filterCombination: FilterCombination) => void;
}

const COMBINATION_OPTIONS = [
  { label: 'AND', value: 'AND' as FilterCombination },
  { label: 'OR', value: 'OR' as FilterCombination },
];

/**
 * FiltersEditor renders the filter list with AND/OR combination control.
 *
 * Honeycomb supports a single top-level filter_combination (AND or OR) that
 * applies to all filters. Per-calculation filters are an advanced feature
 * available only via raw JSON mode.
 */
export function FiltersEditor({ filters, filterCombination, columnOptions, onChange }: Props) {
  const update = (nextFilters: Filter[], nextCombination?: FilterCombination) => {
    onChange(nextFilters, nextCombination ?? filterCombination);
  };

  const add = () => {
    update([...filters, { column: '', op: '=', value: '' }]);
  };

  const remove = (idx: number) => {
    update(filters.filter((_, i) => i !== idx));
  };

  const updateFilter = (idx: number, partial: Partial<Filter>) => {
    update(filters.map((f, i) => (i === idx ? { ...f, ...partial } : f)));
  };

  return (
    <div>
      {filters.length > 1 && (
        <InlineFieldRow>
          <InlineField label="Combine filters" labelWidth={16}>
            <RadioButtonGroup
              options={COMBINATION_OPTIONS}
              value={filterCombination}
              onChange={(v) => update(filters, v as FilterCombination)}
            />
          </InlineField>
        </InlineFieldRow>
      )}

      {filters.map((filter, idx) => {
        const noValue = NO_VALUE_FILTER_OPS.has(filter.op as FilterOp);
        const isCollectionOp = filter.op === 'in' || filter.op === 'not-in';
        return (
          <InlineFieldRow key={idx}>
            <InlineField label={idx === 0 ? 'Column' : ''} labelWidth={8}>
              <Select
                options={columnOptions}
                value={filter.column}
                onChange={(v) => updateFilter(idx, { column: v.value ?? '' })}
                allowCustomValue
                placeholder="column name"
                width={20}
              />
            </InlineField>

            <InlineField label={idx === 0 ? 'Op' : ''} labelWidth={6}>
              <Select
                options={FILTER_OPS}
                value={filter.op}
                onChange={(v) => {
                  const op = (v.value ?? '=') as FilterOp;
                  updateFilter(idx, { op, value: NO_VALUE_FILTER_OPS.has(op) ? undefined : filter.value });
                }}
                width={18}
              />
            </InlineField>

            {!noValue && (
              <InlineField label={idx === 0 ? 'Value' : ''} labelWidth={6}>
                <Input
                  placeholder={isCollectionOp ? 'a,b,c' : 'value'}
                  value={String(filter.value ?? '')}
                  onChange={(e) => updateFilter(idx, { value: e.currentTarget.value })}
                  width={16}
                />
              </InlineField>
            )}

            <IconButton
              name="trash-alt"
              tooltip="Remove filter"
              onClick={() => remove(idx)}
            />
          </InlineFieldRow>
        );
      })}

      <Button variant="secondary" size="sm" icon="plus" onClick={add} disabled={filters.length >= 100}>
        Add filter
      </Button>
    </div>
  );
}
