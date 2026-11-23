import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

// The stand-in src/test/setup.ts installs (./focusVisible) is what these look at.

// rung reports whether el reads as ringed by each selector method a test may ask.
function rung(el: HTMLElement) {
  const parent = el.parentElement as HTMLElement

  return {
    matches: el.matches(':focus-visible'),
    closest: el.closest(':focus-visible') === el,
    has: parent.matches(':has(:focus-visible)'),
    not: el.matches(':not(:focus-visible)'),
    element: parent.querySelector(':focus-visible') === el,
    document: document.querySelectorAll(':focus-visible').length === 1,
  }
}

const ringed = { matches: true, closest: true, has: true, not: false, element: true, document: true }
const unringed = { matches: false, closest: false, has: false, not: true, element: false, document: false }

function Controls() {
  return (
    <div>
      <button type="button">First</button>
      <button type="button">Second</button>
      <input aria-label="Name" />
      <input aria-label="Agree" type="checkbox" />
      <input aria-label="Locked" readOnly />
      <textarea aria-label="Notes" />
      <div aria-label="Draft" role="textbox" tabIndex={0} contentEditable suppressContentEditableWarning />
    </div>
  )
}

describe('the :focus-visible stand-in', () => {
  it('rings focus a script moves before any input, as a page’s first focus', () => {
    render(<Controls />)
    const first = screen.getByRole('button', { name: 'First' })

    first.focus()

    expect(rung(first)).toEqual(ringed)
  })

  it('leaves a click’s focus unringed, and rings it once a key is pressed', async () => {
    const user = userEvent.setup()
    render(<Controls />)
    const first = screen.getByRole('button', { name: 'First' })

    await user.click(first)
    expect(first).toHaveFocus()
    expect(rung(first)).toEqual(unringed)

    // A key held with ⌘ is a shortcut, not the keyboard taking over.
    await user.keyboard('{Meta>}c{/Meta}')
    expect(rung(first)).toEqual(unringed)

    await user.keyboard('{Shift}')
    expect(rung(first)).toEqual(ringed)
  })

  it('rings what Tab moves focus to, and focus a script moves after it', async () => {
    const user = userEvent.setup()
    render(<Controls />)
    const first = screen.getByRole('button', { name: 'First' })
    const second = screen.getByRole('button', { name: 'Second' })

    await user.click(first)
    await user.tab()
    expect(rung(second)).toEqual(ringed)

    first.focus()
    expect(rung(first)).toEqual(ringed)
  })

  it('leaves focus a script moves after a click unringed', async () => {
    const user = userEvent.setup()
    render(<Controls />)

    await user.click(screen.getByRole('button', { name: 'First' }))
    const second = screen.getByRole('button', { name: 'Second' })

    second.focus()
    expect(rung(second)).toEqual(unringed)
  })

  it('rings a click’s focus on what takes typed text', async () => {
    const user = userEvent.setup()
    render(<Controls />)

    for (const name of ['Name', 'Notes', 'Draft']) {
      const field = screen.getByRole('textbox', { name })

      await user.click(field)
      expect(field).toHaveFocus()
      expect(rung(field)).toEqual(ringed)
    }

    for (const field of [
      screen.getByRole('checkbox', { name: 'Agree' }),
      screen.getByRole('textbox', { name: 'Locked' }),
    ]) {
      await user.click(field)
      expect(field).toHaveFocus()
      expect(rung(field)).toEqual(unringed)
    }
  })

  it('reads :focus-visible only as a pseudo-class, never in a quoted value or an escaped class', async () => {
    const user = userEvent.setup()
    render(<Controls />)
    const first = screen.getByRole('button', { name: 'First' })

    first.className = 'has-[:focus-visible]:block'
    first.dataset.state = ':focus-visible'
    await user.click(first)

    expect(first.matches('[data-state=":focus-visible"]')).toBe(true)
    expect(first.matches("[data-state=':focus-visible']")).toBe(true)
    expect(first.matches('.has-\\[\\:focus-visible\\]\\:block')).toBe(true)
  })

  it('forgets the last test’s input', () => {
    render(<Controls />)
    const second = screen.getByRole('button', { name: 'Second' })

    second.focus()

    expect(rung(second)).toEqual(ringed)
  })
})
