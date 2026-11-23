import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

import { installFocusVisible, resetFocusVisible } from './focusVisible'

// jsdom lays nothing out, so it has no scrollIntoView; a no-op stands in, and a test that
// cares spies on it.
Element.prototype.scrollIntoView = function scrollIntoView() {
  // Nothing to scroll without a layout.
}

// jsdom has no ResizeObserver either, and nothing it lays out ever changes size; one that
// never reports stands in, and a test that cares reports through its own.
globalThis.ResizeObserver = class ResizeObserver {
  observe() {
    // Nothing changes size without a layout.
  }

  unobserve() {
    // Nothing to stop following.
  }

  disconnect() {
    // Nothing to stop following.
  }
}

// jsdom has the <dialog> element but none of its methods. These stand-ins open and close it,
// which is all a test can observe: the top layer and the inert page behind a modal need a
// real browser.
HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
  this.setAttribute('open', '')
}

HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
  if (this.hasAttribute('open')) {
    this.removeAttribute('open')
    this.dispatchEvent(new Event('close'))
  }
}

// jsdom rings all focus, a click's too; the stand-in rings it as a browser does
// (src/test/focusVisible.ts).
installFocusVisible()

// Unmount everything rendered by a test before the next one starts, then put the real
// timers back, so fake timers a test turned on end with it, after its unmount has cleared
// its own, and forget the input that decided what focus rings. Mocks and stubbed globals
// are put back by mockReset, restoreMocks, and unstubGlobals in vitest.config.ts.
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  resetFocusVisible()
})
