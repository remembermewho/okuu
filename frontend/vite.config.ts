/// <reference types="vitest/config" />
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// API в dev — бэкенд на :8080 (его APP_ORIGIN = http://localhost:5173 — адрес этого dev-сервера).
// e2e поднимает свой бэкенд и подменяет адрес через VITE_API_TARGET.
const apiTarget = process.env.VITE_API_TARGET ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: Number(process.env.VITE_PORT ?? 5173),
    strictPort: true,
    proxy: {
      // Origin браузера уходит как есть — бэкенд сверяет его с APP_ORIGIN (CSRF).
      '/api': { target: apiTarget, changeOrigin: true },
      '/webhooks': { target: apiTarget, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      output: {
        // Библиотеки — отдельными файлами: меняются реже кода приложения и дольше живут в кэше браузера.
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          if (/node_modules[\\/](react|react-dom|scheduler|react-router)[\\/]/.test(id)) return 'react'
          if (id.includes('@fontsource')) return undefined
          return 'vendor'
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: { modules: { classNameStrategy: 'non-scoped' } },
  },
})
