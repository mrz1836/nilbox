import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { nextTask } from '../test/async'
import { Fold } from './Fold'

// fold is the fold titled title.
function fold(title: string): HTMLDetailsElement {
  const details = screen.getByText(title).closest('details')

  if (details === null) {
    throw new Error(`no fold titled ${title}`)
  }

  return details
}

describe('Fold', () => {
  it('opens as its size says, then as the user chose, which it keeps', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <Fold title="Done" count={3} auto>
        <p>three</p>
      </Fold>,
    )

    expect(fold('Done').open).toBe(true)
    expect(screen.getByText('Done').closest('summary')).toHaveTextContent('Done3')

    rerender(
      <Fold title="Done" count={8} auto={false}>
        <p>eight</p>
      </Fold>,
    )
    await waitFor(() => {
      expect(fold('Done').open).toBe(false)
    })

    // Set back by its size, it opens again: that was no choice of the user's.
    rerender(
      <Fold title="Done" count={3} auto>
        <p>three</p>
      </Fold>,
    )
    await waitFor(() => {
      expect(fold('Done').open).toBe(true)
    })

    await user.click(screen.getByText('Done'))
    await waitFor(() => {
      expect(fold('Done').open).toBe(false)
    })

    // The user folded it: it stays folded whatever its size says.
    rerender(
      <Fold title="Done" count={2} auto>
        <p>two</p>
      </Fold>,
    )
    await nextTask()
    expect(fold('Done').open).toBe(false)
  })
})
