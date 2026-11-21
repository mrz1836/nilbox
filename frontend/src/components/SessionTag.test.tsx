import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { AgentSession } from '../platform'
import { at, renderAt } from '../test/clock'
import { agentSession, agents, launchHold, launchResult, runningHold } from '../test/fixtures'
import { platform } from '../test/platform'
import { AutopilotRowsContext } from './autopilot/AutopilotChip'
import { SessionTag } from './SessionTag'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// entries returns the panel's sessions, as many as want.
function entries(want: number): HTMLElement[] {
  const found = within(screen.getByRole('dialog', { name: 'Agent sessions' })).getAllByRole('listitem')
  expect(found).toHaveLength(want)

  return found
}

function renderTag(sessions: readonly AgentSession[]) {
  return renderAt(<SessionTag sessions={sessions} agents={agents()} />)
}

describe('SessionTag', () => {
  it('follows the running session: its agent, what it is doing, and for how long', () => {
    renderTag([agentSession()])

    const tag = screen.getByRole('button', { name: /Claude session: needs approval/ })
    expect(tag).toHaveTextContent('Claude · needs approval · 1m')
    // It runs in the item's worktree, which the tag says wherever it was launched from.
    expect(tag).toHaveAccessibleName("Claude session: needs approval, in its worktree. Show the item's sessions")
    expect(tag).toHaveAttribute(
      'title',
      `Claude needs approval: Permission prompt\nIn its worktree: ~/projects/.${brand.slug}/worktrees/example-repo/pr-7`,
    )
  })

  it('carries no worktree mark for a session in the clone itself', () => {
    renderTag([agentSession({ worktree: '', dir: '~/projects/example-repo' })])

    const tag = screen.getByRole('button', { name: /Claude session: needs approval/ })
    expect(tag).toHaveAccessibleName("Claude session: needs approval. Show the item's sessions")
    expect(tag).toHaveAttribute('title', 'Claude needs approval: Permission prompt')
  })

  it('leaves the worktree mark to the card when the card already shows its worktree', () => {
    const { container, rerender } = renderTag([agentSession()])
    const marks = () => container.querySelectorAll('button svg')

    expect(marks()).toHaveLength(1)

    rerender(<SessionTag sessions={[agentSession()]} agents={agents()} markWorktree={false} />)
    expect(marks()).toHaveLength(0)
  })

  it('swells a ring out of the dot while the session works, pulses it while it starts, and holds it still otherwise', () => {
    const { container, rerender } = renderTag([agentSession({ state: 'working', detail: '' })])
    const ring = () => container.querySelector('.motion-safe\\:animate-ping')
    const pulse = () => container.querySelector('.motion-safe\\:animate-pulse')

    expect(ring()).not.toBeNull()
    expect(pulse()).toBeNull()

    rerender(<SessionTag sessions={[agentSession({ state: 'starting', detail: '' })]} agents={agents()} />)
    expect(ring()).toBeNull()
    expect(pulse()).not.toBeNull()

    rerender(<SessionTag sessions={[agentSession({ state: 'idle', detail: '' })]} agents={agents()} />)
    expect(ring()).toBeNull()
    expect(pulse()).toBeNull()
  })

  it('opens its panel under its left edge, or under its right edge near the window’s right side', async () => {
    const user = userEvent.setup()
    const { rerender } = renderTag([agentSession()])

    await user.click(screen.getByRole('button', { name: /Claude session/ }))
    expect(screen.getByRole('dialog', { name: 'Agent sessions' })).toHaveClass('left-0')

    rerender(<SessionTag sessions={[agentSession()]} agents={agents()} align="right" />)
    expect(screen.getByRole('dialog', { name: 'Agent sessions' })).toHaveClass('right-0')
  })

  it('shows nothing for an item whose sessions ended over a day ago', () => {
    const { container } = renderTag([agentSession({ state: 'ended', endedAt: '2026-06-13T09:00:00Z' })])

    expect(container).toBeEmptyDOMElement()
  })

  it('opens the item’s sessions, newest first, with what each last said', async () => {
    const user = userEvent.setup()
    const ended = agentSession({
      id: 'older',
      agent: 'codex',
      profile: 'work',
      prompt: '',
      state: 'ended',
      endedAt: '2026-06-15T10:00:00Z',
      lastMessage: 'All tests pass.',
      canFocus: false,
      canResume: true,
    })

    renderTag([agentSession(), ended])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [newest = document.body, older = document.body] = entries(2)

    expect(newest).toHaveTextContent('Claude')
    expect(newest).toHaveTextContent('default profile · Review prompt')
    expect(newest).toHaveTextContent('I need to run the test suite. May I?')
    expect(within(newest).getByRole('button', { name: 'Show in terminal' })).toBeEnabled()
    expect(within(newest).queryByRole('button', { name: 'Resume' })).not.toBeInTheDocument()

    expect(older).toHaveTextContent('Codex')
    expect(older).toHaveTextContent('work profile · resumed')
    expect(older).toHaveTextContent('ended')
    expect(within(older).queryByRole('button', { name: 'Show in terminal' })).not.toBeInTheDocument()
    expect(within(older).getByRole('button', { name: 'Resume' })).toBeEnabled()
  })

  it('says what came of the item on GitHub while each session ran', async () => {
    const user = userEvent.setup()
    const sessions = [
      agentSession({
        id: 'approved',
        githubEvent: 'approved',
        githubEventCommit: 'abc1234def5678abc1234def5678abc1234def56',
        githubEventAt: '2026-06-15T11:58:00Z',
      }),
      agentSession({ id: 'quiet' }),
    ]

    renderTag(sessions)
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [approved = document.body, quiet = document.body] = entries(2)

    expect(approved).toHaveTextContent('On GitHub: Approved at abc1234 · 2m ago')
    expect(quiet).not.toHaveTextContent('On GitHub')
  })

  it('says what each session started with, where, and what its launch worked around', async () => {
    const user = userEvent.setup()
    const notice = 'Couldn’t fetch origin/main, so the worktree starts from the last fetched copy: timed out'
    const sessions = [
      agentSession({ id: 'made', notice }),
      agentSession({ id: 'reused', worktreeAction: 'resume', terminal: 'terminal' }),
      agentSession({ id: 'clone', worktree: '', worktreeAction: '', permission: 'plan mode (outside contributor)' }),
      agentSession({ id: 'before', worktreeAction: '', permission: '', terminal: 'example-terminal' }),
    ]

    renderTag(sessions)
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [made = document.body, reused = document.body, clone = document.body, before = document.body] = entries(4)

    expect(made).toHaveTextContent('full access · new worktree')
    expect(made).toHaveTextContent(notice)
    expect(within(made).getByRole('button', { name: 'Show in terminal' })).toHaveAttribute(
      'title',
      'Bring forward the iTerm2 tab it runs in',
    )
    expect(reused).toHaveTextContent('full access · worktree reused')
    expect(reused).not.toHaveTextContent(notice)
    expect(within(reused).getByRole('button', { name: 'Show in terminal' })).toHaveAttribute(
      'title',
      'Bring forward the Terminal tab it runs in',
    )
    expect(clone).toHaveTextContent('plan mode (outside contributor) · in the clone')
    // A session recorded before its launch's facts were kept says only where it runs.
    expect(before).toHaveTextContent('in its worktree')
    expect(before).not.toHaveTextContent('full access')
    expect(within(before).getByRole('button', { name: 'Show in terminal' })).toHaveAttribute(
      'title',
      'Bring forward the terminal tab it runs in',
    )
  })

  it('says how many tokens each session used, where the agent’s own files say, read again as it changes', async () => {
    const user = userEvent.setup()
    const counts = { input: 18, output: 245, cacheRead: 840_000, cacheWrite: 1500 }
    const read = { known: true, fromRun: false, ...counts, total: 841_763, partial: false }

    platform.sessionUsage.mockImplementation((id) =>
      Promise.resolve(
        id === 'long'
          ? { ...read, total: 1_234_567, partial: true }
          : id === 'none'
            ? {
                known: false,
                fromRun: false,
                input: 0,
                output: 0,
                cacheRead: 0,
                cacheWrite: 0,
                total: 0,
                partial: false,
              }
            : read,
      ),
    )

    const sessions = [
      agentSession({ id: 'read' }),
      agentSession({ id: 'long' }),
      agentSession({ id: 'none' }),
      agentSession({ id: 'unnamed', agentSessionId: '' }),
    ]
    const { rerender } = renderTag(sessions)
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [counted = document.body, partial = document.body, unknown = document.body, unnamed = document.body] =
      entries(4)

    const line = await within(counted).findByText('841.8K tokens · 840K cached')
    expect(line).toHaveAttribute(
      'title',
      '841,763 tokens: 18 in, 245 out, 840,000 read from the cache, 1,500 written to it, as the agent’s own files on this Mac report them.',
    )
    expect(await within(partial).findByText('at least 1.2M tokens · 840K cached')).toHaveAttribute(
      'title',
      expect.stringContaining('too long to read whole'),
    )
    expect(unknown).not.toHaveTextContent('tokens')
    expect(unnamed).not.toHaveTextContent('tokens')
    // A session the agent hasn't named yet has no files to read.
    expect(platform.sessionUsage).not.toHaveBeenCalledWith('unnamed')
    expect(platform.sessionUsage).toHaveBeenCalledTimes(3)

    rerender(
      <SessionTag
        sessions={[
          agentSession({ id: 'read', state: 'working', stateAt: '2026-06-15T12:00:00Z' }),
          ...sessions.slice(1),
        ]}
        agents={agents()}
      />,
    )

    await vi.waitFor(() => {
      expect(platform.sessionUsage).toHaveBeenCalledTimes(4)
    })
    expect(platform.sessionUsage).toHaveBeenLastCalledWith('read')
  })

  it('counts a background run’s tokens as its run reported them, before the agent names it', async () => {
    const user = userEvent.setup()
    platform.sessionUsage.mockResolvedValue({
      known: true,
      fromRun: true,
      input: 1200,
      output: 30,
      cacheRead: 9000,
      cacheWrite: 0,
      total: 10_230,
      partial: false,
    })

    renderTag([agentSession({ id: 'run', agentSessionId: '', runId: 4 })])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [run = document.body] = entries(1)
    expect(await within(run).findByText('10.2K tokens · 9K cached')).toHaveAttribute(
      'title',
      expect.stringContaining('as its run reported them'),
    )
    expect(platform.sessionUsage).toHaveBeenCalledWith('run')
  })

  it('shows a session’s folder in Finder, and says why it couldn’t', async () => {
    const user = userEvent.setup()
    const ended = agentSession({ id: 'older', state: 'ended', endedAt: '2026-06-15T10:00:00Z', canFocus: false })

    renderTag([agentSession(), ended])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [running = document.body, done = document.body] = entries(2)

    const show = within(running).getByRole('button', { name: 'Show in Finder' })
    expect(show).toHaveAttribute('title', `Show ~/projects/.${brand.slug}/worktrees/example-repo/pr-7 in Finder`)
    await user.click(show)
    expect(platform.showSessionFolder).toHaveBeenCalledWith('0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70')
    expect(within(running).queryByRole('alert')).not.toBeInTheDocument()

    platform.showSessionFolder.mockRejectedValueOnce(
      new Error(`~/projects/.${brand.slug}/worktrees/example-repo/pr-7 isn't there anymore.`),
    )
    await user.click(within(done).getByRole('button', { name: 'Show in Finder' }))

    expect(platform.showSessionFolder).toHaveBeenLastCalledWith('older')
    expect(await within(done).findByRole('alert')).toHaveTextContent("isn't there anymore.")
  })

  it('removes a session once it ended, and says why it couldn’t', async () => {
    const user = userEvent.setup()
    const ended = agentSession({ id: 'older', state: 'ended', endedAt: '2026-06-15T10:00:00Z', canFocus: false })
    const failed = agentSession({ id: 'failed', state: 'failed', detail: 'API error' })

    renderTag([agentSession(), failed, ended])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [running = document.body, stuck = document.body, done = document.body] = entries(3)

    // Only a session that ended can be removed; a failed one still counts as running.
    expect(within(running).queryByRole('button', { name: 'Remove this session' })).not.toBeInTheDocument()
    expect(within(stuck).queryByRole('button', { name: 'Remove this session' })).not.toBeInTheDocument()

    const remove = within(done).getByRole('button', { name: 'Remove this session' })
    expect(remove).toHaveAttribute('title', 'Remove this ended session from the list')

    platform.forgetSession.mockRejectedValueOnce(
      new Error('This session is still running. Remove it once it has ended.'),
    )
    await user.click(remove)

    expect(platform.forgetSession).toHaveBeenCalledWith('older')
    expect(await within(done).findByRole('alert')).toHaveTextContent('This session is still running.')

    await user.click(remove)
    expect(platform.forgetSession).toHaveBeenCalledTimes(2)
    expect(within(done).queryByRole('alert')).not.toBeInTheDocument()
    // The panel stays open for the other sessions; the list reads again once the app says
    // the sessions changed.
    entries(3)
  })

  it('shows the terminal, resumes, and copies the session ID', async () => {
    const user = userEvent.setup()
    const ended = agentSession({ id: 'older', state: 'ended', endedAt: '2026-06-15T10:00:00Z', canResume: true })

    renderTag([agentSession(), ended])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [running = document.body, done = document.body] = entries(2)

    await user.click(within(running).getByRole('button', { name: 'Show in terminal' }))
    expect(platform.focusSession).toHaveBeenCalledWith('0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70')

    await user.click(within(done).getByRole('button', { name: 'Resume' }))
    expect(platform.resumeSession).toHaveBeenCalledWith({
      sessionId: 'older',
      access: '',
      alongside: '',
      confirmed: '',
    })

    await user.click(within(running).getByRole('button', { name: 'Copy session ID' }))
    expect(platform.copyText).toHaveBeenCalledWith('0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70')
    expect(within(running).getByRole('button', { name: 'Copied' })).toBeInTheDocument()
  })

  it('says why its terminal could not be shown', async () => {
    const user = userEvent.setup()
    platform.focusSession.mockRejectedValue(new Error('Couldn’t find the session’s tab.'))

    renderTag([agentSession()])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))
    await user.click(screen.getByRole('button', { name: 'Show in terminal' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t find the session’s tab.')
  })

  it('says what a resume worked around', async () => {
    const user = userEvent.setup()
    platform.resumeSession.mockResolvedValue(
      launchResult({ worktreeAction: 'resume', notice: 'started from the last fetched main' }),
    )

    renderTag([agentSession({ state: 'ended', endedAt: '2026-06-15T10:00:00Z', canFocus: false, canResume: true })])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))
    await user.click(screen.getByRole('button', { name: 'Resume' }))

    expect(await screen.findByRole('status')).toHaveTextContent('started from the last fetched main')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Copy session ID' }))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('clears what went wrong once a later action works', async () => {
    const user = userEvent.setup()
    platform.focusSession.mockRejectedValueOnce(new Error('Couldn’t find the session’s tab.'))
    platform.copyText.mockRejectedValueOnce(new Error('The clipboard refused it.'))

    renderTag([agentSession()])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))
    await user.click(screen.getByRole('button', { name: 'Show in terminal' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t find the session’s tab.')

    await user.click(screen.getByRole('button', { name: 'Copy session ID' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The clipboard refused it.')

    await user.click(screen.getByRole('button', { name: 'Show in terminal' }))
    expect(platform.focusSession).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Copy session ID' }))
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('asks before resuming where a session runs, and confirms each hold apart', async () => {
    const user = userEvent.setup()
    const running = runningHold()
    const files = launchHold()
    platform.resumeSession
      .mockResolvedValueOnce(launchResult({ hold: running }))
      .mockResolvedValueOnce(launchResult({ hold: files }))
      .mockResolvedValueOnce(launchResult({ worktreeAction: 'resume' }))

    renderTag([
      agentSession({ id: 'older', state: 'ended', endedAt: '2026-06-15T10:00:00Z', canFocus: false, canResume: true }),
    ])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))
    await user.click(screen.getByRole('button', { name: 'Resume' }))

    await user.click(await screen.findByRole('button', { name: 'Start another' }))
    await user.click(await screen.findByRole('button', { name: 'Open anyway' }))

    const request = { sessionId: 'older', access: '', alongside: '', confirmed: '' }
    expect(platform.resumeSession).toHaveBeenNthCalledWith(2, { ...request, alongside: running.key })
    expect(platform.resumeSession).toHaveBeenNthCalledWith(3, {
      ...request,
      alongside: running.key,
      confirmed: files.key,
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('goes to the running session instead of resuming beside it', async () => {
    const user = userEvent.setup()
    const running = runningHold()
    platform.resumeSession.mockResolvedValueOnce(launchResult({ hold: running }))

    renderTag([
      agentSession({ id: 'older', state: 'ended', endedAt: '2026-06-15T10:00:00Z', canFocus: false, canResume: true }),
    ])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))
    await user.click(screen.getByRole('button', { name: 'Resume' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'A session is already running here' }))
    await user.click(dialog.getByRole('button', { name: 'Show in terminal' }))

    expect(platform.focusSession).toHaveBeenCalledWith(running.running[0]?.sessionId)
    expect(platform.resumeSession).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog', { name: 'A session is already running here' })).not.toBeInTheDocument()
  })

  it('cancels a held resume, and says why one failed', async () => {
    const user = userEvent.setup()
    platform.resumeSession
      .mockResolvedValueOnce(launchResult({ hold: runningHold() }))
      .mockRejectedValueOnce(new Error('The session’s folder is gone.'))

    renderTag([
      agentSession({ id: 'older', state: 'ended', endedAt: '2026-06-15T10:00:00Z', canFocus: false, canResume: true }),
    ])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))
    await user.click(screen.getByRole('button', { name: 'Resume' }))
    await user.click(await screen.findByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog', { name: 'A session is already running here' })).not.toBeInTheDocument()
    expect(platform.resumeSession).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('button', { name: 'Resume' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The session’s folder is gone.')
  })
})

describe('SessionTag for an agent working in the background', () => {
  // inRows renders the tag inside the rows' Autopilot, whose Watch opens a run's view and
  // whose terminal Take over opens the conversation in.
  function inRows(sessions: readonly AgentSession[]) {
    const onWatch = vi.fn()
    const Clock = at()

    render(
      <Clock>
        <AutopilotRowsContext value={{ names: new Map(), chips: new Map(), onWatch, terminal: 'iterm2' }}>
          <SessionTag sessions={sessions} agents={agents()} />
        </AutopilotRowsContext>
      </Clock>,
    )

    return onWatch
  }

  it('offers Watch, Stop, and Take over in place of Show in terminal and Resume', async () => {
    const user = userEvent.setup()
    platform.autopilotRunBySession.mockResolvedValue(81)
    const onWatch = inRows([
      agentSession({ id: 'run', state: 'working', runId: 44, canFocus: false, canResume: false }),
    ])

    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [run = document.body] = entries(1)
    expect(within(run).queryByRole('button', { name: 'Show in terminal' })).not.toBeInTheDocument()
    expect(within(run).queryByRole('button', { name: 'Resume' })).not.toBeInTheDocument()

    await user.click(within(run).getByRole('button', { name: 'Watch' }))
    expect(onWatch).toHaveBeenCalledExactlyOnceWith('run')

    await user.click(within(run).getByRole('button', { name: 'Stop' }))
    expect(platform.autopilotRunBySession).toHaveBeenCalledWith('run')
    expect(platform.stopAutopilotRun).toHaveBeenCalledExactlyOnceWith(81)

    await user.click(within(run).getByRole('button', { name: 'Take over' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Take over this run?' }))
    expect(dialog.getByText(/opens its conversation in iTerm2/)).toBeInTheDocument()
    await user.click(dialog.getByRole('button', { name: 'Take over' }))

    expect(platform.takeOverRun).toHaveBeenCalledExactlyOnceWith({
      sessionId: 'run',
      profile: false,
      alongside: '',
      confirmed: '',
    })
    expect(await within(run).findByRole('status')).toHaveTextContent('Its conversation carries on in iTerm2.')
  })

  it('says a run is gone from the history, and what its take over worked around', async () => {
    const user = userEvent.setup()
    platform.autopilotRunBySession.mockResolvedValue(0)
    platform.takeOverRun.mockResolvedValueOnce(launchResult({ notice: 'Its worktree was already there.' }))
    inRows([agentSession({ id: 'run', state: 'working', runId: 44, canFocus: false, canResume: false })])

    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [run = document.body] = entries(1)
    await user.click(within(run).getByRole('button', { name: 'Stop' }))
    expect(await within(run).findByRole('alert')).toHaveTextContent('That run is no longer in Autopilot’s history.')
    expect(platform.stopAutopilotRun).not.toHaveBeenCalled()

    await user.click(within(run).getByRole('button', { name: 'Take over' }))
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Take over this run?' })).getByRole('button', {
        name: 'Take over',
      }),
    )
    expect(await within(run).findByRole('status')).toHaveTextContent('Its worktree was already there.')

    await user.click(within(run).getByRole('button', { name: 'Take over' }))
    await user.click(
      within(screen.getByRole('dialog', { name: 'Take over this run?' })).getByRole('button', { name: 'Cancel' }),
    )
    expect(screen.queryByRole('dialog', { name: 'Take over this run?' })).not.toBeInTheDocument()
  })

  it('takes an ended run over without stopping it, and offers no Stop', async () => {
    const user = userEvent.setup()
    inRows([
      agentSession({
        id: 'run',
        state: 'ended',
        endedAt: '2026-06-15T11:00:00Z',
        runId: 44,
        canFocus: false,
        canResume: true,
      }),
    ])

    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [run = document.body] = entries(1)
    expect(within(run).queryByRole('button', { name: 'Stop' })).not.toBeInTheDocument()

    await user.click(within(run).getByRole('button', { name: 'Take over' }))
    expect(await screen.findByRole('dialog', { name: 'Take over this conversation?' })).toBeInTheDocument()
  })
})
