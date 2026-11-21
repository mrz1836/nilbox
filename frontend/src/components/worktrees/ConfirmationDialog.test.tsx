import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { useWorktreeDetails } from '../../lib/useWorktreeDetails'
import type { Confirmation } from '../../lib/useWorktrees'
import type { Worktree, WorktreeDetails, WorktreeRepo } from '../../platform'
import { deferred } from '../../test/async'
import { FIXED_NOW, renderAt } from '../../test/clock'
import {
  copiedWorktree,
  copyComparison,
  finishedWorktree,
  linkedItem,
  ownBranch,
  partlyCopiedWorktree,
  recordedWorktree,
  worktree,
  worktreeCommit,
  worktreeDetails,
  worktreeFiles,
  worktreeRepo,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ConfirmationDialog } from './ConfirmationDialog'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const repoPath = '/home/user/projects/example-repo'
const linkedPath = '/home/user/projects/example-repo/.claude/worktrees/pr-7'

const rules = { own: true, pullRequest: true, merged: true, protected: ['main', 'master'] }

interface HarnessProps {
  confirmation: Confirmation
  repos?: readonly WorktreeRepo[] | undefined
  // listing numbers the listing the page shows, 1 unless said.
  listing?: number
}

// Harness is the dialog as the page shows it, with the page's details for its listing.
function Harness({ confirmation, repos = [worktreeRepo()], listing = 1 }: HarnessProps) {
  const details = useWorktreeDetails(listing)

  return (
    <ConfirmationDialog
      confirmation={confirmation}
      rules={rules}
      now={FIXED_NOW}
      repos={repos}
      details={details}
      sessions={new Map()}
      agentName={(id) => id}
      onConfirm={vi.fn()}
      onCancel={vi.fn()}
    />
  )
}

// single draws the confirmation of kind for wt, a worktree of example-repo.
function single(kind: 'remove' | 'force' | 'cleanUp', wt: Worktree, repos?: readonly WorktreeRepo[]) {
  renderAt(<Harness confirmation={{ kind, repoPath, worktree: wt }} repos={repos} />)

  return within(screen.getByRole('dialog'))
}

// dirty is a worktree with n uncommitted files, the listing's first 12 of them listed, and
// unpushed commits not on GitHub, which the backend lets Force remove take.
function dirty(n: number, unpushed = 0, overrides: Partial<Worktree> = {}): Worktree {
  return worktree({
    dirty: true,
    dirtyCount: n,
    files: worktreeFiles(Math.min(n, 12)),
    unpushed,
    onGitHub: unpushed === 0,
    canRemove: false,
    canForceRemove: true,
    branches: [ownBranch({ deletable: unpushed === 0, reason: unpushed === 0 ? '' : 'it has commits not on GitHub' })],
    ...overrides,
  })
}

// paths are the paths a list of files in the dialog shows, in order, each without its
// status letter.
function paths(list: HTMLElement): string[] {
  return within(list)
    .getAllByRole('listitem')
    .map((li) => li.textContent.replace(/^[A-Z]/, ''))
}

