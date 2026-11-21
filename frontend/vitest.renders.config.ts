import babel from '@rolldown/plugin-babel'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import { defineConfig, mergeConfig } from 'vitest/config'

import { shared } from './vitest.config.js'

// The render profile (src/App.renders.test.tsx) through the React Compiler, as the build
// ships the components: the compiler's memoization decides how much of the inbox a change
// renders again. It runs apart from the unit tests, whose coverage would otherwise measure
// the compiler's output rather than the source, and prints what it measured. Its mocks
// and globals start fresh for each test as the unit tests' do (shared).
export default mergeConfig(
  shared,
  defineConfig({
    plugins: [react(), babel({ presets: [reactCompilerPreset()] })],
    test: {
      environment: 'jsdom',
      include: ['src/App.renders.test.tsx'],
      setupFiles: ['./src/test/setup.ts'],
      env: { TZ: 'UTC', REACT_COMPILER: '1' },
      silent: false,
    },
  }),
)
