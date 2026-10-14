import { defineConfig } from '@playwright/test';
export default defineConfig({ testDir: '.', testMatch: ['shell.spec.ts', 'src/lib/ask/*.spec.ts'], use: { baseURL: 'http://127.0.0.1:4173', viewport: { width: 360, height: 800 } }, webServer: { command: 'node static-server.js', url: 'http://127.0.0.1:4173', reuseExistingServer: false } });
