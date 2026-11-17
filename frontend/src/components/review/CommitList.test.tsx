import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { issueComment, review, reviewCommit, yourReview } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { CommitList } from './CommitList'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const a = 'a'.repeat(40)
const b = 'b'.repeat(40)
const c = 'c'.repeat(40)
const d = 'd'.repeat(40)

// commits are four commits, oldest first.
const commits = [
  reviewCommit({ oid: a, headline: 'Add the cache' }),
  reviewCommit({ oid: b, headline: 'Expire old entries' }),
  reviewCommit({ oid: c, headline: 'Address the review', author: 'octo-third' }),
  reviewCommit({ oid: d, headline: 'Fix the tests' }),
]

// rows are the list's rows, commits and dividers, as a screen reader names them.
function rows() {
  return [...screen.getByRole('list', { name: 'Commits' }).children].map((el) =>
    el.getAttribute('role') === 'separator' ? `— ${el.getAttribute('aria-label') ?? ''}` : el.textContent,
  )
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('CommitList', () => {
  it('lists the commits oldest first, marking the reviewed one and those since', () => {
    const scrolled = vi.spyOn(Element.prototype, 'scrollIntoView')
    render(
      <CommitList
        review={review({ commitList: commits, yourReview: yourReview({ commit: b, count: 2 }) })}
        onShowPush={vi.fn()}
      />,
    )

    const shown = rows()
    expect(shown).toHaveLength(5)
    expect(shown[0]).toContain('aaaaaaa Add the cache')
    expect(shown[1]).toContain('Expire old entries')
    expect(shown[1]).toContain('You requested changes')
    expect(shown[2]).toBe('— Since your review')
    expect(shown[3]).toContain('octo-third')
    expect(shown[4]).toContain('Fix the tests')
    expect(screen.getByRole('separator', { name: 'Since your review' })).toHaveAttribute(
      'id',
      'commits-since-your-review',
    )
    expect(scrolled, 'it scrolls to the divider as it first shows').toHaveBeenCalledOnce()
  })

  it('opens a commit on GitHub', async () => {
    render(<CommitList review={review({ commitList: commits })} onShowPush={vi.fn()} />)

    await userEvent.click(screen.getByRole('button', { name: 'ccccccc Address the review' }))

    expect(platform.openExternal).toHaveBeenCalledWith(`https://github.com/acme-org/example-repo/pull/7/commits/${c}`)
  })

  it('marks nothing since a review of the head, and says an approval', () => {
    render(
      <CommitList
        review={review({
          commitList: commits,
          yourReview: yourReview({ state: 'APPROVED', commit: d, since: '', count: 0 }),
        })}
        onShowPush={vi.fn()}
      />,
    )

    expect(screen.queryByRole('separator')).not.toBeInTheDocument()
    expect(rows()[3]).toContain('You approved')
  })

  it('says the branch was rewritten, and shows the force push', async () => {
    const onShowPush = vi.fn()
    const push = issueComment({ id: 'HRFPE_1', kind: 'force_pushed', at: '2026-06-15T11:00:00Z' })
    render(
      <CommitList
        review={review({
          commitList: commits,
          yourReview: yourReview({ commit: 'e'.repeat(40), since: 'rewritten', count: 0, at: '2026-06-15T10:30:00Z' }),
          timeline: { items: [push], total: 1, before: '' },
        })}
        onShowPush={onShowPush}
      />,
    )

    expect(screen.getByText(/The branch was rewritten since you requested changes/)).toBeInTheDocument()
    expect(screen.queryByRole('separator')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Show the force push' }))
    expect(onShowPush).toHaveBeenCalledWith('HRFPE_1')
  })

  it('links the earlier commits on GitHub, and says when the review is before them', async () => {
    render(
      <CommitList
        review={review({
          commitList: commits,
          commits: 104,
          earlierCommits: 100,
          yourReview: yourReview({ commit: 'e'.repeat(40), since: 'beyond', count: 0 }),
        })}
        onShowPush={vi.fn()}
      />,
    )

    expect(screen.getByText(/The commit you reviewed is before the newest 4/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '100 earlier commits on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7/commits')
  })

  it('says when there is nothing to list', () => {
    const { unmount } = render(<CommitList review={review({ fetchedAt: '' })} onShowPush={vi.fn()} />)
    expect(screen.getByText('Not read from GitHub yet.')).toBeInTheDocument()
    unmount()

    render(<CommitList review={review()} onShowPush={vi.fn()} />)
    expect(screen.getByText('No commits.')).toBeInTheDocument()
  })
})
