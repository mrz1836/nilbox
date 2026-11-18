import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { targetOf } from '../../lib/autopilot'
import type { Step, Verb } from '../../lib/nextStep'
import type { TriageTab } from '../../lib/triage'
import type { AgentSession, AutopilotMatch, InboxItem } from '../../platform'
import { at } from '../../test/clock'
import { agentSession, agents, autopilotMatch, item } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { AutopilotRowsContext, type AutopilotRows } from '../autopilot/AutopilotChip'
import type { TriageActions } from '../TriageControls'
import { CardActions, cardOutcomeClass, type CardActionsProps } from './CardActions'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function triageActions(): TriageActions {
  return {
    snoozeDays: 3,
    onDismiss: vi.fn(),
    onSnooze: vi.fn(),
    onRestore: vi.fn(),
    onPin: vi.fn(),
    onUnpin: vi.fn(),
    onRefresh: vi.fn(() => Promise.resolve()),
    onChangeDefault: vi.fn(),
    repoRules: { pinned: [], muted: ['acme-org'] },
    onRepoRule: vi.fn(),
    onRepoRuleSettings: vi.fn(),
  }
}

// step is a step whose primary is verb: loud, in the yours edge, unless changed.
function step(primary: Verb, overrides: Partial<Step> = {}): Step {
  return {
    rule: 'review',
    edge: 'yours',
    story: { text: 'Your review', detail: '', icon: 'dot', triage: false },
    primary,
    quiet: false,
    situation: '',
    ...overrides,
  }
}

interface Setup {
  props?: Partial<CardActionsProps>
  rows?: AutopilotRows | null
}

// renderActions renders a card's actions for a step, in an li with the card's group, and
// returns what they call.
function renderActions(s: Step, { props = {}, rows = null }: Setup = {}) {
  const onOpen = vi.fn<(item: InboxItem, tab: TriageTab) => void>()
  const triage = triageActions()
  const Clock = at()
  const wrap = (children: ReactNode) =>
    rows === null ? children : <AutopilotRowsContext value={rows}>{children}</AutopilotRowsContext>

  render(
    <Clock>
      {wrap(
        <ul>
          <li className="group grid">
            <CardActions
              item={item()}
              tab="inbox"
              step={s}
              session={null}
              sessions={[]}
              agents={agents()}
              triage={triage}
              onOpen={onOpen}
              {...props}
            />
          </li>
        </ul>,
      )}
    </Clock>,
  )

  return { onOpen, triage }
}

