import babel from '@rolldown/plugin-babel'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The render profile (src/App.renders.test.tsx) through the React Compiler, as the build
// ships the components: the compiler's memoization decides how much of the inbox a change
// renders again. It runs apart from the unit tests, whose coverage would otherwise measure
// the compiler's output rather than the source, and prints what it measured.
export default defineConfig({
  plugins: [react(), babel({ presets: [reactCompilerPreset()] })],
  test: {
    environment: 'jsdom',
    include: ['src/App.renders.test.tsx'],
    setupFiles: ['./src/test/setup.ts'],
    env: { TZ: 'UTC', REACT_COMPILER: '1' },
    silent: false,
  },
})
