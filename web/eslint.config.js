// Minimal lint for the UI (npm run lint): ESLint's recommended rules, typescript-eslint's
// recommended set, and the React hooks rules the existing eslint-disable comments refer to.
import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist', 'e2e-results', 'node_modules', 'playwright-report'] },
  js.configs.recommended,
  tseslint.configs.recommended,
  {
    rules: { '@typescript-eslint/no-unused-vars': ['error', { ignoreRestSiblings: true }] },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    plugins: { 'react-hooks': reactHooks },
    languageOptions: { globals: globals.browser },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      'react-hooks/exhaustive-deps': 'warn',
    },
  },
  {
    files: ['e2e/**/*.ts', 'e2e-daemon/**/*.ts', 'scripts/**/*.mjs', '*.config.ts', '*.config.js', 'playwright.port.ts'],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
    // Playwright fixtures are written `async ({}, use)`.
    rules: { 'no-empty-pattern': 'off' },
  },
);
