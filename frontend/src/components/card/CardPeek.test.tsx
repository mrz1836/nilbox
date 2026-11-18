import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { Step } from '../../lib/nextStep'
import type { AgentSession, InboxItem } from '../../platform'
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
import { CardPeek, type CardPeekProps } from './CardPeek'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// open is a step whose primary opens the item: the user's own pull request, waiting.
const open: Step = {
  rule: 'waiting',
  edge: 'waiting',
  story: { text: 'Approved', detail: '', icon: 'dot', triage: false },
  primary: 'open',
  quiet: true,
  situation: '',
}

interface Setup extends Partial<CardPeekProps> {
  rows?: AutopilotRows
}

// openPeek renders an item's peek in a card's grid, and returns the queries scoped to
// it.
function openPeek(it: InboxItem, { rows, ...props }: Setup = {}) {
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
      <ul>
        <li className="group grid">{wrapped}</li>
      </ul>
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

  it('names every project an issue is in, with its status and featured fields', () => {
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

    const roadmap = peek.getByRole('button', {
      name: 'Project Roadmap: In progress, Priority High, Size XL. Open it on GitHub',
    })
    expect(within(roadmap).getByText('High').parentElement).toHaveStyle({ borderColor: '#fa4549b3' })
    expect(within(roadmap).getByText('XL').parentElement).toHaveClass('border-border')
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
    expect(peek.queryByRole('button', { name: /^(Pin|Unpin)$/ })).not.toBeInTheDocument()

    await userEvent.click(peek.getByRole('button', { name: 'GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith(item().url)
  })

  it('offers no pin for an item pinned only with its repository', () => {
    const peek = openPeek(item({ details: details(), repoRule: 'pinned' }), {
      triage: {
        snoozeDays: 3,
        onDismiss: vi.fn(),
        onSnooze: vi.fn(),
        onRestore: vi.fn(),
        onPin: vi.fn(),
        onUnpin: vi.fn(),
        onRefresh: vi.fn(() => Promise.resolve()),
        onChangeDefault: vi.fn(),
        repoRules: { pinned: ['acme-org'], muted: [] },
        onRepoRule: vi.fn(),
        onRepoRuleSettings: vi.fn(),
        onMarkRead: vi.fn(),
        onMarkUnread: vi.fn(),
      },
    })

    expect(peek.getByRole('button', { name: 'Snooze' })).toBeInTheDocument()
    expect(peek.queryByRole('button', { name: /^(Pin|Unpin)$/ })).not.toBeInTheDocument()
  })
})
