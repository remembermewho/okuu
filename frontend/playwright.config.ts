import { defineConfig, devices } from '@playwright/test'

// e2e на настоящем бэкенде: отдельная база okuu_e2e, API на :8091, фронтенд на :5174
// (APP_ORIGIN бэкенда = origin фронтенда). Dev-сервер на :8080/:5173 не трогаем.
const FRONT_PORT = 5174
const API_PORT = 8091
const baseURL = `http://localhost:${FRONT_PORT}`

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1, // одна база на прогон: сценарии идут по очереди
  retries: 0,
  reporter: [['list'], ['html', { outputFolder: 'playwright-report', open: 'never' }]],
  outputDir: 'test-results',
  use: {
    baseURL,
    locale: 'ru-RU',
    timezoneId: 'Asia/Bishkek',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'mobile', use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 }, isMobile: false, hasTouch: true } },
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } },
  ],
  webServer: [
    {
      command: 'node e2e/start-backend.mjs',
      url: `http://localhost:${API_PORT}/readyz`,
      timeout: 180_000,
      reuseExistingServer: false,
      stdout: 'ignore',
      stderr: 'pipe',
      env: { E2E_API_PORT: String(API_PORT), E2E_ORIGIN: baseURL },
    },
    {
      command: `npx vite --port ${FRONT_PORT} --strictPort`,
      url: baseURL,
      timeout: 120_000,
      reuseExistingServer: false,
      env: { VITE_API_TARGET: `http://localhost:${API_PORT}`, VITE_PORT: String(FRONT_PORT) },
    },
  ],
})
