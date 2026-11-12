import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { AgentSession } from '../platform'
import { at } from '../test/clock'
import { agentSession, agents, launchHold, launchResult, runningHold } from '../test/fixtures'
import { platform } from '../test/platform'
import { SessionTag } from './SessionTag'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// entries returns the panel's sessions, as many as want.
function entries(want: number): HTMLElement[] {
  const found = within(screen.getByRole('dialog', { name: 'Agent sessions' })).getAllByRole('listitem')
  expect(found).toHaveLength(want)

  return found
}

function renderTag(sessions: readonly AgentSession[]) {
  return render(<SessionTag sessions={sessions} agents={agents()} />, { wrapper: at() })
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
    expect(within(newest).queryByRole('button', { name: 'Resume' })).toBeNull()

    expect(older).toHaveTextContent('Codex')
    expect(older).toHaveTextContent('work profile · resumed')
    expect(older).toHaveTextContent('ended')
    expect(within(older).queryByRole('button', { name: 'Show in terminal' })).toBeNull()
    expect(within(older).getByRole('button', { name: 'Resume' })).toBeEnabled()
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
    expect(within(running).queryByRole('alert')).toBeNull()

    platform.showSessionFolder.mockRejectedValueOnce(
      new Error(`~/projects/.${brand.slug}/worktrees/example-repo/pr-7 isn't there anymore.`),
    )
    await user.click(within(done).getByRole('button', { name: 'Show in Finder' }))

    expect(platform.showSessionFolder).toHaveBeenLastCalledWith('older')
    expect(await within(done).findByRole('alert')).toHaveTextContent("isn't there anymore.")
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
    expect(platform.resumeSession).toHaveBeenCalledWith({ sessionId: 'older', alongside: '', confirmed: '' })

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
    expect(screen.queryByRole('alert')).toBeNull()

    await user.click(screen.getByRole('button', { name: 'Copy session ID' }))
    expect(screen.queryByRole('status')).toBeNull()
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
    expect(screen.queryByRole('alert')).toBeNull()

    await user.click(screen.getByRole('button', { name: 'Copy session ID' }))
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).toBeNull()
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

    const request = { sessionId: 'older', alongside: '', confirmed: '' }
    expect(platform.resumeSession).toHaveBeenNthCalledWith(2, { ...request, alongside: running.key })
    expect(platform.resumeSession).toHaveBeenNthCalledWith(3, {
      ...request,
      alongside: running.key,
      confirmed: files.key,
    })
    expect(screen.queryByRole('alert')).toBeNull()
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
    expect(screen.queryByRole('dialog', { name: 'A session is already running here' })).toBeNull()
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

    expect(screen.queryByRole('dialog', { name: 'A session is already running here' })).toBeNull()
    expect(platform.resumeSession).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('button', { name: 'Resume' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The session’s folder is gone.')
  })
})
