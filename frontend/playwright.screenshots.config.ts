import { defineConfig, devices } from '@playwright/test'
import { SHOTS } from './e2e/screenshots/demo'

// npm run screenshots — картинки для лендинга (landing/img, webp) с демо-центра:
// отдельная база okuu_screens, `server demo seed`, API на :8091, фронтенд на :5175.
const baseURL = `http://localhost:${SHOTS.frontPort}`

export default defineConfig({
  testDir: './e2e/screenshots',
  testMatch: '*.shot.ts',
  timeout: 120_000,
  workers: 1,
  retries: 0,
  reporter: 'list',
  outputDir: 'test-results/screenshots',
  use: { ...devices['Desktop Chrome'], baseURL, locale: 'ru-RU', timezoneId: 'Asia/Bishkek' },
  webServer: [
    {
      command: 'node e2e/start-backend.mjs',
      url: `http://localhost:${SHOTS.apiPort}/readyz`,
      timeout: 300_000,
      reuseExistingServer: false,
      stdout: 'ignore',
      stderr: 'pipe',
      env: {
        E2E_API_PORT: String(SHOTS.apiPort),
        E2E_ORIGIN: baseURL,
        E2E_DB: SHOTS.db,
        E2E_DEMO_PHONE: SHOTS.phone,
        E2E_DEMO_PASSWORD: SHOTS.password,
      },
    },
    {
      command: `npx vite --port ${SHOTS.frontPort} --strictPort`,
      url: baseURL,
      timeout: 120_000,
      reuseExistingServer: false,
      env: { VITE_API_TARGET: `http://localhost:${SHOTS.apiPort}`, VITE_PORT: String(SHOTS.frontPort) },
    },
  ],
})
