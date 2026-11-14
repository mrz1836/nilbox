import { render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { ConversationOrder, TimelinePage } from '../../platform'
import { at } from '../../test/clock'
import { issueComment, reviewTimeline } from '../../test/fixtures'
import { Timeline } from './Timeline'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

// conversation renders page in order, the visit starting from lastSeenAt.
function conversation(
  page: TimelinePage,
  order: ConversationOrder,
  onOrder?: (o: ConversationOrder) => void,
  lastSeenAt = '',
) {
  return render(
    <Timeline
      lead={<p>The description.</p>}
      page={page}
      repo="acme-org/example-repo"
      author="octo-other"
      lastSeenAt={lastSeenAt}
      onEarlier={vi.fn()}
      loadingEarlier={false}
      earlierError=""
      onQuote={vi.fn()}
      order={order}
      onOrder={onOrder}
    />,
  )
}

// entries names the conversation's rows, in the order shown, by the anchor of each.
function entries(): string[] {
  return within(screen.getByRole('region', { name: 'Conversation' }))
    .getAllByRole('listitem')
    .map((li) => li.id)
    .filter((id) => id !== '')
}

describe('Timeline order', () => {
  it('lists the oldest first, and offers the newest first', async () => {
    const onOrder = vi.fn()
    conversation(reviewTimeline({ total: 9, before: 'Y3Vyc29y' }), 'oldest', onOrder)

    expect(entries()).toEqual(['entry-IC_1', 'entry-PRR_1', 'entry-IC_2'])

    const choice = screen.getByRole('radiogroup', { name: 'Order of the conversation' })
    expect(within(choice).getByRole('radio', { name: 'Oldest first' })).toBeChecked()

    // The earlier entries are older, so they are read from the top.
    const earlier = screen.getByRole('button', { name: 'Show 6 earlier entries' })
    expect(earlier.compareDocumentPosition(screen.getByRole('list', { name: 'Entries' }))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )

    await userEvent.click(within(choice).getByRole('radio', { name: 'Newest first' }))
    expect(onOrder).toHaveBeenCalledWith('newest')
  })

  it('lists the newest first, under the lead, with the earlier entries read from the bottom', () => {
    conversation(reviewTimeline({ total: 9, before: 'Y3Vyc29y' }), 'newest', vi.fn())

    expect(entries()).toEqual(['entry-IC_2', 'entry-PRR_1', 'entry-IC_1'])
    expect(
      screen.getByText('The description.').compareDocumentPosition(screen.getByRole('list', { name: 'Entries' })),
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(
      screen
        .getByRole('list', { name: 'Entries' })
        .compareDocumentPosition(screen.getByRole('button', { name: /earlier/ })),
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(screen.getByRole('radio', { name: 'Newest first' })).toBeChecked()
  })

  it('marks where the new entries end, in either order', () => {
    const lastSeenAt = '2026-06-15T10:30:00Z'
    const { unmount } = conversation(reviewTimeline(), 'oldest', vi.fn(), lastSeenAt)

    const before = screen.getByRole('separator', { name: 'New since your last visit' })
    expect(before).toHaveTextContent(/^New$/)
    expect(before.nextElementSibling).toHaveAttribute('id', 'entry-PRR_1')
    unmount()

    conversation(reviewTimeline(), 'newest', vi.fn(), lastSeenAt)

    const after = screen.getByRole('separator', { name: 'New since your last visit' })
    expect(after, 'its arrow points to the new, above it').toHaveTextContent('New ↑')
    expect(after.previousElementSibling).toHaveAttribute('id', 'entry-PRR_1')
    expect(after.nextElementSibling).toHaveAttribute('id', 'entry-IC_1')
  })

  it('offers no order without a choice to hear, nor for a single entry', () => {
    const { unmount } = conversation(reviewTimeline(), 'oldest')
    expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument()
    unmount()

    conversation({ items: [issueComment()], total: 1, before: '' }, 'oldest', vi.fn())
    expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument()
  })
})
