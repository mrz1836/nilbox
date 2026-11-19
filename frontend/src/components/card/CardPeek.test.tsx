import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { CardPrefsContext, defaultCardPrefs } from '../../lib/cardPrefs'
import type { Step } from '../../lib/nextStep'
import type { AgentSession, InboxItem, RepoRules } from '../../platform'
import { at } from '../../test/clock'
import {
  agentSession,
  agents,
  autopilotMatch,
  details,
  fileCheck,
  item,
  project,
  workflowsGroup,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { AutopilotRowsContext, type AutopilotRows } from '../autopilot/AutopilotChip'
import type { TriageActions } from '../TriageControls'
import { CardPeek, type CardPeekProps } from './CardPeek'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// open is a step whose primary opens the item: the user's own pull request, waiting.
const open: Step = {
  rule: 'waiting',
  edge: 'waiting',
  story: { text: 'Approved', detail: '', icon: 'dot', triage: false, state: '' },
  primary: 'open',
  quiet: true,
  situation: '',
}

// triageActions are a row's triage, each a mock.
function triageActions(repoRules: RepoRules = { pinned: [], muted: [] }): TriageActions {
  return {
    snoozeDays: 3,
    onDismiss: vi.fn(),
    onSnooze: vi.fn(),
    onRestore: vi.fn(),
    onPin: vi.fn(),
    onUnpin: vi.fn(),
    onRefresh: vi.fn(() => Promise.resolve()),
    onChangeDefault: vi.fn(),
    repoRules,
    onRepoRule: vi.fn(),
    onRepoRuleSettings: vi.fn(),
    onMarkRead: vi.fn(),
    onMarkUnread: vi.fn(),
  }
}

interface Setup extends Partial<CardPeekProps> {
  rows?: AutopilotRows
  // unreadDots is Settings → Cards' Unread dots, on unless the test turns it off.
  unreadDots?: boolean
}

// openPeek renders an item's peek in a card's grid, and returns the queries scoped to
// it.
function openPeek(it: InboxItem, { rows, unreadDots = true, ...props }: Setup = {}) {
  const Clock = at()
  const peek = (
    <CardPeek
      id="peek"
      item={it}
      tab="inbox"
      step={open}
      session={null}
      sessions={[]}
      agents={agents()}
      launch={null}
      {...props}
    />
  )
  const wrapped: ReactNode =
    rows === undefined ? peek : <AutopilotRowsContext value={rows}>{peek}</AutopilotRowsContext>

  render(
    <Clock>
      <CardPrefsContext value={{ ...defaultCardPrefs, unread: unreadDots }}>
        <ul>
          <li className="group grid">{wrapped}</li>
        </ul>
      </CardPrefsContext>
    </Clock>,
  )

  return within(screen.getByRole('region', { name: 'Details' }))
}

describe('CardPeek', () => {
  it('names a review’s reviewers once the author pushed, before anyone has reviewed', () => {
    const peek = openPeek(
      item({
        details: details({
          pushedSinceReview: true,
          reviewRequestedAgain: true,
          reviews: { decision: '', reviewers: 0, reviewed: 0, approvals: 0, changesRequested: 0 },
        }),
      }),
    )

    expect(peek.getByText(/^You asked for changes/)).toHaveTextContent(
      'You asked for changes, and the author pushed since, and asked for your review again · no reviewers yet',
    )
  })

  it('says the reviews signal’s sentence alone while nothing was pushed since', () => {
    const peek = openPeek(item({ details: details() }))

    expect(peek.getByText('Reviews: 1 of 2 reviewers reviewed, 1 approved')).toBeInTheDocument()
  })

  it('draws only the checks that ran, and the size of a change without a branch', () => {
    const peek = openPeek(item({ branch: '', details: details() }))

    const checks = peek.getByText(/^Checks: 12 passed/)
    expect([...checks.querySelectorAll('[data-segment]')].map((s) => s.getAttribute('data-segment'))).toEqual([
      'passed',
      'skipped',
    ])
    expect(peek.queryByText('Branch')).not.toBeInTheDocument()
    expect(peek.queryByText(/^Sensitive files/)).not.toBeInTheDocument()
  })

  it('lists sensitive files in the warning color when none is a danger', () => {
    const peek = openPeek(
      item({ details: details(), files: fileCheck({ groups: [workflowsGroup({ level: 'warning' })] }) }),
    )

    expect(peek.getByText('Sensitive files · 1')).toHaveClass('text-warning')
    expect(peek.getByRole('region', { name: 'Workflows and CI' })).toBeInTheDocument()
  })

  it('leaves out the sensitive files of a check that found none', () => {
    const peek = openPeek(item({ details: details(), files: fileCheck({ status: 'clear', groups: [] }) }))

    expect(peek.queryByText(/^Sensitive files/)).not.toBeInTheDocument()
  })

  it('says an item is assigned to the user, with its comments', () => {
    const assigned = openPeek(item({ kind: 'issue', assigned: true, details: details({ comments: 1 }) }))
    expect(assigned.getByText('Assigned to you · 1 comment')).toBeInTheDocument()
  })

  it('names every project an issue is in, with its status and every field by name', () => {
    const fields = [
      { name: 'Phase', value: 'Beta', color: '' },
      { name: 'Size', value: 'XL', color: '' },
      { name: 'Priority', value: 'High', color: '#fa4549' },
    ]
    const peek = openPeek(
      item({
        kind: 'issue',
        details: details({
          projects: [
            project({ fields }),
            project({
              title: 'Sprint',
              url: 'https://github.com/orgs/acme-org/projects/2',
              status: '',
              statusColor: '',
            }),
          ],
        }),
      }),
    )

    // The peek lists each field under its name, so the link leaves the card's featured pills
    // out.
    const roadmap = peek.getByRole('button', { name: 'Project Roadmap: In progress. Open it on GitHub' })
    expect(within(roadmap).queryByText('High')).not.toBeInTheDocument()
    expect(peek.getAllByRole('term').map((term) => term.textContent)).toEqual(
      expect.arrayContaining(['Phase', 'Size', 'Priority']),
    )
    // A single-select option is a pill outlined in its color; any other value is text.
    expect(peek.getByText('Priority').nextElementSibling?.firstElementChild).toHaveStyle({
      borderColor: '#fa4549b3',
    })
    expect(peek.getByText('Size').nextElementSibling).toHaveTextContent('XL')
    expect(peek.getByText('Size').nextElementSibling?.firstElementChild).not.toHaveClass('rounded-full')
    expect(peek.getByRole('button', { name: 'Project Sprint. Open it on GitHub' })).toBeInTheDocument()
    expect(peek.queryByText('Sub-issues')).not.toBeInTheDocument()
  })

  it('names the teams a review is asked of', () => {
    const peek = openPeek(item({ reviewRequest: 'team', reviewTeams: ['platform', 'security'], details: null }))

    expect(peek.getByText('Review requested via @platform, @security')).toBeInTheDocument()
  })

  it('leaves out why for an item with no reason', () => {
    const peek = openPeek(item({ details: details({ comments: 0, labels: [] }) }))

    expect(peek.queryByText('Why it’s here')).not.toBeInTheDocument()
    expect(peek.getByRole('region', { name: 'Around it' })).toBeInTheDocument()
  })

  it('says how a session started when it says, and nothing more when it doesn’t', () => {
    const quiet: AgentSession = agentSession({ state: 'idle', permission: '', worktree: '', detail: '' })
    const peek = openPeek(item({ details: details() }), { session: quiet, sessions: [quiet] })

    expect(peek.getByText(/^Claude · idle ·/)).toHaveClass('text-success')
    expect(peek.getByText('in the clone')).toBeInTheDocument()
  })

  it('says what a routine did with an item it didn’t pin', () => {
    const matched = openPeek(item({ details: details() }), {
      rows: { names: new Map(), chips: new Map([[1, autopilotMatch()]]) },
    })
    expect(matched.getByText(/Snooze passing bot PRs: Snoozed/)).toBeInTheDocument()
    expect(matched.queryByText(/^Pinned by/)).not.toBeInTheDocument()
  })

  it('names the routine that pinned an item no routine matched', () => {
    const peek = openPeek(item({ details: details(), pinnedAt: '2026-06-15T11:00:00Z', pinnedBy: 4 }), {
      rows: { names: new Map([[4, 'Pin outside PRs']]), chips: new Map() },
    })

    expect(peek.getByText(/^Pinned by/)).toHaveTextContent('Pinned by Pin outside PRs')
  })

  it('says nothing of Autopilot while it is off', () => {
    const peek = openPeek(item({ details: details(), pinnedAt: '2026-06-15T11:00:00Z', pinnedBy: 4 }))

    expect(peek.queryByText('Autopilot')).not.toBeInTheDocument()
  })

  it('offers no triage without the row’s, and still opens GitHub', async () => {
    const peek = openPeek(item({ details: details() }))

    expect(peek.queryByRole('button', { name: 'Snooze' })).not.toBeInTheDocument()
    expect(peek.queryByRole('button', { name: /^(Pin|Unpin|Mark as unread)$/ })).not.toBeInTheDocument()

    await userEvent.click(peek.getByRole('button', { name: 'GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith(item().url)
  })

  it('offers no pin for an item pinned only with its repository', () => {
    const peek = openPeek(item({ details: details(), repoRule: 'pinned' }), {
      triage: triageActions({ pinned: ['acme-org'], muted: [] }),
    })

    expect(peek.getByRole('button', { name: 'Snooze' })).toBeInTheDocument()
    expect(peek.queryByRole('button', { name: /^(Pin|Unpin)$/ })).not.toBeInTheDocument()
  })

  it('offers Mark as read on an unread item, after GitHub, which marks it read too', async () => {
    const unread = item({ details: details(), unread: true })
    const triage = triageActions()
    const peek = openPeek(unread, { triage })

    const bar = peek.getAllByRole('button').map((b) => b.textContent)
    expect(bar.slice(-2)).toEqual(['GitHub', 'Mark as read'])

    await userEvent.click(peek.getByRole('button', { name: 'Mark as read' }))
    expect(triage.onMarkRead).toHaveBeenCalledExactlyOnceWith(unread)

    await userEvent.click(peek.getByRole('button', { name: 'GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith(unread.url)
    expect(triage.onMarkRead, 'opening it on GitHub sees it').toHaveBeenCalledTimes(2)
    expect(triage.onMarkUnread).not.toHaveBeenCalled()
  })

  it('offers Mark as unread on a read item', async () => {
    const read = item({ details: details() })
    const triage = triageActions()
    const peek = openPeek(read, { triage })

    expect(peek.queryByRole('button', { name: 'Mark as read' })).not.toBeInTheDocument()
    await userEvent.click(peek.getByRole('button', { name: 'Mark as unread' }))
    expect(triage.onMarkUnread).toHaveBeenCalledExactlyOnceWith(read)
  })

  it('offers neither mark while the cards show no unread dots, and still marks an item opened on GitHub read', async () => {
    const unread = item({ details: details(), unread: true })
    const triage = triageActions()
    const peek = openPeek(unread, { triage, unreadDots: false })

    expect(peek.queryByRole('button', { name: /^Mark as/ })).not.toBeInTheDocument()
    await userEvent.click(peek.getByRole('button', { name: 'GitHub' }))
    expect(triage.onMarkRead).toHaveBeenCalledExactlyOnceWith(unread)
  })
})
