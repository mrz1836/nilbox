import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// jsdom lays nothing out, so it has no scrollIntoView; a no-op stands in, and a test that
// cares spies on it.
Element.prototype.scrollIntoView = function scrollIntoView() {
  // Nothing to scroll without a layout.
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

// Unmount everything rendered by a test before the next one starts. Mocks are reset by
// clearMocks and restoreMocks in vitest.config.ts.
afterEach(() => {
  cleanup()
})
