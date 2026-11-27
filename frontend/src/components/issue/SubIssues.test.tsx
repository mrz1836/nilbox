import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { IssueSubIssue } from '../../platform'
import { nextTask } from '../../test/async'
import { renderAt } from '../../test/clock'
import { issue, issueRef, subIssue } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { SubIssues } from './SubIssues'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// numbered is sub-issue n of issue 12, open or closed for reason.
function numbered(n: number, closed?: 'COMPLETED' | 'NOT_PLANNED' | 'DUPLICATE'): IssueSubIssue {
  return subIssue({
    issue: issueRef({
      number: n,
      title: `Step ${String(n)}`,
      url: `https://github.com/acme-org/example-repo/issues/${String(n)}`,
      state: closed === undefined ? 'OPEN' : 'CLOSED',
      stateReason: closed ?? '',
    }),
    closedAt: closed === undefined ? '' : '2026-06-13T12:00:00Z',
  })
}

// fold is the fold titled title: Open or Done.
function fold(title: string): HTMLDetailsElement {
  const details = screen.getByText(title, { selector: 'summary span' }).closest('details')

  if (details === null) {
    throw new Error(`no fold titled ${title}`)
  }

  return details
}

// names are the sub-issues a list shows, by number, in its order.
function names(label: string): string[] {
  return within(screen.getByRole('list', { name: label, hidden: true }))
    .getAllByRole('listitem', { hidden: true })
    .map((row) => row.querySelector('button span')?.textContent ?? '')
}

