import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The tests run the components as written, without the React Compiler pass the build
// adds: its memo caches add branches a single render never takes, so coverage would
// measure the compiler's output rather than the source.
export default defineConfig({
  plugins: [react()],
  test: {
    // Every test starts with fresh mocks and globals: call history is cleared, every mock
    // answers as it was made to again (the platform fake's defaults, in
    // src/test/platform.tsx), vi.spyOn spies are put back, and every global vi.stubGlobal
    // replaced is restored (src/test/stubs.ts), so no test depends on what an earlier one
    // left behind. src/test/setup.ts puts the real timers back after each test.
    mockReset: true,
    restoreMocks: true,
    unstubGlobals: true,
    projects: [
      {
        extends: true,
        test: {
          name: 'unit',
          environment: 'jsdom',
          include: ['src/**/*.test.{ts,tsx}'],
          setupFiles: ['./src/test/setup.ts'],
          // Calendar words such as "tomorrow" depend on the time zone; tests read them in UTC
          // on every machine.
          env: { TZ: 'UTC' },
        },
      },
    ],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/generated/**', 'src/test/**', 'src/main.tsx'],
      reporter: ['text', 'lcov'],
      reportsDirectory: 'coverage',
      // The floor is the measured coverage rounded down, so a change that leaves new code
      // untested fails the gate. Raise it as coverage grows.
      thresholds: { statements: 99, branches: 96, functions: 99, lines: 99 },
    },
  },
})
