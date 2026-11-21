import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { useState } from 'react'

import { useCollapsedSections } from '../lib/useCollapsedSections'
import { useInbox } from '../lib/useInbox'
import { useInboxOrder } from '../lib/useInboxOrder'
import { ClockContext } from '../lib/useNow'
import { useTriage } from '../lib/useTriage'
import { nextTask } from '../test/async'
import { clockAt, FIXED_NOW, fixedNow } from '../test/clock'
import { item, list, snapshot } from '../test/fixtures'
import { platform } from '../test/platform'
import { InboxView } from './InboxView'

// code holds the party's code back until the test lets it load: its module loads once per
// test file, so only a file of its own sees a party come before its code.
const code = vi.hoisted(() => {
  let release: () => void = () => undefined
  const loaded = new Promise<void>((resolve) => {
    release = resolve
  })

  return { loaded, release }
})

vi.mock('./InboxNil', async (importOriginal) => {
  await code.loaded

  return importOriginal()
})

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// Inbox renders the view over the real inbox state, triage, and collapsed lists, the way
// App does.
function Inbox() {
  const [clock] = useState(() => clockAt(fixedNow))
  const inbox = useInbox()
  const triaged = useTriage(inbox.snapshot)
  const sections = useCollapsedSections()
  const inboxOrder = useInboxOrder()

  return (
    <ClockContext value={clock}>
      <InboxView
        inbox={inbox}
        triaged={triaged}
        launch={null}
        resolutions={new Map()}
        snoozeDays={3}
        onSnoozeSettings={() => undefined}
        collapsed={sections.collapsed}
        onCollapse={sections.setCollapsed}
        inboxOrder={inboxOrder}
      />
    </ClockContext>
  )
}

describe('InboxView’s party before its code has loaded', () => {
  it('shows nothing in its place until the code is there, then shows', async () => {
    const refreshed = '2026-06-15T11:55:00Z'
    const last = item({ id: 1, title: 'Add the widget cache' })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: refreshed, reviewRequested: list([last]) }))
    platform.dismissItem.mockResolvedValue(
      snapshot({
        lastRefreshedAt: refreshed,
        reviewRequested: list([{ ...last, dismissedAt: FIXED_NOW.toISOString() }]),
      }),
    )
    render(<Inbox />)

    const review = within(await screen.findByRole('region', { name: 'Review requested' }))
    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))
    await waitFor(() => {
      expect(platform.reachedNil).toHaveBeenCalledOnce()
    })
    await nextTask()

    // The milestones have answered, but the party's code has not come.
    expect(screen.queryByRole('region', { name: 'inbox == nil' })).not.toBeInTheDocument()
    expect(review.getByRole('tab', { name: 'Dismissed 1' })).toBeInTheDocument()

    code.release()

    expect(await screen.findByRole('region', { name: 'inbox == nil' })).toBeInTheDocument()
  })
})
