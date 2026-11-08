import js from '@eslint/js'
import vitest from '@vitest/eslint-plugin'
import { defineConfig, globalIgnores } from 'eslint/config'
import jsxA11y from 'eslint-plugin-jsx-a11y'
import react from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
import testingLibrary from 'eslint-plugin-testing-library'
import globals from 'globals'
import tseslint from 'typescript-eslint'

// Ways to write raw HTML into the DOM. The UI renders through React only, so these are
// banned outright: they are the usual route for untrusted text to become markup.
const rawHtmlProperties = ['innerHTML', 'outerHTML', 'insertAdjacentHTML']

const rawHtmlMessage = 'Render through React; never write raw HTML into the DOM.'

export default defineConfig([
  globalIgnores(['dist', 'bindings', 'src/generated', 'coverage']),

  // Rules for every file.
  {
    extends: [js.configs.recommended],
    rules: {
      'no-console': ['error', { allow: ['warn', 'error'] }],
      'no-eval': 'error',
      'no-implied-eval': 'error',
      'no-new-func': 'error',
    },
  },

  // Config files run in Node.
  {
    files: ['*.{js,ts}'],
    languageOptions: { globals: globals.node },
  },

  // TypeScript and React sources, type-checked.
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      tseslint.configs.strictTypeChecked,
      react.configs.flat.recommended,
      react.configs.flat['jsx-runtime'],
      reactHooks.configs.flat['recommended-latest'],
      jsxA11y.flatConfigs.strict,
    ],
    languageOptions: {
      globals: globals.browser,
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    settings: { react: { version: 'detect' } },
    rules: {
      'react/no-danger': 'error',
      'no-restricted-properties': [
        'error',
        ...rawHtmlProperties.map((property) => ({ property, message: rawHtmlMessage })),
        { object: 'document', property: 'write', message: rawHtmlMessage },
        { object: 'document', property: 'writeln', message: rawHtmlMessage },
      ],
      'no-restricted-syntax': [
        'error',
        {
          selector: `MemberExpression[computed=true][property.value=/^(${rawHtmlProperties.join('|')})$/]`,
          message: rawHtmlMessage,
        },
      ],
    },
  },

  // The UI reaches the desktop runtime and the generated bindings only through
  // src/platform, so a different shell or OS port swaps that module, never its callers.
  {
    files: ['src/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            { group: ['@wailsio/*'], message: 'Import the desktop runtime through src/platform.' },
            { group: ['**/bindings/**'], message: 'Import the generated bindings through src/platform.' },
          ],
        },
      ],
    },
  },
  {
    files: ['src/platform/**'],
    rules: { 'no-restricted-imports': 'off' },
  },

  // Tests: Vitest's and Testing Library's rules catch tests that pass without checking
  // anything, focused or skipped tests, and queries that don't reflect what a user sees.
  {
    files: ['src/**/*.test.{ts,tsx}', 'src/test/**'],
    extends: [vitest.configs.recommended, testingLibrary.configs['flat/react']],
    rules: {
      // Helpers such as row() and section() return within() queries, which the rule
      // only recognizes when within() is called at the query site.
      'testing-library/prefer-screen-queries': 'off',
      // Tests also pin decorative, aria-hidden markup (the brand mark, icons, hover-reveal
      // wrappers) that no accessible query can reach.
      'testing-library/no-container': 'off',
      'testing-library/no-node-access': 'off',
    },
  },
  {
    // Vitest runs without globals, so Testing Library cannot register its own cleanup;
    // the shared setup file unmounts each test's render by hand.
    files: ['src/test/setup.ts'],
    rules: { 'testing-library/no-manual-cleanup': 'off' },
  },
])
