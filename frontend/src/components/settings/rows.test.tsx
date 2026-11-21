import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Group, groupClass } from './rows'

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
