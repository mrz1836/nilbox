import { act } from '@testing-library/react'
import { vi } from 'vitest'

// Stand-ins for what jsdom lacks or answers one way only. Each stubs a global with
// vi.stubGlobal, and the configuration's unstubGlobals puts it back before the next test.

// reduceMotion makes the system ask for less motion: matchMedia matches the reduced-motion
// query, and no other.
export function reduceMotion(): void {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)' }))
}

// Resizes are the ResizeObservers made since followResizes: their callbacks, which a test
// calls as a layout change would, and what they follow now.
export interface Resizes {
  // callbacks are each observer's callback, in the order they were made.
  callbacks: (() => void)[]
  // observed lists the elements the observers follow: observe adds one, unobserve takes it
  // off, and disconnecting any observer empties the list.
  observed: Element[]
  // resize calls every callback inside act, as a change of the layout would.
  resize: () => void
}

// followResizes stands in for ResizeObserver, keeping each callback for the test to call;
// nothing reports a size on its own.
export function followResizes(): Resizes {
  const callbacks: (() => void)[] = []
  const observed: Element[] = []

  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback: () => void) {
        callbacks.push(callback)
      }

      observe(el: Element) {
        observed.push(el)
      }

      unobserve(el: Element) {
        const at = observed.indexOf(el)

        if (at !== -1) {
          observed.splice(at, 1)
        }
      }

      disconnect() {
        observed.length = 0
      }
    },
  )

  return {
    callbacks,
    observed,
    resize: () => {
      act(() => {
        for (const callback of callbacks) {
          callback()
        }
      })
    },
  }
}