describe('CardActions', () => {
  it.each([
    ['review', 'Review'],
    ['rereview', 'Re-review'],
    ['read', 'Read'],
    ['open', 'Open'],
    ['merge', 'Merge'],
  ] as const)('opens the item in its sheet from %s, from the card’s tab', async (verb, label) => {
    const { onOpen } = renderActions(step(verb), { props: { tab: 'snoozed' } })

    await userEvent.click(screen.getByRole('button', { name: label }))

    expect(onOpen).toHaveBeenCalledExactlyOnceWith(item(), 'snoozed')
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('opens the item on GitHub without a sheet to open it in', async () => {
    renderActions(step('review'), { props: { onOpen: undefined } })

    await userEvent.click(screen.getByRole('button', { name: 'Review' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(item().url)
  })

  it('marks Merge with the merge icon', () => {
    renderActions(step('merge', { edge: 'ready' }))

    const merge = screen.getByRole('button', { name: 'Merge' })
    expect(merge.querySelector('svg')).not.toBeNull()
    expect(merge).toHaveClass('border-success/45', 'text-success', 'group-hover:bg-success')
  })

  it('outlines a loud primary in its edge’s color, filling it while the pointer or focus is on the card', () => {
    renderActions(step('review'))

    expect(screen.getByRole('button', { name: 'Review' })).toHaveClass(
      'border-accent/45',
      'text-accent',
      'group-hover:bg-accent',
      'group-hover:text-white',
      'group-focus-within:bg-accent',
    )
  })

  it('draws a quiet primary in the border color, filling it with the raised color', () => {
    renderActions(step('open', { edge: 'waiting', quiet: true }))

    const open = screen.getByRole('button', { name: 'Open' })
    expect(open).toHaveClass('border-border', 'text-text', 'group-hover:bg-raised')
    expect(open).not.toHaveClass('text-muted')
  })

  it('opens the routine’s Needs you card for Decide', async () => {
    const onShow = vi.fn()
    const match: AutopilotMatch = autopilotMatch({ state: 'waiting' })
    renderActions(step('decide', { edge: 'urgent' }), {
      rows: { names: new Map(), chips: new Map([[item().id, match]]), onShow },
    })

    const decide = screen.getByRole('button', { name: 'Decide' })
    expect(decide).toHaveClass('text-warning')

    await userEvent.click(decide)

    expect(onShow).toHaveBeenCalledExactlyOnceWith(targetOf(match))
  })

  it('waits on Decide until the routine’s match is known', () => {
    renderActions(step('decide', { edge: 'urgent' }), { rows: { names: new Map(), chips: new Map() } })

    expect(screen.getByRole('button', { name: 'Decide' })).toBeDisabled()
  })

  it('shows the terminal of an agent waiting on the user, breathing, and says why it couldn’t', async () => {
    const session = agentSession()
    renderActions(step('terminal', { edge: 'urgent' }), { props: { session, sessions: [session] } })

    const show = screen.getByRole('button', { name: 'Show terminal' })
    expect(show).toHaveAttribute('data-moving')
    expect(show).toHaveClass('motion-safe:animate-breathe', 'text-warning')

    await userEvent.click(show)
    expect(platform.focusSession).toHaveBeenCalledExactlyOnceWith(session.id)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    platform.focusSession.mockRejectedValueOnce(new Error('Couldn’t find the session’s tab.'))
    await userEvent.click(show)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Couldn’t find the session’s tab.')
    expect(alert).toHaveClass(...cardOutcomeClass.split(' '))

    platform.focusSession.mockResolvedValueOnce(undefined)
    await userEvent.click(show)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('waits on Show terminal until the session can show its tab', () => {
    const session = agentSession({ canFocus: false })
    renderActions(step('terminal', { edge: 'urgent' }), { props: { session, sessions: [session] } })

    expect(screen.getByRole('button', { name: 'Show terminal' })).toBeDisabled()
  })

  it('watches a background run waiting on the user', async () => {
    const onWatch = vi.fn()
    const session = agentSession({ runId: 12 })
    renderActions(step('watch', { edge: 'urgent' }), {
      props: { session, sessions: [session] },
      rows: { names: new Map(), chips: new Map(), onWatch },
    })

    await userEvent.click(screen.getByRole('button', { name: 'Watch' }))

    expect(onWatch).toHaveBeenCalledExactlyOnceWith(session.id)
  })

  it('opens the sessions panel for Watch where no run view opens (the Watching list)', async () => {
    const session = agentSession({ runId: 12 })
    renderActions(step('watch', { edge: 'urgent' }), { props: { session, sessions: [session] } })

    await userEvent.click(screen.getByRole('button', { name: 'Watch' }))

    expect(within(screen.getByRole('dialog', { name: 'Agent sessions' })).getAllByRole('listitem')).toHaveLength(1)
  })

  it.each(['terminal', 'watch'] as const)('has no %s without a session', (verb) => {
    renderActions(step(verb, { edge: 'urgent' }))

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('opens the sessions panel for What happened', async () => {
    const sessions: AgentSession[] = [agentSession({ state: 'failed' }), agentSession({ id: 'older', state: 'ended' })]
    renderActions(step('sessions', { edge: 'blocked' }), { props: { session: sessions[0] ?? null, sessions } })

    const what = screen.getByRole('button', { name: 'What happened' })
    expect(what).toHaveClass('text-danger')

    await userEvent.click(what)

    expect(within(screen.getByRole('dialog', { name: 'Agent sessions' })).getAllByRole('listitem')).toHaveLength(2)
  })

  it.each([
    ['unsnooze', 'snoozed', 'Unsnooze', 'End the snooze and put it back in the list now'],
    ['restore', 'dismissed', 'Restore', 'Put it back in the list'],
  ] as const)('brings a set-aside item back with %s', async (verb, tab, label, title) => {
    const { triage } = renderActions(step(verb, { edge: 'none', quiet: true }), { props: { tab } })

    const button = screen.getByRole('button', { name: label })
    expect(button).toHaveAttribute('title', title)
    expect(button).toHaveClass('border-border', 'text-text')

    await userEvent.click(button)

    expect(triage.onRestore).toHaveBeenCalledExactlyOnceWith(item())
  })

  it('unmutes what mutes the item', async () => {
    const { triage } = renderActions(step('unmute', { edge: 'none', quiet: true }), { props: { tab: 'muted' } })

    const unmute = screen.getByRole('button', { name: 'Unmute acme-org' })
    expect(unmute).toHaveTextContent('Unmute')
    expect(unmute).toHaveAttribute('title', 'Unmute acme-org: its items go back to the lists')

    await userEvent.click(unmute)

    expect(triage.onRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org', '')
  })

  it('can’t bring an item back without the triage actions, nor from the inbox tab', () => {
    renderActions(step('restore', { edge: 'none', quiet: true }), { props: { tab: 'dismissed', triage: undefined } })

    expect(screen.getByRole('button', { name: 'Restore' })).toBeDisabled()
  })

  it('puts the agent’s slot after the primary, in a column above the card’s toggle', () => {
    renderActions(step('review'), { props: { agent: <button type="button">Claude</button> } })

    const [primary, agent] = screen.getAllByRole('button')
    expect(primary).toHaveAccessibleName('Review')
    expect(agent).toHaveAccessibleName('Claude')
    expect(primary?.parentElement).toHaveClass('relative', 'z-10', 'justify-end')
  })
})

describe('CardActions, compact', () => {
  it('draws the go button as its words in the edge’s tone, filled while the pointer or focus is on the card', () => {
    renderActions(step('merge', { edge: 'ready' }), { props: { compact: true } })

    const merge = screen.getByRole('button', { name: 'Merge' })
    expect(merge.querySelector('svg')).toBeNull()
    expect(merge).toHaveClass('h-6.5', 'text-success', 'group-hover:bg-success', 'group-focus-within:bg-success')
    expect(merge).not.toHaveClass('border')
  })

  it('mutes a quiet go button, filling it with the raised color', () => {
    renderActions(step('open', { edge: 'yours', quiet: true }), { props: { compact: true } })

    expect(screen.getByRole('button', { name: 'Open' })).toHaveClass('text-muted', 'group-hover:bg-raised')
  })

  it('draws an agent’s verbs as words in their tone, Decide and the bring-backs too', async () => {
    const session = agentSession({ runId: 12 })
    const onWatch = vi.fn()
    renderActions(step('watch', { edge: 'urgent' }), {
      props: { session, sessions: [session], compact: true },
      rows: { names: new Map(), chips: new Map(), onWatch },
    })

    const watch = screen.getByRole('button', { name: 'Watch' })
    expect(watch).toHaveClass('text-warning', 'group-hover:bg-warning')
    await userEvent.click(watch)
    expect(onWatch).toHaveBeenCalledExactlyOnceWith(session.id)
  })

  it.each([
    ['sessions', 'blocked', 'What happened', 'text-danger'],
    ['decide', 'urgent', 'Decide', 'text-warning'],
  ] as const)('tones %s’s go button by its edge', (verb, edge, label, color) => {
    const sessions = [agentSession({ state: 'failed' })]
    renderActions(step(verb, { edge }), {
      props: { session: sessions[0] ?? null, sessions, compact: true },
      rows: { names: new Map(), chips: new Map() },
    })

    expect(screen.getByRole('button', { name: label })).toHaveClass(color, 'h-6.5')
  })

  it('brings a set-aside item back from a quiet go button', async () => {
    const { triage } = renderActions(step('restore', { edge: 'none', quiet: true }), {
      props: { tab: 'dismissed', compact: true },
    })

    const restore = screen.getByRole('button', { name: 'Restore' })
    expect(restore).toHaveClass('text-muted', 'h-6.5')

    await userEvent.click(restore)
    expect(triage.onRestore).toHaveBeenCalledExactlyOnceWith(item())
  })
})
