import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Unmount everything rendered by a test before the next one starts.
afterEach(() => {
  cleanup()
})
