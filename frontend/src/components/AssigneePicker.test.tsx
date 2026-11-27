import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Assigned } from '../lib/assignees'
import type { AssigneeCandidates } from '../platform'
import { deferred } from '../test/async'
import { renderAt } from '../test/clock'
import { assigneeCandidates } from '../test/fixtures'
import { platform } from '../test/platform'
import { AssigneePicker, type AssigneeReads } from './AssigneePicker'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// reads are the issue sheet's reads.
function reads(): AssigneeReads {
  return {
    candidates: platform.issueAssigneeCandidates,
    learn: platform.learnIssueAssignees,
    search: platform.searchIssueAssignees,
  }
}

const ownLock = 'Take yourself off with Unassign me, in the action menu.'

// open renders the picker of issue #1, assigned to assigned, offering candidates, and
// returns its dialog and the hooks it was given.
async function open(
  assigned: Assigned[],
  candidates: AssigneeCandidates | undefined = assigneeCandidates(),
  edit = vi.fn().mockResolvedValue(''),
  onUnassignMe?: () => void,
) {
  const onLearned = vi.fn()
  const onClose = vi.fn()

  renderAt(
    <AssigneePicker
      itemId={1}
      repo="acme-org/example-repo"
      noun="issue"
      assigned={assigned}
      candidates={candidates}
      onLearned={onLearned}
      reads={reads()}
      onUnassignMe={onUnassignMe}
      ownLock={ownLock}
      onEdit={edit}
      onClose={onClose}
    />,
  )

  return { dialog: within(await screen.findByRole('dialog', { name: 'Assignees' })), onLearned, onClose, edit }
}

// sections lists the open picker's sections and the people in each, by their rows' words.
function sections(): [string, string[]][] {
  const dialog = screen.getByRole('dialog', { name: 'Assignees' })

  return within(dialog)
    .getAllByRole('group')
    .map((g) => [
      within(g).getByRole('heading').textContent,
      within(g)
        .getAllByRole('checkbox')
        .map((c) => c.closest('label')?.textContent ?? ''),
    ])
}

