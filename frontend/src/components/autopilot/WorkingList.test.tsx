import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { chipOf } from '../../lib/autopilot'
import type { AutopilotWorking } from '../../platform'
import { at } from '../../test/clock'
import { autopilotMatch, itemRef } from '../../test/fixtures'
import { platform, send } from '../../test/platform'
import { tokensText, WorkingList } from './WorkingList'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// working is a Read only run on the fixture's pull request #7, a few minutes in.
function working(overrides: Partial<AutopilotWorking> = {}): AutopilotWorking {
  return {
    runId: 41,
    matchId: 12,
    routineId: 3,
    routine: 'Review bot PRs',
    item: itemRef(),
    agent: 'claude',
    where: 'background',
    startedAt: '2026-06-15T11:56:00Z',
    createdAt: '2026-06-15T11:56:00Z',
    status: 'running',
    progress: {
      step: 'Reading main.go',
      todos: [],
      tokensIn: 9000,
      tokensOut: 3300,
      cacheRead: 0,
      events: 12,
      unknown: 0,
    },
    ...overrides,
  }
}

function renderList() {
  const onOpen = vi.fn()
  const onToast = vi.fn()

  const onWatch = vi.fn()

  render(<WorkingList onOpen={onOpen} onWatch={onWatch} watching={0} onToast={onToast} />, { wrapper: at() })

  return { onOpen, onToast, onWatch }
}

describe('WorkingList', () => {
  it('shows nothing while no agent works', async () => {
    const { container } = render(<WorkingList onOpen={vi.fn()} onWatch={vi.fn()} watching={0} onToast={vi.fn()} />, {
      wrapper: at(),
    })

    await waitFor(() => {
      expect(platform.autopilotWorking).toHaveBeenCalled()
    })
    expect(container).toBeEmptyDOMElement()
  })

  it('lists each agent working with its step, time, and tokens, and stops one', async () => {
    platform.autopilotWorking.mockResolvedValue({ working: [working()] })
    const user = userEvent.setup()

    renderList()

    const list = await screen.findByRole('region', { name: 'Working' })
    expect(within(list).getByText('Working · 1')).toBeInTheDocument()
    expect(within(list).getByText('Reading main.go')).toBeInTheDocument()
    expect(within(list).getByText(/Review bot PRs · 4 min · 12\.3k tokens/)).toBeInTheDocument()

    await user.click(within(list).getByRole('button', { name: 'Stop' }))

    expect(platform.stopAutopilotRun).toHaveBeenCalledExactlyOnceWith(41)
  })

  it('stops every agent with Stop all', async () => {
    platform.autopilotWorking.mockResolvedValue({ working: [working(), working({ runId: 42 })] })
    const user = userEvent.setup()

    const { onToast } = renderList()

    await user.click(await screen.findByRole('button', { name: 'Stop all' }))

    expect(platform.stopAllAutopilotRuns).toHaveBeenCalledOnce()
    expect(onToast).toHaveBeenCalledWith('Stopping every agent’s run.')
  })

  it('says why a stop didn’t go through', async () => {
    platform.autopilotWorking.mockResolvedValue({ working: [working()] })
    platform.stopAutopilotRun.mockRejectedValue(new Error('That run isn’t going.'))
    platform.stopAllAutopilotRuns.mockRejectedValue(new Error('The runs couldn’t be stopped.'))
    const user = userEvent.setup()

    const { onToast } = renderList()

    await user.click(await screen.findByRole('button', { name: 'Stop' }))
    expect(onToast).toHaveBeenLastCalledWith('That run isn’t going.', 'error')

    await user.click(screen.getByRole('button', { name: 'Stop all' }))
    expect(onToast).toHaveBeenLastCalledWith('The runs couldn’t be stopped.', 'error')
  })

  it('opens a run’s item', async () => {
    platform.autopilotWorking.mockResolvedValue({ working: [working({ item: itemRef({ number: 8 }) })] })
    const user = userEvent.setup()

    const { onOpen } = renderList()

    await user.click(await screen.findByRole('button', { name: /#8/ }))

    expect(onOpen).toHaveBeenCalledExactlyOnceWith(itemRef({ number: 8 }), 3)
  })

  it('reads the runs again as a run’s stream moves on', async () => {
    renderList()
    await waitFor(() => {
      expect(platform.autopilotWorking).toHaveBeenCalledOnce()
    })

    send('autopilot:run', { runId: 41 })

    await waitFor(() => {
      expect(platform.autopilotWorking).toHaveBeenCalledTimes(2)
    })
  })
})

describe('tokensText', () => {
  it('says a run’s tokens so far', () => {
    expect(tokensText(1)).toBe('1 token')
    expect(tokensText(640)).toBe('640 tokens')
    expect(tokensText(12_345)).toBe('12.3k tokens')
    expect(tokensText(250_000)).toBe('250k tokens')
  })
})

describe('chipOf', () => {
  it('says a Read only agent’s notes are ready', () => {
    const run = {
      id: 41,
      matchId: 12,
      step: 0,
      runner: 'background',
      agent: 'claude',
      action: '',
      status: 'ended',
      outcome: 'done',
      verdict: '' as const,
      headBefore: '',
      commit: '',
      headAfter: '',
      budget: 0.5,
      unit: 'usd',
      cost: 0.12,
      costEstimated: true,
      tokensIn: 9000,
      tokensOut: 3300,
      cacheRead: 0,
      turns: 3,
      summary: 'Finished: looks good.',
      error: '',
      manual: false,
      scriptRunId: 7,
      sessionId: 's',
      preset: 'readOnly',
      profile: 'default',
      command: '',
      logCleared: false,
      model: '',
      effort: '',
      worktree: 'pr-1',
      queuedAt: '2026-06-15T11:56:00Z',
      startedAt: '2026-06-15T11:56:00Z',
      finishedAt: '2026-06-15T12:00:00Z',
    }

    expect(chipOf(autopilotMatch({ effects: [], steps: [], runs: [run] }))).toEqual({
      label: 'Notes ready',
      tone: 'accent',
    })
    expect(chipOf(autopilotMatch({ state: 'running', effects: [] }))).toEqual({ label: 'Working', tone: 'accent' })
    expect(chipOf(autopilotMatch({ state: 'queued', effects: [] }))).toEqual({ label: 'Queued', tone: 'muted' })
  })
})
