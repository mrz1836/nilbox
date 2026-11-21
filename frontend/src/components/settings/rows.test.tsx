import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ErrorText as SharedErrorText } from '../controls'
import { ErrorText, Group, groupClass } from './rows'

describe('ErrorText', () => {
  it('is the shared control, for the sections that import it from here', () => {
    expect(ErrorText).toBe(SharedErrorText)
  })

  it('shows nothing while there is no error', () => {
    const { container } = render(<ErrorText id="save-error" error="" className="mt-1" />)

    expect(container).toBeEmptyDOMElement()
  })

  it('announces why a change was refused, under the id a control points at', () => {
    render(
      <>
        <input aria-label="Name" aria-describedby="save-error" />
        <ErrorText id="save-error" error="That name is taken." className="mt-1" />
      </>,
    )

    const alert = screen.getByRole('alert')
    expect(alert.tagName).toBe('P')
    expect(alert).toHaveTextContent('That name is taken.')
    expect(alert).toHaveAttribute('id', 'save-error')
    expect(alert).toHaveAttribute('class', 'mt-1 text-xs text-danger')
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveAccessibleDescription('That name is taken.')
  })

  it('needs no id or placement', () => {
    render(<ErrorText error="Couldn’t save." />)

    const alert = screen.getByRole('alert')
    expect(alert).not.toHaveAttribute('id')
    expect(alert).toHaveAttribute('class', 'text-xs text-danger')
  })
})

describe('Group', () => {
  it('is a card of rows', () => {
    render(
      <Group>
        <p>First</p>
        <p>Second</p>
      </Group>,
    )

    const card = screen.getByText('First').parentElement
    expect(card).toHaveAttribute('class', 'divide-y divide-border rounded-lg border border-border bg-surface')
    expect(card).toHaveAttribute('class', groupClass)
    expect(card).toContainElement(screen.getByText('Second'))
  })
})

describe('groupClass', () => {
  it('is the card a section draws itself', () => {
    expect(groupClass).toBe('divide-y divide-border rounded-lg border border-border bg-surface')
  })
})
