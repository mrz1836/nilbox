import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { AgentSession } from '../platform'
import { at } from '../test/clock'
import { agentSession, agents } from '../test/fixtures'
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
    expect(tag).toHaveAttribute('title', 'Claude needs approval: Permission prompt')
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

  it('shows the terminal, resumes, and copies the session ID', async () => {
    const user = userEvent.setup()
    const ended = agentSession({ id: 'older', state: 'ended', endedAt: '2026-06-15T10:00:00Z', canResume: true })

    renderTag([agentSession(), ended])
    await user.click(screen.getByRole('button', { name: /Claude session/ }))

    const [running = document.body, done = document.body] = entries(2)

    await user.click(within(running).getByRole('button', { name: 'Show in terminal' }))
    expect(platform.focusSession).toHaveBeenCalledWith('0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70')

    await user.click(within(done).getByRole('button', { name: 'Resume' }))
    expect(platform.resumeSession).toHaveBeenCalledWith('older')

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
})
