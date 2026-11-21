import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { deferred } from '../../test/async'
import { renderAt } from '../../test/clock'
import { review, reviewerCandidates } from '../../test/fixtures'
import { platform } from '../../test/platform'
import type { Review, ReviewEntry, Reviewers } from '../../platform'
import { ReviewSummary, type ReviewersEditing } from './ReviewSummary'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// entry is a reviewer as the sheet lists them, with overrides.
function entry(author: string, overrides: Partial<ReviewEntry> = {}): ReviewEntry {
  return {
    author,
    state: '',
    url: '',
    requested: false,
    team: false,
    mine: false,
    bot: false,
    submittedAt: '',
    outdated: false,
    ...overrides,
  }
}

// editable is pull request #7 the user may change the reviewers of: octo-reviewer approved,
// and octo-asked and the team acme-org/ops are asked, as is the user.
function editable(overrides: Partial<Review> = {}): Review {
  return review({
    canEditReviewers: true,
    reviews: [
      entry('octo-reviewer', { state: 'APPROVED', url: 'https://github.com/acme-org/example-repo/pull/7#r1' }),
      entry('octo-asked', { requested: true }),
      entry('octo-user', { requested: true, mine: true }),
      entry('acme-org/ops', { requested: true, team: true }),
    ],
    ...overrides,
  })
}

// editing is what the summary edits the reviewers with, its edit answering as told.
function editing(edit = vi.fn().mockResolvedValue(''), candidates: Reviewers | undefined = reviewerCandidates()) {
  return { candidates, onLearned: vi.fn(), onEdit: edit } satisfies ReviewersEditing
}

// picker opens the reviewer picker, and returns its dialog.
async function picker() {
  await userEvent.click(screen.getByRole('button', { name: 'Edit reviewers' }))

  return within(await screen.findByRole('dialog', { name: 'Reviewers' }))
}

describe('the reviewers list', () => {
  it('only shows while the user can’t change it', () => {
    renderAt(<ReviewSummary review={editable({ canEditReviewers: false })} item={undefined} reviewers={editing()} />)

    expect(screen.queryByRole('button', { name: 'Edit reviewers' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Remove the review request/ })).not.toBeInTheDocument()

    renderAt(<ReviewSummary review={review({ canEditReviewers: true })} item={undefined} />)
    expect(screen.queryByRole('button', { name: 'Edit reviewers' }), 'nothing to edit with').not.toBeInTheDocument()
  })

  it('takes a request off with ×, and asks someone who reviewed again with ↻', async () => {
    const edit = vi.fn().mockResolvedValue('')
    renderAt(<ReviewSummary review={editable()} item={undefined} reviewers={editing(edit)} />)

    await userEvent.click(screen.getByRole('button', { name: 'Remove the review request for octo-asked' }))
    expect(edit).toHaveBeenLastCalledWith([], ['octo-asked'])

    await userEvent.click(screen.getByRole('button', { name: 'Remove the review request for acme-org/ops' }))
    expect(edit).toHaveBeenLastCalledWith([], ['team:ops'])

    await userEvent.click(screen.getByRole('button', { name: 'Ask octo-reviewer to review again' }))
    expect(edit).toHaveBeenLastCalledWith(['octo-reviewer'], [])

    expect(
      screen.queryByRole('button', { name: 'Remove the review request for octo-user' }),
      'the user takes themselves off with Remove me as reviewer',
    ).not.toBeInTheDocument()
  })

  it('never offers to ask the author, a bot, or the user again', () => {
    renderAt(
      <ReviewSummary
        review={editable({
          reviews: [
            entry('octo-other', { state: 'COMMENTED' }),
            entry('helper', { state: 'COMMENTED', bot: true }),
            entry('octo-user', { state: 'APPROVED', mine: true }),
          ],
        })}
        item={undefined}
        reviewers={editing()}
      />,
    )

    expect(screen.queryByRole('button', { name: /review again/ })).not.toBeInTheDocument()
  })

  it('fades a line while its edit runs, and says why one failed', async () => {
    const finish = deferred<string>()
    const edit = vi.fn(() => finish.promise)
    renderAt(<ReviewSummary review={editable()} item={undefined} reviewers={editing(edit)} />)

    await userEvent.click(screen.getByRole('button', { name: 'Remove the review request for octo-asked' }))

    const row = screen.getByText('octo-asked').closest('li')
    expect(row).toHaveAttribute('aria-busy', 'true')
    expect(
      screen.getByRole('button', { name: 'Ask octo-reviewer to review again' }),
      'one edit at a time',
    ).toBeDisabled()

    finish.resolve('GitHub still asks octo-asked to review acme-org/example-repo#7.')

    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub still asks octo-asked')
    expect(row).not.toHaveAttribute('aria-busy')
  })

  it('offers its first suggestions as one-click requests while no one is asked', async () => {
    const edit = vi.fn().mockResolvedValue('')
    renderAt(<ReviewSummary review={review({ canEditReviewers: true })} item={undefined} reviewers={editing(edit)} />)

    const reviewers = screen.getByRole('region', { name: 'Reviewers' })
    expect(reviewers).toHaveTextContent('No one is asked to review yet.')

    const chips = within(screen.getByRole('group', { name: 'Suggested reviewers' }))
    expect(
      chips.getAllByRole('button', { name: /^Ask .* to review$/ }).map((b) => b.getAttribute('aria-label')),
    ).toEqual(['Ask octo-suggested to review', 'Ask octo-member to review', 'Ask octo-helper to review'])
    expect(chips.getByRole('button', { name: 'Ask octo-suggested to review' })).toHaveAttribute(
      'title',
      'Octo Suggested · Changed these files',
    )

    await userEvent.click(chips.getByRole('button', { name: 'Ask octo-member to review' }))
    expect(edit).toHaveBeenCalledExactlyOnceWith(['octo-member'], [])

    await userEvent.click(chips.getByRole('button', { name: 'More…' }))
    expect(await screen.findByRole('dialog', { name: 'Reviewers' })).toBeInTheDocument()
  })
})

