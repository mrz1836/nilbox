import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { CardLinksContext, linkIndex, type CardLinks } from '../../lib/cardLinks'
import { CardPrefsContext, defaultCardPrefs } from '../../lib/cardPrefs'
import type { Step } from '../../lib/nextStep'
import type { AgentSession, InboxItem, RepoRules } from '../../platform'
import { at } from '../../test/clock'
import {
  agentSession,
  agents,
  autopilotMatch,
  closingPull,
  details,
  fileCheck,
  item,
  list,
  project,
  snapshot,
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
  // links are the inbox's links around the card, none unless given.
  links?: CardLinks
  // unreadDots is Settings → Cards' Unread dots, on unless the test turns it off.
  unreadDots?: boolean
}

// openPeek renders an item's peek in a card's grid, and returns the queries scoped to
// it.
function openPeek(it: InboxItem, { rows, links, unreadDots = true, ...props }: Setup = {}) {
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
        <CardLinksContext value={links ?? null}>
          <ul>
            <li className="group grid">{wrapped}</li>
          </ul>
        </CardLinksContext>
      </CardPrefsContext>
    </Clock>,
  )

  return within(screen.getByRole('region', { name: 'Details' }))
}

describe('CardPeek', () => {
  it('offers to nudge the author of a pull request waiting on them, however long it has been quiet', async () => {
    const onNudge = vi.fn()
    const waiting = item({ author: 'contributor-a', details: details({ viewerVerdict: 'changes' }) })
    const peek = openPeek(waiting, { onNudge })

    await userEvent.click(peek.getByRole('button', { name: 'Nudge' }))

    expect(onNudge).toHaveBeenCalledExactlyOnceWith(waiting)
  })

  it('offers no second Nudge beside a Nudge primary, nor one for a bot or without a way to post', () => {
    const waiting = item({ author: 'contributor-a', details: details({ viewerVerdict: 'changes' }) })
    const nudge: Step = { ...open, rule: 'awaiting_author', primary: 'nudge', quiet: false }

    const peek = openPeek(waiting, { onNudge: vi.fn(), step: nudge })
    expect(peek.getAllByRole('button', { name: 'Nudge' })).toHaveLength(1)
    expect(peek.getByRole('button', { name: 'Nudge' })).toHaveAttribute('data-verb', 'nudge')
  })

  it('offers no nudge for a bot’s pull request, or where the card can’t post one', () => {
    const peek = openPeek(
      item({ author: 'dependabot[bot]', authorIsBot: true, details: details({ viewerVerdict: 'changes' }) }),
      {
        onNudge: vi.fn(),
      },
    )
    expect(peek.queryByRole('button', { name: 'Nudge' })).not.toBeInTheDocument()
  })

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

  it('lists an issue’s Development, each pull request with where it stands, and shows a listed one’s card', async () => {
    const pull = item({ id: 1, number: 8 })
    const issue = item({
      id: 2,
      kind: 'issue',
      number: 5,
      details: details({
        closingPulls: [
          closingPull({ isDraft: true }),
          closingPull({
            repo: 'octo-org/tool',
            number: 3,
            title: 'A stranger’s fix',
            url: 'https://github.com/octo-org/tool/pull/3',
            merged: true,
            trusted: false,
          }),
        ],
      }),
    })
    const links = { index: linkIndex(snapshot({ yours: list([pull, issue]) })), reveal: vi.fn() }
    const peek = openPeek(issue, { links })

    const development = peek.getByText('Development').nextElementSibling
    expect(development).not.toBeNull()
    const rows = within(development as HTMLElement).getAllByRole('listitem')
    expect(rows.map((row) => row.textContent)).toEqual([
      '#8 Fix the flaky retry testdraft',
      'octo-org/tool#3 A stranger’s fixmerged · from outside the project',
    ])
    expect(within(rows[1] as HTMLElement).getByText('merged · from outside the project')).toHaveClass('text-warning')

    await userEvent.click(within(rows[0] as HTMLElement).getByRole('button'))
    expect(links.reveal).toHaveBeenCalledExactlyOnceWith(1)
    await userEvent.click(within(rows[1] as HTMLElement).getByRole('button'))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/octo-org/tool/pull/3')
  })

  describe('a pull request', () => {
    const pull = item({ id: 1, number: 8 })
    const issue = item({
      id: 2,
      kind: 'issue',
      number: 5,
      title: 'The retry test is flaky',
      details: details({ closingPulls: [closingPull()] }),
    })
    const links = (): CardLinks => ({ index: linkIndex(snapshot({ yours: list([pull, issue]) })), reveal: vi.fn() })

    it('lists the listed issues it Closes', () => {
      expect(openPeek(pull, { links: links() }).getByText('Closes').nextElementSibling).toHaveTextContent(
        '#5 The retry test is flaky',
      )
    })

    it('lists nothing when it closes no listed issue', () => {
      expect(openPeek(item({ id: 3, number: 9 }), { links: links() }).queryByText('Closes')).not.toBeInTheDocument()
    })
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
