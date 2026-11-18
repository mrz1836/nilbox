import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AgentSession } from '../platform'
import { at } from '../test/clock'
import { agentSession, agents, item, itemResolution, profile, prompts } from '../test/fixtures'
import { platform } from '../test/platform'
import { AutopilotRowsContext } from './autopilot/AutopilotChip'
import { LaunchControls, type LaunchControlsProps } from './LaunchControls'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const sessionId = '0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70'

// props are a card's agent slot for the fixture item, with no session.
function props(overrides: Partial<LaunchControlsProps> = {}): LaunchControlsProps {
  return {
    variant: 'card',
    item: item(),
    resolution: itemResolution(),
    agents: agents(),
    profiles: [profile()],
    prompts: prompts(),
    launchDefaults: [],
    onDefaultsSaved: vi.fn(() => Promise.resolve()),
    onManagePrompts: vi.fn(),
    onSettingsSaved: vi.fn(),
    ...overrides,
  }
}

// renderSlot renders the slot in a grid, as a card does, with the rows' Autopilot when
// onWatch is given.
function renderSlot(overrides: Partial<LaunchControlsProps> = {}, onWatch?: (sessionId: string) => void) {
  const Clock = at()
  const slot = <LaunchControls {...props(overrides)} />
  const wrapped: ReactNode =
    onWatch === undefined ? (
      slot
    ) : (
      <AutopilotRowsContext value={{ names: new Map(), chips: new Map(), onWatch }}>{slot}</AutopilotRowsContext>
    )

  return render(
    <Clock>
      <div className="grid">{wrapped}</div>
    </Clock>,
  )
}

// withSession renders the slot for a card whose session is s, the item's only one.
function withSession(s: AgentSession, overrides: Partial<LaunchControlsProps> = {}, onWatch?: (id: string) => void) {
  return renderSlot({ running: s, sessions: [s], ...overrides }, onWatch)
}

function options(): HTMLElement {
  return screen.getByRole('button', { name: /^Launch options/ })
}

