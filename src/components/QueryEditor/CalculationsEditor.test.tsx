import React from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { CalculationsEditor } from './CalculationsEditor';
import { CalculationOp, calculationOpsFor } from '../../types';

function renderEditor(op: CalculationOp) {
  render(<CalculationsEditor calculations={[{ op }]} columnOptions={[]} loadingColumns={false} onChange={() => {}} />);
}

describe('CalculationsEditor column input', () => {
  it.each<[CalculationOp, boolean]>([
    ['HISTOGRAM_COUNT', true], // column required
    ['COUNT_DATAPOINTS', true], // column optional
    ['COUNT', false],
  ])('%s shows the column input: %s', (op, shown) => {
    renderEditor(op);
    expect(screen.queryByText('Column') !== null).toBe(shown);
  });
});

describe('calculationOpsFor', () => {
  const values = (kind: Parameters<typeof calculationOpsFor>[0], keep: CalculationOp[] = []) =>
    calculationOpsFor(kind, keep).map((o) => o.value);

  it('offers metrics-only ops and hides event-only ops on metrics datasets', () => {
    const ops = values('metrics');
    expect(ops).toEqual(expect.arrayContaining(['COUNT_DATAPOINTS', 'HISTOGRAM_COUNT', 'SUM', 'P99', 'HEATMAP']));
    expect(ops).not.toEqual(expect.arrayContaining(['COUNT']));
    expect(ops).not.toContain('CONCURRENCY');
    expect(ops).not.toContain('RATE_SUM');
  });

  it('hides metrics-only ops on event datasets', () => {
    const ops = values('events');
    expect(ops).toEqual(expect.arrayContaining(['COUNT', 'CONCURRENCY', 'RATE_SUM', 'P99']));
    expect(ops).not.toContain('COUNT_DATAPOINTS');
    expect(ops).not.toContain('HISTOGRAM_COUNT');
  });

  it('offers everything when the dataset kind is unknown', () => {
    expect(values('unknown')).toHaveLength(calculationOpsFor('unknown').length);
    expect(values('unknown')).toEqual(expect.arrayContaining(['COUNT', 'COUNT_DATAPOINTS']));
  });

  it('keeps an already selected op visible even when it is invalid for the dataset', () => {
    expect(values('metrics', ['RATE_SUM'])).toContain('RATE_SUM');
  });
});

describe('CalculationsEditor HISTOGRAM_COUNT column choices', () => {
  // jsdom lacks IntersectionObserver, which Grafana's open select menu uses.
  beforeAll(() => {
    window.IntersectionObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof IntersectionObserver;
  });

  const columnOptions = [
    { label: 'app.ads.ad_requests', value: 'app.ads.ad_requests', type: 'integer' },
    { label: 'http.server.duration', value: 'http.server.duration', type: 'histogram' },
  ];

  async function openColumns(op: CalculationOp) {
    render(
      <CalculationsEditor calculations={[{ op }]} columnOptions={columnOptions} loadingColumns={false} onChange={() => {}} />
    );
    await userEvent.click(screen.getAllByRole('combobox')[1]); // [0] is the operation select
  }

  it('lists only histogram columns', async () => {
    await openColumns('HISTOGRAM_COUNT');
    expect(screen.getByText('http.server.duration')).toBeInTheDocument();
    expect(screen.queryByText('app.ads.ad_requests')).not.toBeInTheDocument();
  });

  it('lists every column for other ops', async () => {
    await openColumns('SUM');
    expect(screen.getByText('http.server.duration')).toBeInTheDocument();
    expect(screen.getByText('app.ads.ad_requests')).toBeInTheDocument();
  });
});
