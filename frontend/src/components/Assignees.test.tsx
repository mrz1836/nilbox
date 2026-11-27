import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Assigned } from '../lib/assignees'
import { deferred } from '../test/async'
import { renderAt } from '../test/clock'
import { assigneeCandidates } from '../test/fixtures'
import { platform } from '../test/platform'
import { AssigneesSection, type AssigneesEditing } from './Assignees'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// editing is what the section edits the assignees with, its edit answering as told, and its
// reads the review sheet's.
function editing(edit = vi.fn().mockResolvedValue('')): AssigneesEditing {
  return {
    reads: {
      candidates: platform.reviewAssigneeCandidates,
      learn: platform.learnReviewAssignees,
      search: platform.searchReviewAssignees,
    },
    ownLock: 'Take yourself off with Unassign me, in the card’s menu.',
    onEdit: edit,
  }
}

// section renders the summary's assignees of pull request #1, assigned to assigned.
function section(assigned: Assigned[], edits?: AssigneesEditing, more = 0) {
  return renderAt(
    <AssigneesSection
      look="summary"
      itemId={1}
      repo="acme-org/example-repo"
      noun="pull request"
      url="https://github.com/acme-org/example-repo/pull/7"
      assignees={assigned}
      more={more}
      editing={edits}
    />,
  )
}

const assigned: Assigned[] = [
  { login: 'octo-user', mine: true },
  { login: 'octo-other', mine: false },
]

describe('AssigneesSection', () => {
  it('lists who it is assigned to, and only shows while the user can’t change them', async () => {
    section(assigned, undefined, 2)

    const region = screen.getByRole('region', { name: 'Assignees' })
    expect(region).toHaveTextContent('octo-user (you)')
    expect(within(region).queryByRole('button', { name: 'Edit assignees' })).not.toBeInTheDocument()
    expect(within(region).queryByRole('button', { name: /Unassign/ })).not.toBeInTheDocument()

    await userEvent.click(within(region).getByRole('button', { name: 'and 2 more on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
    expect(platform.reviewAssigneeCandidates, 'nothing to suggest from').not.toHaveBeenCalled()
  })

  it('shows nothing in the summary for a pull request no one is assigned to that the user can’t change', () => {
    section([])

    expect(screen.queryByRole('region', { name: 'Assignees' })).not.toBeInTheDocument()
  })

  it('unassigns anyone but the user with ×, a line fading while it runs', async () => {
    const unassigning = deferred<string>()
    const edit = vi.fn(() => unassigning.promise)
    section(assigned, editing(edit))

    expect(
      screen.queryByRole('button', { name: 'Unassign octo-user' }),
      'Unassign me does that',
    ).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Unassign octo-other' }))
    expect(edit).toHaveBeenCalledExactlyOnceWith([], ['octo-other'])

    const line = screen.getByText('octo-other').closest('li')
    expect(line).toHaveAttribute('aria-busy', 'true')
    expect(line).toHaveClass('opacity-50')

    unassigning.resolve('')
    await waitFor(() => {
      expect(line).not.toHaveAttribute('aria-busy')
    })
  })

  it('takes the user off with × on their own line, as Unassign me, whether or not they may edit', async () => {
    const onUnassignMe = vi.fn()
    const { rerender } = section(assigned)

    expect(screen.queryByRole('button', { name: 'Unassign me' }), 'without Unassign me, none').not.toBeInTheDocument()

    rerender(
      <AssigneesSection
        look="details"
        itemId={1}
        repo="acme-org/example-repo"
        noun="issue"
        url="https://github.com/acme-org/example-repo/issues/12"
        assignees={assigned}
        more={0}
        onUnassignMe={onUnassignMe}
      />,
    )

    const mine = screen.getByRole('button', { name: 'Unassign me' })
    expect(mine.closest('li')).toHaveTextContent('octo-user (you)')
    expect(mine).toHaveAttribute('title', 'Take yourself off this issue on GitHub. Uses one REST request.')
    expect(screen.queryByRole('button', { name: 'Unassign octo-other' }), 'the others only while editing').toBeNull()

    await userEvent.click(mine)
    expect(onUnassignMe).toHaveBeenCalledOnce()
  })

  it('says why an edit failed', async () => {
    section(assigned, editing(vi.fn().mockResolvedValue('GitHub kept octo-other assigned.')))

    await userEvent.click(screen.getByRole('button', { name: 'Unassign octo-other' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub kept octo-other assigned.')
  })

  it('offers, with no one assigned, to assign yourself, the author, or more', async () => {
    const edit = vi.fn().mockResolvedValue('')
    section([], editing(edit))

    // Something missing, in the warning tone, beside an empty place for a picture.
    expect(screen.getByText('No one is assigned.')).toHaveClass('text-warning', 'font-medium')

    const chips = within(await screen.findByRole('group', { name: 'Suggested assignees' }))
    expect(chips.getAllByRole('button').map((b) => b.textContent)).toEqual([
      'OAssign yourself',
      'Oocto-other',
      'Oocto-commenter',
      'More…',
    ])
    expect(chips.getByRole('button', { name: 'Assign octo-other' })).toHaveAttribute(
      'title',
      'Opened this pull request',
    )

    await userEvent.click(chips.getByRole('button', { name: 'Assign yourself' }))
    expect(edit).toHaveBeenCalledExactlyOnceWith(['octo-user'], [])
    await waitFor(() => {
      expect(platform.reviewAssigneeCandidates, 'the suggestions again after the edit').toHaveBeenCalledTimes(2)
    })

    await userEvent.click(chips.getByRole('button', { name: 'More…' }))
    expect(await screen.findByRole('dialog', { name: 'Assignees' })).toBeInTheDocument()
  })

  it('opens the picker with Edit, and closes it with what the edit said', async () => {
    const edit = vi.fn().mockResolvedValue('')
    platform.reviewAssigneeCandidates.mockResolvedValue(assigneeCandidates())
    section(assigned, editing(edit))

    await userEvent.click(screen.getByRole('button', { name: 'Edit assignees' }))

    const picker = within(await screen.findByRole('dialog', { name: 'Assignees' }))
    await userEvent.click(await picker.findByRole('checkbox', { name: /^octo-member/ }))
    await userEvent.click(picker.getByRole('button', { name: 'Assign 1' }))

    expect(edit).toHaveBeenCalledExactlyOnceWith(['octo-member'], [])
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Assignees' })).not.toBeInTheDocument()
    })
  })
})
