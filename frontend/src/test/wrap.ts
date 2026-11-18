import { expect } from 'vitest'

// noWrap are the classes that keep text on one line, or cut it short there.
const noWrap = ['whitespace-nowrap', 'whitespace-pre', 'text-nowrap', 'truncate', 'text-ellipsis']

// expectWraps fails when the element, or anything between it and the panel, keeps its text
// on one line.
export function expectWraps(element: HTMLElement, panel: HTMLElement) {
  for (let node: HTMLElement | null = element; node !== null && node !== panel; node = node.parentElement) {
    for (const name of noWrap) {
      expect(node).not.toHaveClass(name)
    }
  }
}
