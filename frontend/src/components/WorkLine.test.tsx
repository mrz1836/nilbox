import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotMatch } from '../platform'
import { at, renderAt } from '../test/clock'
import { agentMatch, agentRun, agents, autopilotMatch } from '../test/fixtures'
import { AutopilotRowsContext } from './autopilot/AutopilotChip'
import { WorkLine } from './WorkLine'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// showLine renders item 1's work line in a sheet, the rows' Autopilot holding match as its
// newest match, or none, and returns what the line's link opens a run with.
function showLine(match: AutopilotMatch | undefined) {
  const onShow = vi.fn()
  const Clock = at()

  render(
    <Clock>
      <AutopilotRowsContext
        value={{ names: new Map(), chips: new Map(match === undefined ? [] : [[1, match]]), onShow }}
      >
        <WorkLine worktree={undefined} sessions={[]} agents={agents()} itemId={1} />
      </AutopilotRowsContext>
    </Clock>,
  )

  return onShow
}

describe('WorkLine’s Autopilot line', () => {
  it('says what the newest background run made of the item, linking to its run', async () => {
    const pushed = agentRun({ status: 'ended', outcome: 'done', commit: '4f2a9c1000', verdict: 'fixed' })
    const match = agentMatch({ state: 'done', reason: 'ran', runs: [pushed] })
    const onShow = showLine(match)

    const line = screen.getByRole('button', { name: 'Fix failing checks pushed 4f2a9c1 · Fixed' })
    await userEvent.click(line)

    expect(onShow).toHaveBeenCalledExactlyOnceWith({
      routineId: match.routineId,
      matchId: match.id,
      runId: pushed.id,
      createdAt: match.createdAt,
      activity: false,
    })
  })

  it('says nothing of a match with no background run, nor without the rows’ Autopilot', () => {
    showLine(autopilotMatch())
    expect(screen.queryByRole('button')).not.toBeInTheDocument()

    renderAt(<WorkLine worktree={undefined} sessions={[]} agents={agents()} itemId={1} />)
    expect(screen.queryByText(/Fix failing checks/)).not.toBeInTheDocument()
  })
})
