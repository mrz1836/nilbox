import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Profiler, useState } from 'react'

import { useCollapsedSections } from '../lib/useCollapsedSections'
import { useInbox } from '../lib/useInbox'
import { useInboxOrder } from '../lib/useInboxOrder'
import { ClockContext } from '../lib/useNow'
import { useTriage } from '../lib/useTriage'
import type { NilMoment } from '../platform'
import { deferred } from '../test/async'
import { clockAt, FIXED_NOW, fixedNow } from '../test/clock'
import { item, list, nilMoment, snapshot } from '../test/fixtures'
import { platform } from '../test/platform'
import { InboxView } from './InboxView'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// The inbox reads the party's code once per window, as once per test file here, so only a
// file whose first party is its test's shows what a session's first party does. This one
// holds that test apart from InboxView.test.tsx, whose earlier tests throw parties first.

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

describe('InboxView’s first party', () => {
  it('shows as soon as the milestones answer, once its code has loaded', async () => {
    const refreshed = '2026-06-15T11:55:00Z'
    const last = item({ id: 1, title: 'Add the widget cache' })
    const moment = deferred<NilMoment>()
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: refreshed, reviewRequested: list([last]) }))
    platform.dismissItem.mockResolvedValue(
      snapshot({
        lastRefreshedAt: refreshed,
        reviewRequested: list([{ ...last, dismissedAt: FIXED_NOW.toISOString() }]),
      }),
    )
    platform.reachedNil.mockReturnValue(moment.promise)
    // shown records, for each commit of the inbox, whether the party was on screen once it
    // committed.
    const shown: boolean[] = []
    render(
      <Profiler
        id="inbox"
        onRender={() => {
          shown.push(screen.queryByRole('region', { name: 'inbox == nil' }) !== null)
        }}
      >
        <Inbox />
      </Profiler>,
    )

    const review = within(await screen.findByRole('region', { name: 'Review requested' }))
    // The inbox loads the party's code once it has painted, long before the inbox is empty.
    await import('./InboxNil')

    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))
    await waitFor(() => {
      expect(platform.reachedNil).toHaveBeenCalledOnce()
    })

    // The milestones answer inside act, which lands that answer and every commit it sets off.
    const before = shown.length
    await act(async () => {
      moment.resolve(nilMoment({ earned: true, cleared: 1 }))
      await moment.promise
    })

    // The commit that answer brings shows the party. A party that shows its empty fallback
    // first commits without it, and only a commit later with it, which the window holds back
    // for about 300 ms and act lets through at once: so the test reads the order of the
    // commits, which no clock moves, rather than the time.
    expect(shown.slice(before)[0]).toBe(true)
    expect(screen.getByRole('region', { name: 'inbox == nil' })).toBeInTheDocument()
  })
})