describe('ConfirmationDialog for one worktree', () => {
  it('says on top what removing it would cost, then what it does to the folder, and its branches', () => {
    const dialog = single(
      'remove',
      worktree({ item: linkedItem({ kind: 'issue', number: 9, state: 'open', headOid: '' }) }),
    )

    const band = dialog.getByText(/^Safe to remove/).closest('[data-tone]')
    expect(band).toHaveAttribute('data-tone', 'success')
    expect(band).toHaveTextContent('Safe to remove: no uncommitted files, and every commit is on GitHub.')
    expect(screen.getByRole('dialog', { name: 'Remove this worktree?' })).toHaveTextContent(
      'This deletes the folder ~/projects/example-repo/.claude/worktrees/pr-7, which has no uncommitted changes.',
    )
    expect(dialog.getByText('Made for issue #9, open.')).toBeInTheDocument()
    expect(dialog.getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })).toBeChecked()
    expect(dialog.queryByRole('region', { name: 'Files it discards' })).not.toBeInTheDocument()
    // Only Force remove reads the worktree's details.
    expect(platform.worktreeDetails).not.toHaveBeenCalled()
  })

  it('says a finished worktree’s clean up loses nothing', () => {
    const dialog = single('cleanUp', finishedWorktree())

    expect(dialog.getByText(/^Safe to remove/).closest('[data-tone]')).toHaveAttribute('data-tone', 'success')
    expect(screen.getByRole('dialog', { name: 'Clean up this worktree?' })).toHaveTextContent(
      'whose work is finished: it has no uncommitted changes, and every commit on it is on GitHub.',
    )
    expect(dialog.getByText('Made for pull request #7, merged.')).toBeInTheDocument()
  })

  it('says a clean up of one whose commits went into a merged pull request as copies loses nothing', () => {
    const dialog = single('cleanUp', recordedWorktree())

    expect(dialog.getByText(/^Safe to remove/).closest('[data-tone]')).toHaveTextContent(
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, merged 2d ago.',
    )
    expect(screen.getByRole('dialog', { name: 'Clean up this worktree?' })).toHaveTextContent(
      'whose work is finished: it has no uncommitted changes, and the change of every commit on it is on GitHub.',
    )
    expect(dialog.getByText('Its commits were copied onto pull request #42, merged.')).toBeInTheDocument()
    expect(dialog.getByRole('checkbox', { name: /^Delete branch worktree-agent-a1b2c3/ })).toBeChecked()
  })

  it('says a clean up holds back a worktree whose copies are gone since, as its details read, in both its words', () => {
    const gone = worktreeDetails({
      unpushed: 10,
      commits: [worktreeCommit({ oid: copiedWorktree().head })],
      copies: copyComparison({ ref: '', branch: '', identical: 0 }),
    })
    renderAt(
      <ConfirmationDialog
        confirmation={{ kind: 'cleanUp', repoPath, worktree: copiedWorktree({ stage: 'finished', canCleanUp: true }) }}
        rules={rules}
        now={FIXED_NOW}
        repos={[worktreeRepo()]}
        details={{ of: () => ({ state: 'read', details: gone }), read: vi.fn() }}
        sessions={new Map()}
        agentName={(id) => id}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    const dialog = screen.getByRole('dialog', { name: 'Clean up this worktree?' })
    expect(
      within(dialog)
        .getByText(/^Its folder is clean, but 10 commits/)
        .closest('[data-tone]'),
    ).toHaveAttribute('data-tone', 'warning')
    expect(dialog).toHaveTextContent(
      'Clean up checks it again first, and deletes the folder ~/projects/example-repo/.claude/worktrees/agent-a1b2c3 only if its work is still finished, which, as last read, it isn’t.',
    )
    expect(dialog).not.toHaveTextContent(/whose work is finished/)
  })

  it.each([
    ['uncommitted files since', finishedWorktree(), worktreeDetails({ files: worktreeFiles(2), fileCount: 2 })],
    ['commits not on GitHub since', finishedWorktree(), worktreeDetails({ unpushed: 1, commits: [worktreeCommit()] })],
    [
      'commits it couldn’t count, as listed and as read',
      finishedWorktree({ unpushed: -1, onGitHub: false }),
      worktreeDetails({ unpushed: -1 }),
    ],
  ])('says a clean up holds back a worktree with %s, as its details read', (_, wt, read) => {
    renderAt(
      <ConfirmationDialog
        confirmation={{ kind: 'cleanUp', repoPath, worktree: wt }}
        rules={rules}
        now={FIXED_NOW}
        repos={[worktreeRepo()]}
        details={{ of: () => ({ state: 'read', details: read }), read: vi.fn() }}
        sessions={new Map()}
        agentName={(id) => id}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    expect(screen.getByRole('dialog', { name: 'Clean up this worktree?' })).toHaveTextContent(
      /Clean up checks it again first, and deletes the folder .+ only if its work is still finished, which, as last read, it isn’t\./,
    )
  })

  it.each([
    ['by its branch', 'branch', 'On the branch of pull request #7, open.'],
    ['by its commits’ copies', 'copy', 'Its commits were copied onto pull request #7, open.'],
  ] as const)('says how a worktree linked %s is linked to its pull request', (_, linkedBy, words) => {
    const dialog = single('remove', worktree({ item: linkedItem({ state: 'open', headOid: '' }), linkedBy }))

    expect(dialog.getByText(words)).toBeInTheDocument()
  })

  it('holds the branch of one whose commits are copied onto an open pull request’s, saying why', () => {
    const dialog = single('remove', copiedWorktree())

    expect(dialog.getByText(/^Safe to remove/).closest('[data-tone]')).toHaveTextContent(
      'Safe to remove: no uncommitted files, and every commit’s change is on GitHub, copied onto PR #42’s branch.Removing it keeps its branch, worktree-agent-a1b2c3, with the original commits.',
    )

    const branch = dialog.getByRole('checkbox', { name: /^Delete branch worktree-agent-a1b2c3/ })
    expect(branch).toBeDisabled()
    expect(branch).not.toBeChecked()
    expect(
      dialog.getByText(
        "Stays: its commits are on GitHub only as copies, in pull request #42, which hasn't merged with them.",
      ),
    ).toBeInTheDocument()
  })

  it('words the cost without its clone when the clone isn’t listed', () => {
    const dialog = single('remove', worktree(), [])

    expect(dialog.getByText(/^Safe to remove/)).toBeInTheDocument()
  })

  it('words each cost as the worktree’s details counted it, once its drawer read them', () => {
    const counted = worktreeDetails({ files: worktreeFiles(3), fileCount: 3 })
    renderAt(
      <ConfirmationDialog
        confirmation={{
          kind: 'all',
          targets: [{ repoPath, worktree: worktree() }],
        }}
        rules={rules}
        now={FIXED_NOW}
        repos={[worktreeRepo()]}
        details={{ of: () => ({ state: 'read', details: counted }), read: vi.fn() }}
        sessions={new Map()}
        agentName={(id) => id}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    expect(screen.getByText(/^Removing it would discard 3 uncommitted files/).closest('[data-tone]')).toHaveAttribute(
      'data-tone',
      'warning',
    )
  })
})

describe('ConfirmationDialog for Force remove', () => {
  it('lists the files it discards and the commits it keeps, reading the rest as it opens', async () => {
    const answer = deferred<WorktreeDetails>()
    platform.worktreeDetails.mockReturnValue(answer.promise)
    const dialog = single('force', dirty(30, 2))

    expect(platform.worktreeDetails).toHaveBeenCalledExactlyOnceWith(repoPath, linkedPath)
    expect(dialog.getByText(/^Removing it would discard 30 uncommitted files/).closest('[data-tone]')).toHaveAttribute(
      'data-tone',
      'warning',
    )

    const files = dialog.getByRole('region', { name: 'Files it discards' })
    expect(within(files).getByRole('heading')).toHaveTextContent('Discards 30 files')
    expect(within(files).getAllByRole('listitem')).toHaveLength(12)
    expect(within(files).getByRole('status')).toHaveTextContent('reading the other 18…')

    const commits = dialog.getByRole('region', { name: 'Commits not on GitHub' })
    expect(within(commits).getByRole('heading')).toHaveTextContent('Keeps 2 not on GitHub, on worktree-pr-7')
    expect(within(commits).getByRole('status')).toHaveTextContent('reading them…')
    expect(within(commits).queryByRole('list')).not.toBeInTheDocument()

    await act(async () => {
      answer.resolve(
        worktreeDetails({
          files: worktreeFiles(30),
          fileCount: 30,
          unpushed: 2,
          commits: [worktreeCommit(), worktreeCommit({ oid: '1'.repeat(40), subject: 'Warm it on start' })],
        }),
      )
      await Promise.resolve()
    })

    expect(within(files).getAllByRole('listitem')).toHaveLength(30)
    expect(within(files).queryByRole('status')).not.toBeInTheDocument()
    expect(paths(within(files).getByRole('list')).at(-1)).toBe('src/file-030.ts')
    expect(
      within(commits)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['0b1c397Keep the widget cache warm2h', '1111111Warm it on start2h'])
    expect(within(commits).queryByRole('status')).not.toBeInTheDocument()
  })

  it('reads the details again after a listing that lands while it’s open', async () => {
    platform.worktreeDetails.mockResolvedValue(worktreeDetails({ files: worktreeFiles(20), fileCount: 20 }))
    const confirmation: Confirmation = { kind: 'force', repoPath, worktree: dirty(20) }
    const { rerender } = renderAt(<Harness confirmation={confirmation} />)

    const files = screen.getByRole('region', { name: 'Files it discards' })
    await vi.waitFor(() => {
      expect(within(files).getAllByRole('listitem')).toHaveLength(20)
    })

    rerender(<Harness confirmation={confirmation} listing={2} />)

    await vi.waitFor(() => {
      expect(platform.worktreeDetails).toHaveBeenCalledTimes(2)
    })
    await vi.waitFor(() => {
      expect(within(files).getAllByRole('listitem')).toHaveLength(20)
    })
    expect(within(files).queryByRole('status')).not.toBeInTheDocument()
  })

  it('says where the files past the 200 it lists are', async () => {
    platform.worktreeDetails.mockResolvedValue(worktreeDetails({ files: worktreeFiles(200), fileCount: 250 }))
    const dialog = single('force', dirty(250))

    const files = dialog.getByRole('region', { name: 'Files it discards' })
    expect(
      await within(files).findByText(/^Only the first 200 are listed here; 50 more are in Terminal/),
    ).toHaveTextContent('Only the first 200 are listed here; 50 more are in Terminal (git status)')
    expect(within(files).getAllByRole('listitem')).toHaveLength(200)
    expect(dialog.queryByRole('region', { name: 'Commits not on GitHub' })).not.toBeInTheDocument()
  })

  it('says where the commits past the 200 it lists are', async () => {
    const commits = Array.from({ length: 200 }, (_, i) => worktreeCommit({ oid: i.toString(16).padStart(40, '0') }))
    platform.worktreeDetails.mockResolvedValue(
      worktreeDetails({ files: worktreeFiles(1), fileCount: 1, unpushed: 350, commits }),
    )
    const dialog = single('force', dirty(1, 350))

    const listed = dialog.getByRole('region', { name: 'Commits not on GitHub' })
    expect(
      await within(listed).findByText(/^Only the first 200 are listed here; 150 more are in Terminal/),
    ).toHaveTextContent('Only the first 200 are listed here; 150 more are in Terminal (git log)')
    expect(within(listed).getByRole('heading')).toHaveTextContent('Keeps 350 not on GitHub')
    expect(within(listed).getAllByRole('listitem')).toHaveLength(200)
  })

  it('keeps nothing as not on GitHub for one at its pull request’s head, whatever no remote has', async () => {
    const atHead = dirty(14, 3, { onGitHub: true, branches: [ownBranch()] })
    platform.worktreeDetails.mockResolvedValue(
      worktreeDetails({
        files: worktreeFiles(14),
        fileCount: 14,
        unpushed: 3,
        commits: [worktreeCommit({ oid: atHead.head })],
      }),
    )
    const dialog = single('force', atHead)

    // The rest of the files read means the details landed.
    await vi.waitFor(() => {
      expect(
        within(dialog.getByRole('region', { name: 'Files it discards' })).queryByRole('status'),
      ).not.toBeInTheDocument()
    })
    expect(dialog.queryByRole('region', { name: 'Commits not on GitHub' })).not.toBeInTheDocument()
    expect(dialog.queryByText(/not on GitHub/)).not.toBeInTheDocument()
  })

  it('keeps nothing as not on GitHub for one whose commits are on GitHub as copies', async () => {
    const copied = copiedWorktree({
      dirty: true,
      dirtyCount: 14,
      files: worktreeFiles(12),
      canRemove: false,
      canForceRemove: true,
    })
    platform.worktreeDetails.mockResolvedValue(
      worktreeDetails({
        files: worktreeFiles(14),
        fileCount: 14,
        unpushed: 10,
        commits: [worktreeCommit({ oid: copied.head, copy: 'identical', copyOid: '3'.repeat(40) })],
        copies: copyComparison(),
      }),
    )
    const dialog = single('force', copied)

    await vi.waitFor(() => {
      expect(
        within(dialog.getByRole('region', { name: 'Files it discards' })).queryByRole('status'),
      ).not.toBeInTheDocument()
    })
    expect(dialog.getByText(/^Removing it would discard 14 uncommitted files/)).toBeInTheDocument()
    expect(dialog.queryByRole('region', { name: 'Commits not on GitHub' })).not.toBeInTheDocument()
  })

  it('marks a commit it keeps that has only an edited copy', async () => {
    const partly = partlyCopiedWorktree({
      dirty: true,
      dirtyCount: 2,
      files: worktreeFiles(2),
      canRemove: false,
      canForceRemove: true,
    })
    platform.worktreeDetails.mockResolvedValue(
      worktreeDetails({
        files: worktreeFiles(2),
        fileCount: 2,
        unpushed: 1,
        commits: [worktreeCommit({ oid: partly.head, copy: 'edited', copyOid: '5'.repeat(40) })],
        copies: copyComparison({ identical: 0, edited: 1 }),
      }),
    )
    const dialog = single('force', partly)

    const commits = dialog.getByRole('region', { name: 'Commits not on GitHub' })
    expect(within(commits).getByRole('heading')).toHaveTextContent('Keeps 1 not on GitHub, on worktree-agent-d4e5f6')
    expect(await within(commits).findByText(/^≈ edited copy/)).toHaveTextContent('≈ edited copy 5555555')
    expect(
      dialog.getByText(/^Its 1 commit not on GitHub stays in the clone on/).closest('[data-tone]'),
    ).toHaveTextContent('1 has only an edited copy on origin/feature-x.')
  })

  it('lists a path git lists twice once for each status', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const files = [
      { code: 'D ', path: 'notes.txt' },
      { code: '??', path: 'notes.txt' },
    ]
    platform.worktreeDetails.mockResolvedValue(worktreeDetails({ files, fileCount: 2 }))
    const dialog = single('force', dirty(2, 0, { files }))

    const listed = dialog.getByRole('region', { name: 'Files it discards' })
    await vi.waitFor(() => {
      expect(platform.worktreeDetails).toHaveBeenCalledOnce()
    })
    expect(paths(within(listed).getByRole('list'))).toEqual(['notes.txt', 'notes.txt'])
    expect(errors.mock.calls.flat().join(' ')).not.toMatch(/same key/)
    errors.mockRestore()
  })

  it('reads the details again as it opens after a read that failed', () => {
    const read = vi.fn()
    const wt = dirty(3)
    renderAt(
      <ConfirmationDialog
        confirmation={{ kind: 'force', repoPath, worktree: wt }}
        rules={rules}
        now={FIXED_NOW}
        repos={[worktreeRepo()]}
        details={{ of: () => ({ state: 'failed', error: 'Couldn’t read the worktree.' }), read }}
        sessions={new Map()}
        agentName={(id) => id}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    expect(read).toHaveBeenCalledExactlyOnceWith(repoPath, wt)
  })

  it('keeps the listing’s files and count when the details can’t be read', async () => {
    platform.worktreeDetails.mockRejectedValue(new Error('git status: exit status 128'))
    const dialog = single('force', dirty(30, 1))

    const files = dialog.getByRole('region', { name: 'Files it discards' })
    expect(
      await within(files).findByText(/^Only the first 12 are listed here; 18 more are in Terminal/),
    ).toBeInTheDocument()
    expect(within(files).getByRole('heading')).toHaveTextContent('Discards 30 files')

    const commits = dialog.getByRole('region', { name: 'Commits not on GitHub' })
    expect(within(commits).getByRole('heading')).toHaveTextContent('Keeps 1 not on GitHub, on worktree-pr-7')
    expect(within(commits).queryByRole('status')).not.toBeInTheDocument()
  })

  it('counts the commits as the listing did when the details couldn’t count them', async () => {
    platform.worktreeDetails.mockResolvedValue(worktreeDetails({ files: worktreeFiles(1), fileCount: 1, unpushed: -1 }))
    const dialog = single('force', dirty(1, 3))

    const files = dialog.getByRole('region', { name: 'Files it discards' })
    expect(within(files).getByRole('heading')).toHaveTextContent('Discards 1 file')

    const commits = dialog.getByRole('region', { name: 'Commits not on GitHub' })
    await vi.waitFor(() => {
      expect(within(commits).queryByRole('status')).not.toBeInTheDocument()
    })
    expect(within(commits).getByRole('heading')).toHaveTextContent('Keeps 3 not on GitHub')
  })

  it('says a detached worktree’s commits go with it', async () => {
    platform.worktreeDetails.mockResolvedValue(
      worktreeDetails({ files: worktreeFiles(2), fileCount: 2, unpushed: 1, commits: [worktreeCommit()] }),
    )
    const dialog = single('force', dirty(2, 1, { branch: '', detached: true, branches: [] }))

    const commits = dialog.getByRole('region', { name: 'Commits not on GitHub' })
    expect(within(commits).getByRole('heading')).toHaveTextContent('Loses 1 not on GitHub, which no branch holds')
    expect(await within(commits).findByText('Keep the widget cache warm')).toBeInTheDocument()
    expect(dialog.getByText(/^1 commit that no branch holds would be lost with it\./)).toBeInTheDocument()
  })

  it('confirms with the branches ticked', async () => {
    const onConfirm = vi.fn()
    renderAt(
      <ConfirmationDialog
        confirmation={{ kind: 'force', repoPath, worktree: dirty(3) }}
        rules={rules}
        now={FIXED_NOW}
        repos={[worktreeRepo()]}
        details={{ of: () => undefined, read: vi.fn() }}
        sessions={new Map()}
        agentName={(id) => id}
        onConfirm={onConfirm}
        onCancel={vi.fn()}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Force remove' }))

    expect(onConfirm).toHaveBeenCalledWith({ branches: ['worktree-pr-7'] })
  })
})

describe('ConfirmationDialog in bulk', () => {
  it('says in a line under each worktree what removing it would cost', () => {
    const issue9 = worktree({
      path: `${repoPath}/.claude/worktrees/issue-9`,
      displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
      branch: 'worktree-issue-9',
      unpushed: 1,
      onGitHub: false,
      branches: [ownBranch({ name: 'worktree-issue-9', deletable: false, reason: 'it has commits not on GitHub' })],
    })
    renderAt(
      <Harness
        confirmation={{
          kind: 'all',
          targets: [
            { repoPath, worktree: worktree() },
            { repoPath, worktree: issue9 },
          ],
        }}
      />,
    )

    const listed = within(screen.getByRole('list', { name: 'Worktrees' }))
    const [clean, ahead] = listed
      .getAllByRole('listitem')
      .filter((li) => li.parentElement?.getAttribute('aria-label') === 'Worktrees')
    expect(
      within(clean as HTMLElement)
        .getByText(/^Safe to remove/)
        .closest('[data-tone]'),
    ).toHaveAttribute('data-tone', 'success')
    const line = within(ahead as HTMLElement)
      .getByText(/^Its folder is clean, but 1 commit on/)
      .closest('[data-tone]')
    expect(line).toHaveAttribute('data-tone', 'warning')
    expect(line).toHaveTextContent('Its folder is clean, but 1 commit on worktree-issue-9 isn’t on GitHub.')
    // The line sits apart from the worktree's box, whose name it leaves alone.
    expect(listed.getByRole('checkbox', { name: '~/projects/example-repo/.claude/worktrees/issue-9' })).toBeChecked()
    expect(platform.worktreeDetails).not.toHaveBeenCalled()
  })

  it('says under a finished worktree whose commits went in as copies where they went', () => {
    renderAt(<Harness confirmation={{ kind: 'finished', targets: [{ repoPath, worktree: recordedWorktree() }] }} />)

    const line = screen.getByText(/^Safe to remove/).closest('[data-tone]')
    expect(line).toHaveAttribute('data-tone', 'success')
    expect(line).toHaveTextContent(
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, merged 2d ago.',
    )
    expect(screen.getByRole('dialog', { name: 'Clean up finished worktrees?' })).toHaveTextContent(
      'each has no uncommitted changes, and every commit on it, or its change, is on GitHub.',
    )
    expect(screen.getByText('Its commits were copied onto pull request #42, merged')).toBeInTheDocument()
  })
})
