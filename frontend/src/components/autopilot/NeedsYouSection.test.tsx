import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotMatch } from '../../platform'
import { at } from '../../test/clock'
import { autopilotMatch, itemRef } from '../../test/fixtures'
import { NeedsYouSection } from './NeedsYouSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// waiting is a match that waits for the user, its routine's backlog when backlog is set.
function waiting(id: number, backlog = false): AutopilotMatch {
  return autopilotMatch({
    id,
    state: 'waiting',
    blockedBy: backlog ? 'backlog' : 'ask',
    reason: '',
    words: backlog ? '3 matched at once: run on them from Activity.' : 'It asks before it runs.',
    item: itemRef({ id, number: id }),
    steps: [],
    effects: [],
    closedAt: null,
    createdAt: '2026-06-15T11:50:00Z',
  })
}

// matches are a match that waits on its own, 2, and a backlog of three, 21 to 23.
const matches = [waiting(2), waiting(21, true), waiting(22, true), waiting(23, true)]

function renderSection(matchId: number) {
  render(
    <NeedsYouSection
      matches={matches}
      routine={0}
      error=""
      readiness={{ readiness: null, checking: false, error: '', check: () => undefined }}
      highlight={{ matchId, seq: 1 }}
      agentFor={() => 'Claude Code'}
      onOpen={() => undefined}
      onToast={() => undefined}
      onChanged={() => undefined}
    />,
    { wrapper: at() },
  )
}

describe('NeedsYouSection', () => {
  it('scrolls the card of the match a notification asked for into view', () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    renderSection(2)

    const [card] = scroll.mock.contexts

    expect(scroll).toHaveBeenCalledExactlyOnceWith({ block: 'center' })
    expect(card).toHaveAttribute('data-match', '2')
    expect(screen.getAllByRole('listitem').length).toBeGreaterThan(1)
  })

  it('scrolls a backlog’s card for any match it gathers', () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    renderSection(22)

    const [card] = scroll.mock.contexts

    expect(scroll).toHaveBeenCalledOnce()
    expect(card).toHaveAttribute('data-match', '21 22 23')
  })
})
