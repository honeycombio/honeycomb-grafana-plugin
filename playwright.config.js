// @ts-check
// Plain JS (not TS) so Playwright does not try to parse the repo's tsconfig
// chain, which extends the packaged @grafana/tsconfig that Playwright's
// lightweight tsconfig resolver cannot follow.
const { dirname } = require('node:path');
const { defineConfig, devices } = require('@playwright/test');

// Auth setup project provided by @grafana/plugin-e2e: logs in as admin and
// stores session state for the test projects.
const pluginE2eAuth = `${dirname(require.resolve('@grafana/plugin-e2e'))}/auth`;

module.exports = defineConfig({
  testDir: './tests/e2e',
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? [['html', { open: 'never' }], ['github']] : 'html',
  use: {
    baseURL: process.env.GRAFANA_URL || 'http://localhost:3000',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'auth',
      testDir: pluginE2eAuth,
      testMatch: [/.*\.setup\.(js|ts)/],
    },
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        storageState: 'playwright/.auth/admin.json',
      },
      dependencies: ['auth'],
    },
  ],
});
