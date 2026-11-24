import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { Status } from '../lib/statusOrder'
import { ListSortControl, type ListSortControlProps } from './ListSortControl'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const statuses: Status[] = [
  { name: 'In progress', color: '#bf8700' },
  { name: 'Ready', color: '#218bff' },
  { name: 'Deferred', color: '' },
]

// Stored keeps the order each change stores, as the inbox does.
function Stored(props: Partial<ListSortControlProps> & { onStored?: (order: readonly string[]) => void }) {
  const { onStored, ...rest } = props
  const [order, setOrder] = useState<readonly string[]>(['In progress', 'Gone', 'Ready', 'Deferred'])

  return (
    <ListSortControl
      title="Assigned"
      tab="inbox"
      sort="status"
      onSortChange={vi.fn()}
      statuses={statuses}
      order={order}
      customized
      onOrderChange={(next) => {
        onStored?.(next)
        setOrder(next)
      }}
      {...rest}
    />
  )
}

// rows lists the statuses the order shows, top first.
function rows() {
  return within(screen.getByRole('list', { name: 'Statuses, top ranked first' }))
    .getAllByRole('listitem')
    .map((row) => row.textContent)
}

// sorts names the sorts the control offers, in its order.
function sorts() {
  return within(screen.getByRole('radiogroup', { name: 'Sort Assigned by' }))
    .getAllByRole('radio')
    .map((radio) => radio.closest('label')?.textContent)
}

describe('ListSortControl', () => {
  it('switches between newest activity, repository, author, and project status', async () => {
    const onSortChange = vi.fn()
    const { rerender } = render(<Stored sort="recent" onSortChange={onSortChange} />)

    const sort = screen.getByRole('radiogroup', { name: 'Sort Assigned by' })
    expect(within(sort).getByRole('radio', { name: 'Recent' })).toBeChecked()
    expect(screen.queryByRole('button', { name: 'Status order' })).not.toBeInTheDocument()

    await userEvent.click(within(sort).getByRole('radio', { name: 'Repo' }))
    expect(onSortChange).toHaveBeenLastCalledWith('repo')

    rerender(<Stored sort="repo" onSortChange={onSortChange} />)
    expect(within(sort).getByRole('radio', { name: 'Repo' })).toBeChecked()
    expect(
      screen.queryByRole('button', { name: 'Status order' }),
      'the order is only for status',
    ).not.toBeInTheDocument()

    await userEvent.click(within(sort).getByRole('radio', { name: 'Author' }))
    expect(onSortChange).toHaveBeenLastCalledWith('author')

    await userEvent.click(within(sort).getByRole('radio', { name: 'Status' }))
    expect(onSortChange).toHaveBeenLastCalledWith('status')

    rerender(<Stored sort="status" onSortChange={onSortChange} />)
    expect(within(sort).getByRole('radio', { name: 'Status' })).toBeChecked()
    expect(screen.getByRole('button', { name: 'Status order' })).toBeInTheDocument()
  })

  it('words Recent and the name sorts for the tab’s own order', () => {
    const { rerender } = render(<Stored sort="recent" />)
    expect(screen.getByRole('radio', { name: 'Recent' }).closest('label')).toHaveAttribute(
      'title',
      'Newest activity first',
    )
    expect(screen.getByRole('radio', { name: 'Repo' }).closest('label')).toHaveAttribute(
      'title',
      'By repository, A–Z, newest activity first in each',
    )

    rerender(<Stored sort="recent" tab="snoozed" />)
    expect(screen.getByRole('radio', { name: 'Recent' }).closest('label')).toHaveAttribute(
      'title',
      'Soonest back first',
    )
    expect(screen.getByRole('radio', { name: 'Author' }).closest('label')).toHaveAttribute(
      'title',
      'By author, A–Z, soonest back first in each',
    )

    rerender(<Stored sort="recent" tab="dismissed" />)
    expect(screen.getByRole('radio', { name: 'Recent' }).closest('label')).toHaveAttribute(
      'title',
      'Most recently dismissed first',
    )
  })

  it('offers Status only while the tab has statuses, or the list is sorted by status', () => {
    const { rerender } = render(<Stored sort="recent" statuses={[]} />)
    expect(sorts()).toEqual(['Recent', 'Repo', 'Author'])

    rerender(<Stored sort="recent" />)
    expect(sorts()).toEqual(['Recent', 'Repo', 'Author', 'Status'])

    rerender(<Stored sort="status" statuses={[]} />)
    expect(sorts(), 'so it can be turned back').toEqual(['Recent', 'Repo', 'Author', 'Status'])
    expect(screen.getByRole('radio', { name: 'Status' })).toBeChecked()
  })

  it('shows the statuses found, in the order, with their colors', async () => {
    render(<Stored />)

    await userEvent.click(screen.getByRole('button', { name: 'Status order' }))

    expect(screen.getByRole('dialog', { name: 'Status order' })).toBeInTheDocument()
    expect(rows()).toEqual(['1In progress', '2Ready', '3Deferred'])
    expect(screen.getByRole('button', { name: 'Move In progress up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move Deferred down' })).toBeDisabled()
  })

  it('moves a status, keeping the ones not shown in place, and announces it', async () => {
    const onStored = vi.fn()
    render(<Stored onStored={onStored} />)
    await userEvent.click(screen.getByRole('button', { name: 'Status order' }))

    await userEvent.click(screen.getByRole('button', { name: 'Move Ready up' }))

    expect(onStored).toHaveBeenLastCalledWith(['Ready', 'Gone', 'In progress', 'Deferred'])
    expect(rows()).toEqual(['1Ready', '2In progress', '3Deferred'])
    expect(screen.getByRole('status')).toHaveTextContent('Ready moved to 1 of 3')
    // Ready reached the top, so focus moves onto its other arrow.
    expect(screen.getByRole('button', { name: 'Move Ready down' })).toHaveFocus()

    await userEvent.click(screen.getByRole('button', { name: 'Move In progress down' }))
    expect(rows()).toEqual(['1Ready', '2Deferred', '3In progress'])
    expect(screen.getByRole('button', { name: 'Move In progress up' })).toHaveFocus()
  })

  it('resets the order, once the user has changed it', async () => {
    const onStored = vi.fn()
    const { unmount } = render(<Stored onStored={onStored} />)
    await userEvent.click(screen.getByRole('button', { name: 'Status order' }))

    await userEvent.click(screen.getByRole('button', { name: 'Reset' }))
    expect(onStored).toHaveBeenLastCalledWith([])

    unmount()
    render(<Stored customized={false} />)
    await userEvent.click(screen.getByRole('button', { name: 'Status order' }))
    expect(screen.getByRole('button', { name: 'Reset' })).toBeDisabled()
  })
})
