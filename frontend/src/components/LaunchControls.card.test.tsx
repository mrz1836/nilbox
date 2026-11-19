import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AgentSession } from '../platform'
import { at } from '../test/clock'
import {
  agentSession,
  agents,
  item,
  itemResolution,
  launchDefaults,
  profile,
  prompts,
  resolution,
  situationChoices,
} from '../test/fixtures'
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
    expect(pill).toHaveClass('border-run', 'motion-safe:animate-border-run')
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
    // The split control holds the pill, Show in terminal, then the ▾.
    expect(screen.getAllByRole('button').map((b) => b.getAttribute('aria-label'))).toEqual([
      pill.getAttribute('aria-label'),
      'Show in terminal',
      options().getAttribute('aria-label'),
    ])

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

  it('names the launch apart from the card’s Open while the agent loads', () => {
    renderSlot({
      resolution: null,
      column: (agent) => (
        <div>
          <button type="button">Open</button>
          {agent}
        </div>
      ),
    })

    const launch = screen.getByRole('button', { name: 'Open in the agent' })
    expect(launch).toBeDisabled()
    expect(launch).toHaveTextContent(/^Agent$/)
    expect(screen.getByText('Agent')).toHaveClass('@max-3xl:sr-only')
    expect(screen.getAllByRole('button', { name: 'Open' })).toHaveLength(1)
  })

  it('narrows with its list: the agent’s name, a pill’s words, and its age give way', () => {
    const { unmount } = renderSlot()

    expect(screen.getByText('Claude')).toHaveClass('@max-3xl:sr-only')
    unmount()

    withSession(agentSession({ state: 'working', detail: '' }))

    const age = screen.getByText('1m')
    expect(age.parentElement).toHaveClass('@max-3xl:hidden')
    expect(age.closest('[data-pill-label]')).toHaveTextContent(/^Claude working 1m$/)
  })

  it.each([
    ['no session', null],
    ['a starting session', agentSession({ state: 'starting', canFocus: false })],
    ['a working session', agentSession({ state: 'working' })],
    ['a background run', agentSession({ state: 'working', runId: 31, canFocus: false })],
    ['a done session', agentSession({ state: 'idle' })],
    ['a done session it can’t show', agentSession({ state: 'idle', canFocus: false })],
    ['an ended session', agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:00Z' })],
  ])('draws one split control for %s, closed by the ▾', (_state, s) => {
    const { container } = s === null ? renderSlot() : withSession(s)

    const split = options().parentElement as HTMLElement
    const [first] = screen.getAllByRole('button') as [HTMLElement]
    expect(split.lastElementChild).toBe(options())
    expect(split).toContainElement(first)
    expect(split).toHaveClass('inline-flex', 'min-w-0', 'max-w-full')
    expect(container.querySelectorAll('button[aria-haspopup="dialog"][aria-label^="Launch options"]')).toHaveLength(1)
    // Only the ▾ keeps its width; the first part narrows, its words cut short.
    expect(options()).toHaveClass('shrink-0', 'rounded-r-lg', 'border-l-0')
    expect(first.closest('.min-w-0')).not.toBeNull()
    expect(first).toHaveClass('rounded-l-lg', 'min-w-0')
  })

  it.each([
    ['ended', agentSession({ state: 'ended', endedAt: '2026-06-15T12:00:00Z' }), 'Findings · just now'],
    ['done', agentSession({ state: 'idle', stateAt: '2026-06-15T12:00:00Z' }), 'Claude’s done · just now'],
    ['working', agentSession({ state: 'working', stateAt: '2026-06-15T12:00:00Z' }), 'Claude working just now'],
  ])('cuts a %s pill’s age short before its words, never past its border', (_state, s, words) => {
    withSession(s)

    const age = screen.getByText('just now')
    const text = age.closest('[data-pill-label]') as HTMLElement
    expect(text).toHaveTextContent(words)
    expect(text).toHaveClass('min-w-0', 'truncate')
    // The age comes last in the one line that truncates, so it goes first.
    expect(text.lastElementChild).toContainElement(age)
    expect(text.closest('button')).toHaveClass('min-w-0')
    expect(options()).toHaveClass('shrink-0')
    expect(text.closest('button')).not.toContainElement(options())
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

// fixChecks is the prompt the launch defaults choose when a pull request's checks fail: the
// library's Fix checks, the app's.
const fixChecks = { id: 3, nickname: 'Fix checks', name: 'Fix a pull request’s failing checks', inheritedId: 3 }

// failing is a pull request's resolution whose prompt for failing checks is Fix checks, and
// for every other situation its own, Review.
function failing() {
  return itemResolution({ situations: situationChoices({ checks_failing: fixChecks }) })
}

describe('LaunchControls, card variant, for a situation', () => {
  it('names the launch for the situation’s prompt, asking nothing more until it opens', async () => {
    renderSlot({ situation: 'checks_failing', resolution: failing() })

    const open = screen.getByRole('button', { name: 'Open in Claude: Fix checks' })
    expect(open).toHaveTextContent(/^Fix checks$/)
    expect(open).toHaveAttribute('title', 'Open in Claude with “Fix checks” as default')
    expect(options()).toHaveAccessibleName('Launch options: Fix checks · default')
    expect(platform.resolveLaunch).not.toHaveBeenCalled()

    await userEvent.click(open)

    expect(platform.launch).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ itemId: 1, agent: '', promptId: 0, profile: '', situation: 'checks_failing' }),
    )
    expect(platform.resolveLaunch).not.toHaveBeenCalled()
  })

  it('names a re-review’s launch for its prompt', () => {
    renderSlot({
      situation: 'rereview',
      resolution: itemResolution({
        situations: situationChoices({ rereview: { id: 6, nickname: 'Re-review', name: 'Re-review a pull request' } }),
      }),
    })

    expect(screen.getByRole('button', { name: 'Open in Claude: Re-review' })).toHaveTextContent(/^Re-review$/)
  })

  it('names an issue’s launch for its prompt, and a pull request’s for the agent', () => {
    const { unmount } = renderSlot({
      item: item({ kind: 'issue' }),
      resolution: itemResolution({
        kind: 'issue',
        prompt: { ...resolution().prompt, id: 4, nickname: 'Work on it', name: 'Work on an issue', inheritedId: 4 },
      }),
    })

    expect(screen.getByRole('button', { name: 'Open in Claude: Work on it' })).toHaveTextContent(/^Work on it$/)
    unmount()

    // A situation the resolution doesn't name leaves the launch the agent's.
    renderSlot({ situation: 'conflicts', resolution: itemResolution({ situations: [] }) })

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toHaveTextContent(/^Claude$/)
  })

  it('opens the options on the situation’s prompt, resolved once with the settings it brings', async () => {
    platform.resolveLaunch.mockResolvedValue(
      resolution({
        prompt: { ...resolution().prompt, ...fixChecks },
        model: { value: 'sonnet', source: 'prompt', inherited: 'sonnet', inheritedSource: 'prompt' },
        effort: { value: 'low', source: 'prompt', inherited: 'low', inheritedSource: 'prompt' },
      }),
    )
    renderSlot({ situation: 'checks_failing', resolution: failing() })

    await userEvent.click(options())

    const dialog = within(screen.getByRole('dialog', { name: 'Launch options' }))

    expect(dialog.getByRole('combobox', { name: 'Prompt' })).toHaveValue('3')
    expect(dialog.getByRole('option', { name: 'Fix checks · app default' })).toBeInTheDocument()
    expect(await dialog.findByRole('option', { name: 'Default · Sonnet (prompt)' })).toBeInTheDocument()
    expect(dialog.getByRole('option', { name: 'Default · Low (prompt)' })).toBeInTheDocument()
    expect(platform.resolveLaunch).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ itemId: 1, agent: '', promptId: 0, profile: '', situation: 'checks_failing' }),
    )

    await userEvent.click(dialog.getByRole('button', { name: 'Preview' }))

    await waitFor(() => {
      expect(platform.previewLaunch).toHaveBeenCalledWith(expect.objectContaining({ situation: 'checks_failing' }))
    })

    await userEvent.click(dialog.getByRole('button', { name: 'Open in Claude' }))

    expect(platform.launch).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ promptId: 0, situation: 'checks_failing' }),
    )
    expect(platform.resolveLaunch).toHaveBeenCalledOnce()
  })

  it('saves a prompt chosen for the situation as the situation’s default', async () => {
    renderSlot({
      situation: 'checks_failing',
      resolution: failing(),
      launchDefaults: [launchDefaults({ name: 'example-repo', profiles: {}, prPromptId: 1 })],
    })

    await userEvent.click(options())

    const dialog = within(screen.getByRole('dialog', { name: 'Launch options' }))

    await userEvent.selectOptions(dialog.getByRole('combobox', { name: 'Prompt' }), '2')
    await userEvent.click(dialog.getByRole('button', { name: 'example-repo' }))

    expect(platform.resolveLaunch).toHaveBeenCalledWith(
      expect.objectContaining({ promptId: 2, situation: 'checks_failing' }),
    )
    expect(platform.saveLaunchDefaults).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        owner: 'acme-org',
        name: 'example-repo',
        prPromptId: 1,
        checksPromptId: 2,
        conflictsPromptId: null,
      }),
    )
  })
})
