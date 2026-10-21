import js from '@eslint/js'
import globals from 'globals'
import tseslint from 'typescript-eslint'
import react from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'

export default tseslint.config(
  { ignores: ['dist', 'playwright-report', 'test-results', 'src/api/schema.d.ts'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: { ecmaVersion: 2023, globals: { ...globals.browser, ...globals.node } },
    plugins: { react, 'react-hooks': reactHooks, 'react-refresh': reactRefresh },
    settings: { react: { version: 'detect' } },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // Хуки и константы рядом с компонентами — осознанно; HMR от этого лишь перезагружает модуль.
      'react-refresh/only-export-components': 'off',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      // Все тексты интерфейса — в src/i18n/ru.ts: в JSX не должно быть строк.
      'react/jsx-no-literals': ['error', { noStrings: false, allowedStrings: ['·', '—', '–', '/', '%', '×', ':', '+', '−', '(', ')'], ignoreProps: true }],
    },
  },
  {
    // Тесты и e2e могут искать элементы по тексту.
    files: ['**/*.test.{ts,tsx}', 'e2e/**/*.ts', 'src/test/**/*.{ts,tsx}'],
    rules: { 'react/jsx-no-literals': 'off' },
  },
)
