import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { renderAt } from '../../test/clock'
import { issue, issueRef, project, pullRef } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ItemLink, LinksContext } from '../ItemLink'
import { IssueHeader } from './IssueHeader'
import { IssueSummary } from './IssueSummary'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

describe('IssueSummary', () => {
  it('shows the pull requests that fix the issue, where each stands, and what mentioned it', async () => {
    renderAt(
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
    expect(within(development).getByRole('heading')).toHaveTextContent('Development7')

    // Each linked item shows its state as a mark.
    const marks = within(development)
      .getAllByRole('img')
      .map((m) => m.getAttribute('aria-label'))
    expect(marks).toEqual([
      'Draft pull request',
      'Merged pull request',
      'Closed pull request',
      'Open pull request',
      'Open pull request',
      'Open issue',
    ])

    await userEvent.click(within(development).getByRole('button', { name: 'and 2 more on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/12')
  })

  it('says when no pull request is linked, nobody is assigned, and there are no labels', () => {
    renderAt(<IssueSummary issue={issue({ fixes: [], mentioned: [], assignees: [], labels: [], participants: [] })} />)

    expect(screen.getByRole('region', { name: 'Development' })).toHaveTextContent('No pull request is linked to it.')
    expect(screen.getByRole('region', { name: 'Assignees' })).toHaveTextContent('No one')
    expect(screen.getByRole('region', { name: 'Labels' })).toHaveTextContent('None')
    expect(screen.getByRole('region', { name: 'Participants' })).toHaveTextContent('No one yet')
    expect(screen.queryByRole('region', { name: 'Milestone' })).not.toBeInTheDocument()
  })

  it('folds the mentions away under their count past three', async () => {
    const mentions = [9, 10, 11, 12].map((n) =>
      issueRef({ repo: 'acme-org/api', number: n, title: `Mention ${String(n)}` }),
    )
    const { rerender } = renderAt(<IssueSummary issue={issue({ mentioned: mentions.slice(0, 3) })} />)

    const mentioned = () => screen.getByText('Mentioned in').closest('details')
    expect(mentioned()).toHaveProperty('open', true)
    expect(screen.getByRole('list', { name: 'Mentioned in' }).children).toHaveLength(3)

    rerender(<IssueSummary issue={issue({ mentioned: [...mentions, pullRef({ state: 'MERGED' })] })} />)
    await waitFor(() => {
      expect(mentioned()).toHaveProperty('open', false)
    })
    expect(mentioned()?.querySelector('summary')).toHaveTextContent('Mentioned in5')
    expect(
      within(screen.getByRole('list', { name: 'Mentioned in', hidden: true }))
        .getAllByRole('img', { hidden: true })
        .map((m) => m.getAttribute('aria-label')),
    ).toEqual(['Open issue', 'Open issue', 'Open issue', 'Open issue', 'Merged pull request'])

    await userEvent.click(screen.getByText('Mentioned in'))
    await waitFor(() => {
      expect(mentioned()).toHaveProperty('open', true)
    })
  })

  it('lists the sub-issues’ progress and the parent first, and shows the sub-issues', async () => {
    const onShowSubIssues = vi.fn()

    renderAt(
      <IssueSummary
        issue={issue({
          projects: [project()],
          milestone: {
            title: '1.5',
            url: 'https://github.com/acme-org/example-repo/milestone/2',
            dueOn: '',
            state: 'OPEN',
            progress: 40,
          },
          subIssuesTotal: 4,
          subIssuesDone: 2,
          parent: issueRef({ number: 2, title: 'Parser', state: 'CLOSED', stateReason: 'COMPLETED' }),
          duplicateOf: issueRef({ number: 3, state: 'CLOSED', stateReason: 'NOT_PLANNED' }),
        })}
        onShowSubIssues={onShowSubIssues}
      />,
    )

    expect(screen.getAllByRole('region').map((r) => r.getAttribute('aria-label'))).toEqual([
      'Sub-issues',
      'Part of',
      'Development',
      'Assignees',
      'Labels',
      'Projects',
      'Milestone',
      'Duplicate of',
      'Participants',
    ])

    const subIssues = screen.getByRole('region', { name: 'Sub-issues' })
    expect(subIssues).toHaveTextContent('2 of 4 done')
    await userEvent.click(within(subIssues).getByRole('button', { name: 'Show' }))
    expect(onShowSubIssues).toHaveBeenCalledOnce()

    expect(within(screen.getByRole('region', { name: 'Part of' })).getByRole('img')).toHaveAccessibleName(
      'Closed as completed',
    )
    expect(within(screen.getByRole('region', { name: 'Duplicate of' })).getByRole('img')).toHaveAccessibleName(
      'Closed as not planned',
    )
    expect(within(screen.getByRole('region', { name: 'Participants' })).getByRole('heading')).toHaveTextContent(
      'Participants2',
    )
  })

  it('offers no Show without a sub-issues tab to show, and no sub-issues line without any', () => {
    const { rerender } = renderAt(<IssueSummary issue={issue({ subIssuesTotal: 2, subIssuesDone: 0 })} />)

    expect(screen.getByRole('region', { name: 'Sub-issues' })).toHaveTextContent('0 of 2 done')
    expect(screen.queryByRole('button', { name: 'Show' })).not.toBeInTheDocument()

    rerender(<IssueSummary issue={issue()} onShowSubIssues={vi.fn()} />)
    expect(screen.queryByRole('region', { name: 'Sub-issues' })).not.toBeInTheDocument()
  })

  it('shows the projects, the milestone, the parent, and who took part', async () => {
    renderAt(
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

    const partOf = within(screen.getByRole('region', { name: 'Part of' })).getByRole('button', { name: '#2 Parser' })
    expect(partOf, 'the title under the name').toHaveClass('flex-col')
    expect(
      within(screen.getByRole('region', { name: 'Duplicate of' })).getByRole('button', { name: '#3 Parser bug' }),
    ).toHaveClass('flex-col')
    expect(
      within(screen.getByRole('region', { name: 'Development' })).getByRole('button', { name: /#14/ }),
    ).not.toHaveClass('flex-col')
    expect(screen.getByRole('region', { name: 'Participants' })).toHaveTextContent('and 4 more on GitHub')
  })

  it('says when a milestone is due, until that day has passed', () => {
    renderAt(
      <IssueSummary
        issue={issue({
          milestone: { title: '2.0', url: '', dueOn: '2026-07-01T00:00:00Z', state: 'OPEN', progress: 0 },
        })}
      />,
    )

    expect(screen.getByRole('region', { name: 'Milestone' })).toHaveTextContent('Due Jul 1, 2026')
  })

  it('says a closed milestone was due, without the warning', () => {
    renderAt(
      <IssueSummary
        issue={issue({
          milestone: { title: '1.0', url: '', dueOn: '2026-01-01T00:00:00Z', state: 'CLOSED', progress: 100 },
        })}
      />,
    )

    expect(screen.getByRole('region', { name: 'Milestone' })).toHaveTextContent('Due Jan 1, 2026')
  })

  it('shows a milestone with no due date', () => {
    renderAt(
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
    const { rerender } = renderAt(
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
    renderAt(<ItemLink linked={pullRef()} from="acme-org/example-repo" />)

    const link = screen.getByRole('button', { name: '#14' })
    expect(link).toHaveAttribute('title', 'Keep the trailing newline\nOpen it on GitHub')
    await userEvent.click(link)
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/14')
  })

  it('opens an item the inbox holds in its sheet', async () => {
    const follow = vi.fn()
    renderAt(
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
    renderAt(<ItemLink linked={issueRef({ repo: 'acme-org/api', title: '' })} from="acme-org/example-repo" titled />)

    expect(screen.getByRole('button', { name: 'acme-org/api#3' })).toBeInTheDocument()
  })

  it('stacks the title under the name, cutting a long repository short but never the number', () => {
    renderAt(
      <>
        <ItemLink
          linked={issueRef({
            repo: 'acme-org/a-rather-long-repository-name',
            number: 363,
            title: 'Add the people directory',
          })}
          from="acme-org/example-repo"
          titled
          stacked
        />
        <ItemLink linked={issueRef({ number: 2, title: '' })} from="acme-org/example-repo" titled stacked />
      </>,
    )

    const link = screen.getByRole('button', {
      name: 'acme-org/a-rather-long-repository-name#363 Add the people directory',
    })
    const [nameLine, title] = [...link.children]
    expect(nameLine).toHaveTextContent(/^acme-org\/a-rather-long-repository-name#363$/)
    expect(within(nameLine as HTMLElement).getByText('acme-org/a-rather-long-repository-name')).toHaveClass('truncate')
    expect(within(nameLine as HTMLElement).getByText('#363')).toHaveClass('shrink-0')
    expect(title).toHaveTextContent('Add the people directory')
    expect(title).toHaveClass('line-clamp-3')
    expect(link).toHaveClass('flex-col')

    expect(screen.getByRole('button', { name: '#2' }).children).toHaveLength(1)
  })
})