describe('LaunchControls, card variant', () => {
  it('launches as one split control, the agent named, with the options beside it', async () => {
    renderSlot()

    const open = screen.getByRole('button', { name: 'Open in Claude' })
    expect(open).toHaveTextContent(/^Claude$/)
    expect(open).toHaveAttribute('title', 'Open in Claude with “Review” as default')
    expect(open.querySelector('svg')).not.toBeNull()
    expect(options()).toHaveAccessibleName('Launch options: Review · default')
    expect(options()).toHaveTextContent('')

    await userEvent.click(open)

    expect(platform.launch).toHaveBeenCalledOnce()
    expect(platform.launch.mock.calls[0]?.[0]).toMatchObject({ itemId: 1, agent: '', promptId: 0 })
  })

  it('opens the launch options in their dialog', async () => {
    renderSlot()

    await userEvent.click(options())

    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBeInTheDocument()
    expect(options()).toHaveAttribute('aria-expanded', 'true')
  })

  it('locks the launch of an item from someone the repository doesn’t trust', () => {
    const { container, rerender } = renderSlot({ item: item({ trust: 'outside', trusted: false }) })

    expect(container.querySelector('[title="Launches read-only: an outside contributor’s"]')).not.toBeNull()

    rerender(<LaunchControls {...props()} />)
    expect(container.querySelector('[title^="Launches read-only"]')).toBeNull()
  })

  it.each([
    ['starting', 'Claude starting'],
    ['working', 'Claude working'],
    ['unknown', 'Claude status unavailable'],
  ] as const)('follows a session %s with a moving pill that opens the item’s sessions', async (state, words) => {
    withSession(agentSession({ state, detail: '' }))

    const pill = screen.getByRole('button', {
      name: `Claude session: ${words.replace('Claude ', '')}. Show the item's sessions`,
    })
    expect(pill).toHaveTextContent(`${words} 1m`)
    expect(pill).toHaveAttribute('data-moving')
    expect(pill).toHaveClass('border-run', 'motion-safe:animate-border-run')
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Launch options/ })).not.toBeInTheDocument()

    await userEvent.click(pill)

    expect(within(screen.getByRole('dialog', { name: 'Agent sessions' })).getAllByRole('listitem')).toHaveLength(1)
  })

  it('shows a working session’s terminal beside its pill, and says why it couldn’t', async () => {
    withSession(agentSession({ state: 'working' }), { outcomeClass: 'col-[3/-1] mt-1.5' })

    const show = screen.getByRole('button', { name: 'Show in terminal' })
    await userEvent.click(show)
    expect(platform.focusSession).toHaveBeenCalledExactlyOnceWith(sessionId)

    platform.focusSession.mockRejectedValueOnce(new Error('Couldn’t find the session’s tab.'))
    await userEvent.click(show)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Couldn’t find the session’s tab.')
    expect(alert).toHaveClass('col-[3/-1]')
    expect(alert).not.toHaveClass('col-[2/-1]')
  })

  it('waits for a starting session’s terminal', () => {
    withSession(agentSession({ state: 'starting', canFocus: false }))

    expect(screen.getByRole('button', { name: 'Show in terminal' })).toBeDisabled()
  })

  it('watches a background run beside its pill', async () => {
    const onWatch = vi.fn()
    withSession(agentSession({ state: 'working', runId: 31, canFocus: false }), {}, onWatch)

    expect(screen.queryByRole('button', { name: 'Show in terminal' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Watch' }))

    expect(onWatch).toHaveBeenCalledExactlyOnceWith(sessionId)
  })

  it('can’t watch a background run where no run view opens', () => {
    withSession(agentSession({ state: 'working', runId: 31, canFocus: false }))

    expect(screen.getByRole('button', { name: 'Watch' })).toBeDisabled()
  })

  it.each([
    ['needs approval', agentSession()],
    ['needs input', agentSession({ state: 'needs_input' })],
    ['failed', agentSession({ state: 'failed', endedAt: '' })],
  ])('leaves only the options once a session %s, its primary being the way in', (_state, s) => {
    withSession(s)

    expect(screen.getAllByRole('button')).toEqual([options()])
  })

  it('shows the terminal of a session whose turn is done', async () => {
    withSession(agentSession({ state: 'idle', detail: '' }))

    const done = screen.getByRole('button', { name: 'Claude’s done · 1m' })
    expect(done).toHaveClass('text-success')
    expect(options()).toBeInTheDocument()

    await userEvent.click(done)

    expect(platform.focusSession).toHaveBeenCalledExactlyOnceWith(sessionId)
  })

  it('opens the item’s sessions for a done session whose terminal can’t be shown', async () => {
    withSession(agentSession({ state: 'idle', canFocus: false }))

    await userEvent.click(screen.getByRole('button', { name: 'Claude’s done · 1m' }))

    expect(screen.getByRole('dialog', { name: 'Agent sessions' })).toBeInTheDocument()
    expect(platform.focusSession).not.toHaveBeenCalled()
  })

  it('opens what an ended session found in the item’s sheet, and offers to launch again', async () => {
    const onOpen = vi.fn()
    withSession(agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:00Z', canFocus: false }), { onOpen })

    const findings = screen.getByRole('button', { name: 'Findings · 1m' })
    expect(findings).toHaveClass('text-success')

    await userEvent.click(findings)
    expect(onOpen).toHaveBeenCalledOnce()

    await userEvent.click(options())
    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBeInTheDocument()
  })

  it('opens what an ended session found on GitHub without a sheet', async () => {
    withSession(agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:00Z' }))

    await userEvent.click(screen.getByRole('button', { name: 'Findings · 1m' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(item().url)
  })

  it('opens what a background run found in its run view', async () => {
    const onWatch = vi.fn()
    const onOpen = vi.fn()
    withSession(agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:00Z', runId: 31 }), { onOpen }, onWatch)

    await userEvent.click(screen.getByRole('button', { name: 'Findings · 1m' }))

    expect(onWatch).toHaveBeenCalledExactlyOnceWith(sessionId)
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('frames its slot in the card’s action column', () => {
    renderSlot({
      column: (agent) => (
        <div data-testid="column">
          <button type="button">Review</button>
          {agent}
        </div>
      ),
    })

    const column = within(screen.getByTestId('column'))
    expect(column.getAllByRole('button').map((b) => b.textContent)).toEqual(['Review', 'Claude', ''])
  })

  it('offers a fresh launch beside a session that failed more than a day ago, locked for an outside item', () => {
    const failed = agentSession({ state: 'failed', endedAt: '' })
    const { unmount } = withSession(failed, { sessionFresh: true })

    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
    expect(options()).toBeInTheDocument()
    unmount()

    const { container } = withSession(failed, {
      sessionFresh: false,
      item: item({ trust: 'outside', trusted: false }),
    })

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toHaveTextContent(/^Claude$/)
    expect(options()).toBeInTheDocument()
    expect(container.querySelector('[title="Launches read-only: an outside contributor’s"]')).not.toBeNull()
  })

  it('narrows with its list: the agent’s name, a pill’s words, and its age give way', () => {
    const { unmount } = renderSlot()

    expect(screen.getByText('Claude')).toHaveClass('@max-3xl:sr-only')
    unmount()

    withSession(agentSession({ state: 'working', detail: '' }))

    expect(screen.getByText('Claude working')).toHaveAttribute('data-pill-label')
    expect(screen.getByText('1m').parentElement).toHaveClass('@max-3xl:hidden')
  })

  it('hands the card’s peek its way to the launch options', async () => {
    const { rerender } = renderSlot({ peek: (launch) => <div data-testid="peek">{launch}</div> })

    const peek = within(screen.getByTestId('peek'))
    await userEvent.click(peek.getByRole('button', { name: 'Claude…' }))

    expect(screen.getByRole('dialog', { name: 'Launch options' })).toBeInTheDocument()
    expect(peek.getByRole('button', { name: 'Claude…' })).toHaveAttribute('aria-expanded', 'true')

    rerender(
      <LaunchControls {...props({ resolution: null, peek: (launch) => <div data-testid="peek">{launch}</div> })} />,
    )

    expect(within(screen.getByTestId('peek')).getByRole('button', { name: 'Launch…' })).toBeDisabled()
  })
})
