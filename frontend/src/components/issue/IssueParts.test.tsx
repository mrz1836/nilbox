import { render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { timelineRows } from '../../lib/issues'
import type { IssueTimelineItem } from '../../platform'
import { at } from '../../test/clock'
import { issue, issueComment, issueEvent, issueRef, project, pullRef } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ItemLink, LinksContext } from '../ItemLink'
import { IssueHeader } from './IssueHeader'
import { IssueSummary } from './IssueSummary'
import { TimelineEntry } from './TimelineEntry'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

// entry renders one timeline entry of issue 12.
function entry(item: IssueTimelineItem, onQuote = vi.fn()) {
  const [row] = timelineRows([item])

  if (row === undefined) {
    throw new Error('no row')
  }

  return render(<TimelineEntry row={row} repo="acme-org/example-repo" author="octo-other" onQuote={onQuote} />)
}

describe('TimelineEntry', () => {
  it('shows a comment with who wrote it, and opens it on GitHub', async () => {
    entry(issueComment({ editedAt: '2026-06-15T11:30:00Z', actorIsBot: false }))

    const comment = screen.getByRole('article', { name: 'octo-other’s comment' })
    expect(comment).toHaveTextContent('Author')
    expect(comment).toHaveTextContent('· edited')
    await userEvent.click(within(comment).getByRole('button', { name: 'Open the comment on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/issues/12#issuecomment-2',
    )
  })

  it('shows a comment with no text, and a long one cut short', async () => {
    entry(issueComment({ body: ' ', trimmed: true, actor: '' }))

    const comment = screen.getByRole('article', { name: 'ghost’s comment' })
    expect(comment).toHaveTextContent('No text.')
    await userEvent.click(within(comment).getByRole('button', { name: 'Read it all on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/issues/12#issuecomment-2',
    )
  })

  it('folds a hidden comment and a bot’s', () => {
    const { unmount } = entry(issueComment({ minimized: true, minimizedReason: 'OFF_TOPIC', trimmed: true, url: '' }))
    expect(screen.getByText('octo-other’s comment was hidden as off topic')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Read it all on GitHub' })).not.toBeInTheDocument()
    unmount()

    const view = entry(issueComment({ minimized: true }))
    expect(screen.getByText('octo-other’s comment was hidden')).toBeInTheDocument()
    view.unmount()

    const { unmount: unmountStale } = entry(
      issueComment({ actor: 'github-actions', actorIsBot: true, body: '\nThis issue is stale.\nIt closes soon.' }),
    )
    expect(screen.getByText('github-actions · bot · This issue is stale.')).toBeInTheDocument()
    unmountStale()

    entry(issueComment({ actor: 'github-actions', actorIsBot: true, body: '' }))
    expect(screen.getByText('github-actions · bot ·')).toBeInTheDocument()
  })

  it.each([
    [
      issueEvent({ kind: 'mentioned', label: null, ref: pullRef(), willClose: true }),
      'mentioned this in #14 Keep the trailing newline, which closes it once merged',
    ],
    [issueEvent({ kind: 'linked', label: null, ref: pullRef() }), 'linked #14'],
    [issueEvent({ kind: 'unlinked', label: null, ref: pullRef() }), 'unlinked #14'],
    [
      issueEvent({ kind: 'closed', label: null, stateReason: 'NOT_PLANNED', ref: pullRef() }),
      'closed this as not planned in #14',
    ],
    [
      issueEvent({ kind: 'closed', label: null, stateReason: 'COMPLETED', commit: 'abcdef1234' }),
      'closed this as completed in commit abcdef1',
    ],
    [
      issueEvent({ kind: 'closed', label: null, stateReason: 'DUPLICATE', duplicateOf: issueRef() }),
      'closed this as a duplicate of #3',
    ],
    [issueEvent({ kind: 'closed', label: null }), 'closed this'],
    [issueEvent({ kind: 'reopened', label: null }), 'reopened this'],
    [issueEvent({ kind: 'assigned', label: null, assignee: 'octo-other' }), 'self-assigned this'],
    [issueEvent({ kind: 'assigned', label: null, assignee: 'octo-user' }), 'assigned octo-user'],
    [issueEvent({ kind: 'unassigned', label: null, assignee: 'octo-other' }), 'removed their assignment'],
    [issueEvent({ kind: 'unassigned', label: null, assignee: '' }), 'unassigned ghost'],
    [
      issueEvent({ kind: 'renamed', label: null, from: 'Parser bug', to: 'Parser drops the newline' }),
      'changed the title from “Parser bug” to “Parser drops the newline”',
    ],
    [issueEvent({ kind: 'duplicate', label: null, ref: issueRef() }), 'marked this and #3 Parser bug as duplicates'],
    [issueEvent({ kind: 'labeled', label: null }), 'added a label'],
    [issueEvent({ kind: 'unlabeled', label: null }), 'removed a label'],
  ])('words an event: %#', (event, words) => {
    entry(event)

    expect(screen.getByText(/octo-other/).closest('p')).toHaveTextContent(words)
  })

  it('shows the labels one person changed at once on one line, a bot marked', () => {
    const rows = timelineRows([
      issueEvent({ actorIsBot: true }),
      issueEvent({ id: 'l2', kind: 'unlabeled', label: { name: 'triage', color: '' } }),
    ])
    const [row] = rows

    if (row === undefined) {
      throw new Error('no row')
    }

    render(<TimelineEntry row={row} repo="acme-org/example-repo" author="" onQuote={vi.fn()} />)

    const line = screen.getByText(/octo-other/).closest('p')
    expect(line).toHaveTextContent('octo-otherbotaddedbugandremovedtriage')
  })

  it('shows a label removed alone', () => {
    entry(issueEvent({ kind: 'unlabeled' }))

    expect(screen.getByText(/octo-other/).closest('p')).toHaveTextContent('removedbug')
  })
})

describe('IssueSummary', () => {
  it('shows the pull requests that fix the issue, where each stands, and what mentioned it', async () => {
    render(
      <IssueSummary
        issue={issue({
          fixes: [
            {
              pr: pullRef({ draft: true }),
              base: 'main',
              mergedAt: '',
              checks: 'failing',
              review: 'changes_requested',
            },
            {
              pr: pullRef({ number: 15, state: 'MERGED' }),
              base: 'release-1.2',
              mergedAt: '2026-06-15T10:00:00Z',
              checks: '',
              review: '',
            },
            {
              pr: pullRef({ number: 16, state: 'CLOSED', repo: 'acme-org/api' }),
              base: 'main',
              mergedAt: '',
              checks: '',
              review: '',
            },
            { pr: pullRef({ number: 17 }), base: 'main', mergedAt: '', checks: 'passing', review: '' },
            { pr: pullRef({ number: 18 }), base: 'main', mergedAt: '', checks: '', review: 'approved' },
          ],
          moreFixes: 2,
          mentioned: [issueRef({ repo: 'acme-org/api', number: 9, title: 'Crash on start' })],
        })}
      />,
    )

    const development = screen.getByRole('region', { name: 'Development' })
    expect(development).toHaveTextContent('draft · checks failing · changes requested')
    expect(development).toHaveTextContent('merged into release-1.2')
    expect(development).toHaveTextContent('acme-org/api#16')
    expect(development).toHaveTextContent('open · checks passing')
    expect(development).toHaveTextContent('open · approved')
    expect(development).toHaveTextContent('Mentioned in')
    expect(development).toHaveTextContent('acme-org/api#9 Crash on start')

    await userEvent.click(within(development).getByRole('button', { name: 'and 2 more on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/12')
  })

  it('says when no pull request is linked, nobody is assigned, and there are no labels', () => {
    render(<IssueSummary issue={issue({ fixes: [], mentioned: [], assignees: [], labels: [], participants: [] })} />)

    expect(screen.getByRole('region', { name: 'Development' })).toHaveTextContent('No pull request is linked to it.')
    expect(screen.getByRole('region', { name: 'Assignees' })).toHaveTextContent('No one')
    expect(screen.getByRole('region', { name: 'Labels' })).toHaveTextContent('None')
    expect(screen.getByRole('region', { name: 'Participants' })).toHaveTextContent('No one yet')
    expect(screen.queryByRole('region', { name: 'Milestone' })).not.toBeInTheDocument()
  })

  it('shows the projects, the milestone, the sub-issues, the parent, and who took part', async () => {
    render(
      <IssueSummary
        issue={issue({
          projects: [project()],
          milestone: {
            title: '1.5',
            url: 'https://github.com/acme-org/example-repo/milestone/2',
            dueOn: '2026-06-01T00:00:00Z',
            state: 'OPEN',
            progress: 40,
          },
          subIssuesTotal: 3,
          subIssuesDone: 1,
          subIssues: [issueRef({ number: 20 })],
          parent: issueRef({ number: 2, title: 'Parser' }),
          duplicateOf: issueRef({ number: 3 }),
          moreParticipants: 4,
        })}
      />,
    )

    expect(screen.getByRole('region', { name: 'Projects' })).toBeInTheDocument()
    const milestone = screen.getByRole('region', { name: 'Milestone' })
    expect(milestone).toHaveTextContent('Overdue since Jun 1, 2026')
    expect(milestone).toHaveTextContent('40% done')
    await userEvent.click(within(milestone).getByRole('button', { name: '1.5' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/milestone/2')

    expect(screen.getByRole('region', { name: 'Sub-issues' })).toHaveTextContent('1 of 3 sub-issues done')
    expect(screen.getByRole('button', { name: 'and 2 more on GitHub' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Part of' })).toHaveTextContent('#2 Parser')
    expect(screen.getByRole('region', { name: 'Duplicate of' })).toHaveTextContent('#3')
    expect(screen.getByRole('region', { name: 'Participants' })).toHaveTextContent('and 4 more on GitHub')
  })

  it('says when a milestone is due, until that day has passed', () => {
    render(
      <IssueSummary
        issue={issue({
          milestone: { title: '2.0', url: '', dueOn: '2026-07-01T00:00:00Z', state: 'OPEN', progress: 0 },
        })}
      />,
    )

    expect(screen.getByRole('region', { name: 'Milestone' })).toHaveTextContent('Due Jul 1, 2026')
  })

  it('says a closed milestone was due, without the warning', () => {
    render(
      <IssueSummary
        issue={issue({
          milestone: { title: '1.0', url: '', dueOn: '2026-01-01T00:00:00Z', state: 'CLOSED', progress: 100 },
        })}
      />,
    )

    expect(screen.getByRole('region', { name: 'Milestone' })).toHaveTextContent('Due Jan 1, 2026')
  })

  it('shows a milestone with no due date', () => {
    render(
      <IssueSummary
        issue={issue({ milestone: { title: '3.0', url: '', dueOn: '', state: 'OPEN', progress: 12.6 } })}
      />,
    )

    const milestone = screen.getByRole('region', { name: 'Milestone' })
    expect(milestone).not.toHaveTextContent('Due')
    expect(milestone).toHaveTextContent('13% done')
  })
})

describe('IssueHeader', () => {
  it('names the card while nothing is read, and nothing before the card comes', () => {
    const { rerender } = render(
      <IssueHeader
        titleId="t"
        issue={null}
        item={undefined}
        position=""
        checking={false}
        prev={null}
        next={null}
        onRefresh={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    expect(screen.getByRole('img', { name: 'Open issue' })).toBeInTheDocument()
    expect(screen.queryByText(/opened/)).not.toBeInTheDocument()

    rerender(
      <IssueHeader
        titleId="t"
        issue={issue({ openedAt: '', type: { name: 'Task', color: '', description: '' } })}
        item={undefined}
        position=""
        checking={false}
        prev={null}
        next={null}
        onRefresh={vi.fn()}
        onClose={vi.fn()}
      />,
    )

    expect(screen.getByText('Task')).toBeInTheDocument()
    expect(screen.queryByText(/opened/)).not.toBeInTheDocument()
  })
})

describe('ItemLink', () => {
  it('names an item of the same repository by its number, and opens it on GitHub', async () => {
    render(<ItemLink linked={pullRef()} from="acme-org/example-repo" />)

    const link = screen.getByRole('button', { name: '#14' })
    expect(link).toHaveAttribute('title', 'Keep the trailing newline\nOpen it on GitHub')
    await userEvent.click(link)
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/14')
  })

  it('opens an item the inbox holds in its sheet', async () => {
    const follow = vi.fn()
    render(
      <LinksContext value={{ holds: (repo, number) => repo === 'acme-org/example-repo' && number === 14, follow }}>
        <ItemLink linked={pullRef()} from="acme-org/example-repo" />
        <ItemLink linked={issueRef()} from="acme-org/example-repo" />
      </LinksContext>,
    )

    const held = screen.getByRole('button', { name: '#14' })
    expect(held).toHaveAttribute('title', 'Keep the trailing newline\nOpen it here')
    await userEvent.click(held)
    expect(follow).toHaveBeenCalledWith('acme-org/example-repo', 14)
    expect(platform.openExternal).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: '#3' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/3')
  })

  it('names one of another repository in full, with its title when asked', () => {
    render(<ItemLink linked={issueRef({ repo: 'acme-org/api', title: '' })} from="acme-org/example-repo" titled />)

    expect(screen.getByRole('button', { name: 'acme-org/api#3' })).toBeInTheDocument()
  })
})
