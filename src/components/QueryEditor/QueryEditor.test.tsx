import { createDataFrame, LoadingState, PanelData } from '@grafana/data';

import { extractHoneycombQueryURL } from './QueryEditor';

function panelData(frames: Array<{ refId: string; custom?: Record<string, unknown> }>): PanelData {
  return {
    state: LoadingState.Done,
    series: frames.map(({ refId, custom }) =>
      createDataFrame({ refId, fields: [], meta: custom ? { custom } : undefined })
    ),
  } as unknown as PanelData;
}

describe('extractHoneycombQueryURL', () => {
  it('returns undefined when there is no data', () => {
    expect(extractHoneycombQueryURL(undefined, 'A')).toBeUndefined();
    expect(extractHoneycombQueryURL(panelData([]), 'A')).toBeUndefined();
  });

  it('returns the URL from a frame matching the refId', () => {
    const data = panelData([{ refId: 'A', custom: { honeycombQueryURL: 'https://ui.honeycomb.io/a' } }]);
    expect(extractHoneycombQueryURL(data, 'A')).toBe('https://ui.honeycomb.io/a');
  });

  // In a multi-query panel, data.series holds every query's frames. Query B's
  // editor must not pick up query A's URL just because A's frame comes first.
  it('ignores frames belonging to other queries in the panel', () => {
    const data = panelData([
      { refId: 'A', custom: { honeycombQueryURL: 'https://ui.honeycomb.io/a' } },
      { refId: 'B', custom: { honeycombQueryURL: 'https://ui.honeycomb.io/b' } },
    ]);
    expect(extractHoneycombQueryURL(data, 'B')).toBe('https://ui.honeycomb.io/b');
    expect(extractHoneycombQueryURL(data, 'C')).toBeUndefined();
  });

  it('skips frames without a usable URL', () => {
    const data = panelData([
      { refId: 'A' },
      { refId: 'A', custom: { honeycombQueryURL: '   ' } },
      { refId: 'A', custom: { honeycombQueryURL: 42 } },
      { refId: 'A', custom: { honeycombQueryURL: 'https://ui.honeycomb.io/a' } },
    ]);
    expect(extractHoneycombQueryURL(data, 'A')).toBe('https://ui.honeycomb.io/a');
  });
});
