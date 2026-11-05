import React from 'react';
import { SelectableValue } from '@grafana/data';
import { Button, IconButton, InlineField, InlineFieldRow, Input, Select } from '@grafana/ui';

import {
  Calculation,
  CalculationOp,
  calculationOpsFor,
  COLUMN_OPTIONAL_OPS,
  COLUMN_REQUIRED_OPS,
  DatasetKind,
} from '../../types';

interface Props {
  calculations: Calculation[];
  columnOptions: Array<SelectableValue<string>>;
  loadingColumns: boolean;
  datasetKind?: DatasetKind;
  onChange: (calculations: Calculation[]) => void;
}

/**
 * CalculationsEditor renders a list of calculation rows (op + optional column + optional alias).
 * Users can add/remove calculations and reorder them.
 */
export function CalculationsEditor({ calculations, columnOptions, loadingColumns, datasetKind = 'unknown', onChange }: Props) {
  const add = () => {
    onChange([...calculations, { op: datasetKind === 'metrics' ? 'COUNT_DATAPOINTS' : 'COUNT' }]);
  };

  const remove = (idx: number) => {
    onChange(calculations.filter((_, i) => i !== idx));
  };

  const updateCalc = (idx: number, partial: Partial<Calculation>) => {
    const next = calculations.map((c, i) => (i === idx ? { ...c, ...partial } : c));
    onChange(next);
  };

  return (
    <div>
      {calculations.map((calc, idx) => {
        const calcOp = calc.op as CalculationOp;
        const takesColumn = COLUMN_REQUIRED_OPS.has(calcOp) || COLUMN_OPTIONAL_OPS.has(calcOp);
        return (
          <InlineFieldRow key={idx}>
            <InlineField label={idx === 0 ? 'Operation' : ''} labelWidth={10}>
              <Select
                options={calculationOpsFor(datasetKind, [calcOp])}
                value={calc.op}
                onChange={(v) => {
                  const op = (v.value ?? 'COUNT') as CalculationOp;
                  const update: Partial<Calculation> = { op };
                  // Clear column if the new op doesn't need one.
                  if (!COLUMN_REQUIRED_OPS.has(op) && !COLUMN_OPTIONAL_OPS.has(op)) {
                    update.column = undefined;
                  }
                  updateCalc(idx, update);
                }}
                width={16}
              />
            </InlineField>

            {takesColumn && (
              <InlineField label={idx === 0 ? 'Column' : ''} labelWidth={8}>
                <Select
                  // Honeycomb rejects HISTOGRAM_COUNT on any column that is not a histogram.
                  options={calcOp === 'HISTOGRAM_COUNT' ? columnOptions.filter((o) => o.type === 'histogram') : columnOptions}
                  value={calc.column}
                  onChange={(v) => updateCalc(idx, { column: v.value })}
                  allowCustomValue
                  placeholder="column name"
                  isLoading={loadingColumns}
                  width={20}
                />
              </InlineField>
            )}

            <InlineField label={idx === 0 ? 'Alias' : ''} labelWidth={8} tooltip="Optional display name for this metric">
              <Input
                placeholder="optional alias"
                value={calc.alias || ''}
                onChange={(e) => updateCalc(idx, { alias: e.currentTarget.value || undefined })}
                width={16}
              />
            </InlineField>

            <IconButton
              name="trash-alt"
              tooltip="Remove calculation"
              onClick={() => remove(idx)}
              disabled={calculations.length === 1}
            />
          </InlineFieldRow>
        );
      })}

      <Button
        variant="secondary"
        size="sm"
        icon="plus"
        onClick={add}
        disabled={calculations.length >= 100}
      >
        Add calculation
      </Button>
    </div>
  );
}
