import { expect, test } from '@grafana/plugin-e2e';

import pluginJson from '../../src/plugin.json';

test.describe('config editor', () => {
  test('renders the connection fields', async ({ createDataSourceConfigPage, page }) => {
    await createDataSourceConfigPage({ type: pluginJson.id });

    await expect(page.getByText('API Region')).toBeVisible();
    await expect(page.getByPlaceholder('your-honeycomb-api-key')).toBeVisible();
  });

  test('save & test reports a clear error for an invalid API key', async ({
    createDataSourceConfigPage,
    page,
  }) => {
    const configPage = await createDataSourceConfigPage({ type: pluginJson.id });

    await page.getByPlaceholder('your-honeycomb-api-key').fill('not-a-real-key');
    await configPage.saveAndTest();

    // The backend binary must start, call Honeycomb, and surface the auth
    // failure as a friendly message — not crash or time out.
    await expect(configPage).toHaveAlert('error');
  });
});
