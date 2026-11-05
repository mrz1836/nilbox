import js from '@eslint/js'
import { defineConfig, globalIgnores } from 'eslint/config'
import jsxA11y from 'eslint-plugin-jsx-a11y'
import react from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
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
])
