import { defineConfig } from '@playwright/test';

// Expects a running dev server: ./server/reqbased -config reqbase.dev.yml -dev
export default defineConfig({
  testDir: './e2e',
  timeout: 45_000,
  retries: 0,
  workers: 1, // flows share one git workspace — keep them sequential
  use: {
    baseURL: process.env.REQBASE_URL || 'http://127.0.0.1:8643',
    viewport: { width: 1440, height: 880 },
  },
});