describe('SubIssues', () => {
  it('lists what is left first, then what is done, each in GitHub’s order under its count', () => {
    renderAt(
      <SubIssues
        issue={issue({
          subIssuesTotal: 4,
          subIssuesDone: 2,
          subIssues: [numbered(20, 'COMPLETED'), numbered(21), numbered(22, 'NOT_PLANNED'), numbered(23)],
        })}
      />,
    )

    expect(screen.getByText('2 of 4 done')).toBeInTheDocument()
    expect(fold('Open').querySelector('summary')).toHaveTextContent('Open2')
    expect(fold('Done').querySelector('summary')).toHaveTextContent('Done2')
    expect(fold('Open').compareDocumentPosition(fold('Done')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(names('Open sub-issues')).toEqual(['#21', '#23'])
    expect(names('Done sub-issues')).toEqual(['#20', '#22'])
    expect(fold('Open').open).toBe(true)
    expect(fold('Done').open).toBe(true)
  })

  it('shows only the fold that has any', () => {
    renderAt(
      <SubIssues issue={issue({ subIssuesTotal: 1, subIssuesDone: 1, subIssues: [numbered(20, 'COMPLETED')] })} />,
    )

    expect(screen.queryByText('Open', { selector: 'summary span' })).not.toBeInTheDocument()
    expect(fold('Done').open).toBe(true)
  })

  it('folds what is done past five, and keeps the user’s choice', async () => {
    const done = [20, 21, 22, 23, 24, 25].map((n) => numbered(n, 'COMPLETED'))
    const { rerender } = renderAt(
      <SubIssues issue={issue({ subIssuesTotal: 7, subIssuesDone: 6, subIssues: [numbered(19), ...done] })} />,
    )

    expect(fold('Done').open).toBe(false)
    expect(fold('Open').open).toBe(true)

    await userEvent.click(screen.getByText('Done', { selector: 'summary span' }))
    await waitFor(() => {
      expect(fold('Done').open).toBe(true)
    })

    // Another issue with as many done keeps the fold as the user left it.
    rerender(
      <SubIssues issue={issue({ number: 13, subIssuesTotal: 6, subIssuesDone: 6, subIssues: done.toReversed() })} />,
    )
    await nextTask()
    expect(fold('Done').open).toBe(true)
  })

  it('says how each done sub-issue was closed, and when', () => {
    renderAt(
      <SubIssues
        issue={issue({
          subIssuesTotal: 4,
          subIssuesDone: 4,
          subIssues: [
            numbered(20, 'COMPLETED'),
            numbered(21, 'NOT_PLANNED'),
            numbered(22, 'DUPLICATE'),
            { ...numbered(23, 'NOT_PLANNED'), closedAt: '' },
            { ...numbered(24, 'COMPLETED'), closedAt: '' },
          ],
        })}
      />,
    )

    const rows = within(screen.getByRole('list', { name: 'Done sub-issues' })).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent(/^#20 Step 20closed 2d ago$/)
    expect(rows[1]).toHaveTextContent(/^#21 Step 21not planned · closed 2d ago$/)
    expect(rows[2]).toHaveTextContent(/^#22 Step 22duplicate · closed 2d ago$/)
    expect(rows[3]).toHaveTextContent(/^#23 Step 23not planned$/)
    expect(rows[4]).toHaveTextContent(/^#24 Step 24$/)
    expect(rows.map((row) => within(row).getByRole('img').getAttribute('aria-label'))).toEqual([
      'Closed as completed',
      'Closed as not planned',
      'Closed as a duplicate',
      'Closed as not planned',
      'Closed as completed',
    ])
    expect(within(rows[1] as HTMLElement).getByRole('button')).toHaveClass('text-muted')
  })

  it('shows who each is assigned to, and how far along its own sub-issues are', () => {
    renderAt(
      <SubIssues
        issue={issue({
          subIssuesTotal: 3,
          subIssuesDone: 0,
          subIssues: [
            subIssue({
              assignees: [
                { login: 'octo-user', mine: true },
                { login: 'octo-bob', mine: false },
                { login: 'octo-carol', mine: false },
              ],
              moreAssignees: 2,
              subIssuesTotal: 3,
              subIssuesDone: 1,
            }),
            subIssue({ issue: issueRef({ number: 21 }), assignees: [{ login: 'octo-bob', mine: false }] }),
            subIssue({ issue: issueRef({ number: 22 }) }),
          ],
        })}
      />,
    )

    const rows = within(screen.getByRole('list', { name: 'Open sub-issues' })).getAllByRole('listitem')
    const first = within(rows[0] as HTMLElement)
    const pictures = first.getByRole('img', { name: 'Assigned to you, octo-bob, octo-carol, and 2 more' })
    expect(pictures).toHaveAttribute('title', 'Assigned to you, octo-bob, octo-carol, and 2 more')
    expect(pictures).toHaveTextContent('OOO+2')
    expect(first.getByText('Sub-issues: 1 of 3 sub-issues done (33%)')).toBeInTheDocument()
    expect(rows[0]).toHaveTextContent('1/3')
    expect(within(rows[0] as HTMLElement).getByRole('button')).not.toHaveClass('text-muted')

    expect(within(rows[1] as HTMLElement).getByRole('img', { name: 'Assigned to octo-bob' })).toBeInTheDocument()
    expect(within(rows[2] as HTMLElement).getAllByRole('img')).toHaveLength(1)
    expect(rows[2]).not.toHaveTextContent('/')
  })

  it('says GitHub has more than it read, and opens them all there', async () => {
    renderAt(
      <SubIssues issue={issue({ subIssuesTotal: 30, subIssuesDone: 10, subIssues: [numbered(20), numbered(21)] })} />,
    )

    expect(screen.getByText(/Showing 2 of 30/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Open all on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/12')
  })

  it('says GitHub lists sub-issues this account can’t read', async () => {
    const { rerender } = renderAt(<SubIssues issue={issue({ subIssuesTotal: 3, subIssuesDone: 1, subIssues: [] })} />)

    expect(screen.getByText(/GitHub lists 3 sub-issues this account can’t read/)).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Open on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/12')

    rerender(<SubIssues issue={issue({ subIssuesTotal: 3, subIssues: [], fetchedAt: '' })} />)
    expect(screen.getByText('Not read from GitHub yet.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open on GitHub' })).not.toBeInTheDocument()
  })
})
