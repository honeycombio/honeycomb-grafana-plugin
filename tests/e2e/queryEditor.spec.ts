import { expect, test } from '@grafana/plugin-e2e';

import pluginJson from '../../src/plugin.json';

test.describe('query editor', () => {
  test('renders for a new panel without errors', async ({
    createDataSource,
    panelEditPage,
    page,
  }) => {
    const datasource = await createDataSource({ type: pluginJson.id });
    await panelEditPage.datasource.set(datasource.name);

    // The query editor should render its builder UI even before a dataset
    // is chosen (metadata calls will fail with the unset API key, but the
    // editor must not crash). Older Grafana versions re-bind the query row
    // to the new datasource asynchronously, so allow a generous timeout.
    await expect(page.getByText('Dataset').first()).toBeVisible({ timeout: 15000 });
  });
});