describe('AssigneePicker', () => {
  it('lists who it is assigned to first, the user’s row locked, then whom the app suggests', async () => {
    const { dialog } = await open([
      { login: 'octo-user', mine: true },
      { login: 'octo-commenter', mine: false },
    ])

    expect(sections()).toEqual([
      ['Assigned', ['Oocto-user (you)', 'Oocto-commenter']],
      ['Suggested', ['Oocto-other Opened this issue']],
      ['Works on acme-org/example-repo', ['Oocto-member Octo Member Reviewed 12 here · 3d ago']],
      ['In acme-org', ['Oocto-org Works on acme-org/web · 1d ago']],
    ])

    const me = dialog.getByRole('checkbox', { name: /^octo-user/ })
    expect(me).toBeChecked()
    expect(me, 'Unassign me does that').toBeDisabled()
    expect(me.closest('label')).toHaveAttribute('title', ownLock)
    expect(dialog.getByText(/People from the issues and pull requests you’ve viewed/)).toBeInTheDocument()
  })

  it('lets the user assign themselves while they aren’t, and names what Apply sends', async () => {
    const { dialog, edit, onClose } = await open([{ login: 'octo-other', mine: false }])

    await userEvent.click(dialog.getByRole('checkbox', { name: /^octo-user/ }))
    await userEvent.click(dialog.getByRole('checkbox', { name: /^octo-other/ }))
    await userEvent.click(dialog.getByRole('button', { name: 'Assign 1 · Unassign 1' }))

    expect(edit).toHaveBeenCalledExactlyOnceWith(['octo-user'], ['octo-other'])
    await waitFor(() => {
      expect(onClose).toHaveBeenCalled()
    })
  })

  it('unassigns the user with Unassign me once the rest of the edit went, never through the edit', async () => {
    const onUnassignMe = vi.fn()
    const mine = [
      { login: 'octo-user', mine: true },
      { login: 'octo-commenter', mine: false },
    ]
    const { dialog, edit, onClose } = await open(mine, assigneeCandidates(), undefined, onUnassignMe)

    const me = dialog.getByRole('checkbox', { name: /^octo-user/ })
    expect(me).toBeEnabled()
    expect(me.closest('label')).not.toHaveAttribute('title')

    await userEvent.click(me)
    await userEvent.click(dialog.getByRole('checkbox', { name: /^octo-commenter/ }))
    await userEvent.click(dialog.getByRole('button', { name: 'Unassign 2' }))

    expect(edit).toHaveBeenCalledExactlyOnceWith([], ['octo-commenter'])
    expect(onUnassignMe).toHaveBeenCalledOnce()
    expect(edit.mock.invocationCallOrder[0]).toBeLessThan(onUnassignMe.mock.invocationCallOrder[0] ?? 0)
    await waitFor(() => {
      expect(onClose).toHaveBeenCalled()
    })
  })

  it('keeps the user assigned when the rest of the edit fails, and says why', async () => {
    const onUnassignMe = vi.fn()
    const { dialog } = await open(
      [
        { login: 'octo-user', mine: true },
        { login: 'octo-commenter', mine: false },
      ],
      assigneeCandidates(),
      vi.fn().mockResolvedValue('GitHub kept octo-commenter assigned.'),
      onUnassignMe,
    )

    await userEvent.click(dialog.getByRole('checkbox', { name: /^octo-user/ }))
    await userEvent.click(dialog.getByRole('checkbox', { name: /^octo-commenter/ }))
    await userEvent.click(dialog.getByRole('button', { name: 'Unassign 2' }))

    expect(await dialog.findByText('GitHub kept octo-commenter assigned.')).toBeInTheDocument()
    expect(onUnassignMe).not.toHaveBeenCalled()
  })

  it('learns who can be assigned as it opens, when a read is due, and only then', async () => {
    const learned = assigneeCandidates({ readAt: '2026-06-14T12:00:00Z', complete: true, total: 5 })
    const learning = deferred<AssigneeCandidates>()
    platform.learnIssueAssignees.mockImplementation(() => learning.promise)

    const { dialog, onLearned } = await open([], assigneeCandidates({ stale: true }))

    expect(await dialog.findByRole('status')).toHaveTextContent(
      'Learning who can be assigned in acme-org/example-repo…',
    )

    learning.resolve(learned)

    expect(
      await dialog.findByText('Everyone who can be assigned in acme-org/example-repo · read 1d ago'),
    ).toBeInTheDocument()
    expect(platform.learnIssueAssignees).toHaveBeenCalledExactlyOnceWith(1)
    expect(onLearned).toHaveBeenCalledWith(learned)
  })

  it('searches GitHub a pause after typing, and lists whom it found', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })

    platform.searchIssueAssignees.mockResolvedValue(
      assigneeCandidates({
        people: [
          {
            login: 'octo-zed',
            name: 'Octo Zed',
            mine: false,
            source: 'found',
            reviewed: 0,
            authored: 0,
            committed: 0,
            assignedAt: '',
            lastAt: '',
            where: '',
          },
        ],
      }),
    )

    const { dialog } = await open([])
    const user = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })

    await user.type(dialog.getByRole('searchbox', { name: 'Filter or search people' }), 'zed')
    expect(platform.searchIssueAssignees, 'not while typing').not.toHaveBeenCalled()
    expect(dialog.getByText('Searching…')).toBeInTheDocument()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })

    expect(platform.searchIssueAssignees).toHaveBeenCalledExactlyOnceWith(1, 'zed')
    expect(await dialog.findByRole('group', { name: 'Found on GitHub' })).toHaveTextContent('octo-zed')
  })

  it('never searches once the app holds everyone who can be assigned', async () => {
    const { dialog } = await open([], assigneeCandidates({ complete: true, readAt: '2026-06-14T12:00:00Z' }))

    await userEvent.type(dialog.getByRole('searchbox', { name: 'Filter or search people' }), 'zzz')

    expect(await dialog.findByText('No one matches.')).toBeInTheDocument()
    expect(platform.searchIssueAssignees).not.toHaveBeenCalled()
  })

  it('says why a search failed', async () => {
    platform.searchIssueAssignees.mockRejectedValue(new Error('Couldn’t search GitHub for "zzz": GitHub is down.'))
    const { dialog } = await open([])

    await userEvent.type(dialog.getByRole('searchbox', { name: 'Filter or search people' }), 'zzz')

    expect(await dialog.findByText('Couldn’t search GitHub for "zzz": GitHub is down.')).toBeInTheDocument()
  })
})
