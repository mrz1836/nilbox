import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// jsdom lays nothing out, so it has no scrollIntoView; a no-op stands in, and a test that
// cares spies on it.
Element.prototype.scrollIntoView = function scrollIntoView() {
  // Nothing to scroll without a layout.
}

// Unmount everything rendered by a test before the next one starts.
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