describe('ReviewerPicker', () => {
  it('lists who is asked, whom GitHub suggests, who works on the repository, and its teams', async () => {
    renderAt(<ReviewSummary review={editable()} item={undefined} reviewers={editing()} />)

    const panel = await picker()
    const section = (name: string) => within(panel.getByRole('group', { name }))

    expect(section('Requested').getAllByRole('checkbox')).toHaveLength(3)
    expect(section('Requested').getByRole('checkbox', { name: /octo-asked/ })).toBeChecked()
    expect(section('Requested').getByRole('checkbox', { name: /acme-org\/ops/ })).toBeChecked()
    expect(section('Requested').getByRole('checkbox', { name: /octo-user/ }), 'the user’s own is theirs').toBeDisabled()
    expect(
      section('Suggested for this pull request').getByRole('checkbox', { name: /octo-suggested/ }),
    ).toHaveAccessibleName('octo-suggested Octo Suggested Changed these files')
    expect(
      section('Works on acme-org/example-repo').getByRole('checkbox', { name: /octo-member/ }),
    ).toHaveAccessibleName('octo-member Octo Member Reviewed 12 here · 3d ago')
    const teams = section('Teams')
    expect(teams.getAllByRole('checkbox')).toEqual([
      teams.getByRole('checkbox', { name: 'acme-org/web Web' }),
      teams.getByRole('checkbox', { name: 'acme-org/api API' }),
    ])
    expect(panel.getByText('Contributors to acme-org/example-repo · from 50 pull requests, 2d ago')).toBeInTheDocument()
    expect(platform.learnReviewers, 'nothing to learn: the read isn’t due').not.toHaveBeenCalled()
  })

  it('filters on this Mac as the user types, and sends people and teams apart', async () => {
    const edit = vi.fn().mockResolvedValue('')
    renderAt(<ReviewSummary review={editable()} item={undefined} reviewers={editing(edit)} />)

    const panel = await picker()
    const field = panel.getByRole('searchbox', { name: 'Filter people and teams' })

    await userEvent.type(field, 'mem')
    expect(panel.getAllByRole('checkbox')).toEqual([
      panel.getByRole('checkbox', { name: 'octo-member Octo Member Reviewed 12 here · 3d ago' }),
    ])
    await userEvent.click(panel.getByRole('checkbox', { name: /octo-member/ }))

    await userEvent.clear(field)
    await userEvent.type(field, 'web')
    await userEvent.click(panel.getByRole('checkbox', { name: /acme-org\/web/ }))

    await userEvent.clear(field)
    await userEvent.click(panel.getByRole('checkbox', { name: /acme-org\/ops/ }))

    const apply = panel.getByRole('button', { name: 'Request 2 · Remove 1' })
    await userEvent.click(apply)

    expect(edit).toHaveBeenCalledExactlyOnceWith(['octo-member', 'team:web'], ['team:ops'])
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Reviewers' })).not.toBeInTheDocument()
    })
  })

  it('moves with ↑ and ↓, checks with Enter, and applies with ⌘↩', async () => {
    const edit = vi.fn().mockResolvedValue('')
    renderAt(<ReviewSummary review={editable()} item={undefined} reviewers={editing(edit)} />)

    const panel = await picker()
    const field = panel.getByRole('searchbox', { name: 'Filter people and teams' })
    expect(field).toHaveFocus()

    await userEvent.type(field, 'octo-s')
    expect(field.getAttribute('aria-activedescendant'), 'a filter puts the keyboard on the first match').toBe(
      panel.getByRole('checkbox', { name: /octo-suggested/ }).closest('label')?.id,
    )

    await userEvent.keyboard('{Enter}')
    expect(panel.getByRole('checkbox', { name: /octo-suggested/ })).toBeChecked()

    await userEvent.clear(field)
    expect(field, 'no filter, no row').not.toHaveAttribute('aria-activedescendant')

    await userEvent.keyboard('{ArrowDown}{ArrowDown}{ArrowUp}{ArrowDown}{ArrowDown}{Enter}')
    expect(panel.getByRole('checkbox', { name: /acme-org\/ops/ }), 'the third row, taken off').not.toBeChecked()

    await userEvent.keyboard('{ArrowUp}{Enter}')
    expect(panel.getByRole('checkbox', { name: /octo-user/ }), 'a locked row stays').toBeChecked()

    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(edit).toHaveBeenCalledExactlyOnceWith(['octo-suggested'], ['team:ops'])
  })

  it('learns who works on the repository as it opens, when that is due, keeping what the user checked', async () => {
    const learned = deferred<Reviewers>()
    platform.learnReviewers.mockReturnValue(learned.promise)
    const edit = editing(
      vi.fn().mockResolvedValue(''),
      reviewerCandidates({ stale: true, people: [], teams: [], pulls: 0, readAt: '' }),
    )
    renderAt(<ReviewSummary review={editable()} item={undefined} reviewers={edit} />)

    const panel = await picker()
    expect(panel.getByRole('status')).toHaveTextContent('Learning who works on acme-org/example-repo…')
    expect(platform.learnReviewers).toHaveBeenCalledExactlyOnceWith(1)
    expect(
      panel.getByText('Contributors to acme-org/example-repo · from the pull requests you’ve viewed'),
    ).toBeInTheDocument()

    await userEvent.click(panel.getByRole('checkbox', { name: /octo-asked/ }))

    learned.resolve(reviewerCandidates())

    expect(await panel.findByRole('checkbox', { name: /octo-member/ })).not.toBeChecked()
    expect(panel.queryByRole('status')).not.toBeInTheDocument()
    expect(panel.getByRole('checkbox', { name: /octo-asked/ }), 'still unchecked').not.toBeChecked()
    expect(edit.onLearned).toHaveBeenCalledExactlyOnceWith(reviewerCandidates())
  })

  it('says quietly why it couldn’t learn more, and keeps what it has', async () => {
    platform.learnReviewers.mockRejectedValue(
      new Error('Couldn’t learn who works on acme-org/example-repo: GitHub is down.'),
    )
    renderAt(
      <ReviewSummary
        review={editable()}
        item={undefined}
        reviewers={editing(undefined, reviewerCandidates({ stale: true }))}
      />,
    )

    const panel = await picker()

    expect(
      await panel.findByText('Couldn’t learn who works on acme-org/example-repo: GitHub is down.'),
    ).toBeInTheDocument()
    expect(panel.getByRole('checkbox', { name: /octo-member/ })).toBeInTheDocument()
  })

  it('says when no one matches, or the app knows no one yet', async () => {
    renderAt(
      <ReviewSummary
        review={editable({ reviews: [] })}
        item={undefined}
        reviewers={editing(undefined, reviewerCandidates({ people: [], teams: [] }))}
      />,
    )

    const panel = await picker()
    expect(panel.getByText('The app hasn’t seen anyone work on acme-org/example-repo yet.')).toBeInTheDocument()

    await userEvent.type(panel.getByRole('searchbox', { name: 'Filter people and teams' }), 'zz')
    expect(panel.getByText('No one matches.')).toBeInTheDocument()
  })
})
