import { render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { timelineRows } from '../../lib/timeline'
import type { TimelineEntry as TimelineEntryData } from '../../platform'
import { at } from '../../test/clock'
import { issueComment, issueEvent, issueRef, lineComment, pullRef, reviewEntry } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { TimelineEntry } from './TimelineEntry'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

// entry renders one timeline entry of issue 12, or of pull request 7, whose line comments
// jump with onJump.
function entry(item: TimelineEntryData, onQuote = vi.fn(), onJump?: (path: string) => void) {
  const [row] = timelineRows([item])

  if (row === undefined) {
    throw new Error('no row')
  }

  return render(
    <TimelineEntry row={row} repo="acme-org/example-repo" author="octo-other" onQuote={onQuote} onJump={onJump} />,
  )
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

describe('TimelineEntry of a review', () => {
  it('shows a review with its state, its text, and its line comments, which jump to the file', async () => {
    const onQuote = vi.fn()
    const onJump = vi.fn()
    entry(
      reviewEntry({
        outdated: true,
        moreLineComments: 2,
        lineComments: [
          lineComment({ startLine: 10, hunk: [' func get() {', '-\treturn nil', '+\treturn cache[key]'] }),
          lineComment({ id: 'PRRC_2', path: 'cache/store.go', line: 3, outdated: true, body: 'And here.', hunk: [] }),
        ],
      }),
      onQuote,
      onJump,
    )

    const review = screen.getByRole('article', { name: 'octo-reviewer’s review' })
    expect(within(review).getByText('Changes requested')).toHaveClass('text-danger')
    expect(review).toHaveTextContent('on an earlier commit')
    expect(review).toHaveTextContent('Two things before this merges.')

    const comments = within(review).getByRole('list', { name: 'Line comments' })
    expect(within(comments).getAllByRole('listitem')).toHaveLength(2)
    const diff = within(comments).getByLabelText('The diff at cache/widget.go:10–12')
    expect(diff).toHaveTextContent('func get() {- return nil+ return cache[key]')
    const [context, removed, added] = [...diff.children]
    expect(context).not.toHaveClass('bg-danger/12')
    expect(removed).toHaveClass('bg-danger/12')
    expect(added).toHaveClass('bg-success/12')
    expect(within(comments).getByText('Outdated')).toBeInTheDocument()

    await userEvent.click(within(comments).getByRole('button', { name: 'cache/store.go:3' }))
    expect(onJump).toHaveBeenCalledWith('cache/store.go')

    await userEvent.click(within(review).getByRole('button', { name: 'Quote reply' }))
    expect(onQuote).toHaveBeenCalledWith('Two things before this merges.')

    await userEvent.click(within(review).getByRole('button', { name: 'on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/pull/7#pullrequestreview-1',
    )
    expect(review).toHaveTextContent('and 2 more line comments on GitHub')
  })

  it('names a line comment’s file only, without a jump, and offers no quote of a review with no text', () => {
    entry(reviewEntry({ body: '', reviewState: 'COMMENTED' }))

    const review = screen.getByRole('article', { name: 'octo-reviewer’s review' })
    expect(within(review).getByText('Commented')).toHaveClass('text-muted')
    expect(within(review).queryByRole('button', { name: 'Quote reply' })).not.toBeInTheDocument()
    expect(within(review).queryByRole('button', { name: 'cache/widget.go:12' })).not.toBeInTheDocument()
    expect(within(review).getByText('cache/widget.go:12')).toBeInTheDocument()
  })

  it('colors an approval', () => {
    entry(reviewEntry({ reviewState: 'APPROVED', lineComments: [] }))

    expect(screen.getByText('Approved')).toHaveClass('text-success')
  })

  it.each([
    [reviewEntry({ body: '', reviewState: 'APPROVED', lineComments: [] }), 'approved these changes'],
    [reviewEntry({ body: '', reviewState: 'CHANGES_REQUESTED', lineComments: [] }), 'requested changes'],
    [
      reviewEntry({ body: ' ', reviewState: 'DISMISSED', lineComments: [], outdated: true }),
      'left a review, since dismissed on an earlier commit',
    ],
  ])('shows a review that says nothing on a line: %#', (review, words) => {
    entry(review)

    expect(screen.queryByRole('article')).not.toBeInTheDocument()
    expect(screen.getByText(/octo-reviewer/).closest('p')).toHaveTextContent(words)
  })

  it('folds a bot’s review, its line comments inside', () => {
    entry(reviewEntry({ actor: 'octo-review-bot', actorIsBot: true, body: '### Summary\n\nNo issues found.' }))

    expect(screen.getByText('octo-review-bot · bot · changes requested · ### Summary')).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'octo-review-bot’s review' })).not.toHaveAttribute('open')
    expect(screen.getByRole('list', { name: 'Line comments' })).toBeInTheDocument()
  })

  it.each([
    [
      issueEvent({ kind: 'merged', label: null, commit: '4444444abc', branch: 'main' }),
      'merged commit 4444444 into main',
    ],
    [issueEvent({ kind: 'merged', label: null }), 'merged this'],
    [
      issueEvent({ kind: 'force_pushed', label: null, from: '2222222aaa', to: '1111111bbb' }),
      'force-pushed the branch from 2222222 to 1111111',
    ],
    [issueEvent({ kind: 'force_pushed', label: null }), 'force-pushed the branch'],
    [
      issueEvent({ kind: 'review_requested', label: null, reviewer: 'acme-org/maintainers' }),
      'requested a review from acme-org/maintainers',
    ],
    [issueEvent({ kind: 'review_requested', label: null }), 'requested a review'],
    [issueEvent({ kind: 'ready_for_review', label: null }), 'marked this ready for review'],
    [issueEvent({ kind: 'converted_to_draft', label: null }), 'marked this as a draft'],
  ])('words a pull request’s event: %#', (event, words) => {
    entry(event)

    expect(screen.getByText(/octo-other/).closest('p')).toHaveTextContent(words)
  })
})
