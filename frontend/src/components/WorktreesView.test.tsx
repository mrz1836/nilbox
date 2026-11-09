import { act, render, screen, within, type BoundFunctions, type queries } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { BranchResult, BranchRules, LinkedItem, MergedBranch, Worktree } from '../platform'
import { fixedNow } from '../test/clock'
import {
  finishedWorktree,
  linkedItem,
  mainWorktree,
  mergedBranch,
  ownBranch,
  removeResult,
  worktree,
  worktreeCleanup,
  worktreeRepo,
  worktreesSnapshot,
} from '../test/fixtures'
import { platform } from '../test/platform'
import { WorktreesView } from './WorktreesView'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const repoPath = '/home/user/projects/example-repo'
const linkedPath = '/home/user/projects/example-repo/.claude/worktrees/pr-7'
const linkedDisplay = '~/projects/example-repo/.claude/worktrees/pr-7'

// row returns the queries scoped to the worktree row showing displayPath.
function row(displayPath: string) {
  const li = screen.getByText(displayPath).closest('li')
  if (li === null) {
    throw new Error(`no row shows ${displayPath}`)
  }

  return within(li)
}

// repo waits for one repository's section and returns the queries scoped to it.
async function repo(name: string) {
  return within(await screen.findByRole('region', { name }))
}

// confirmWith clicks the open dialog's button called name.
async function confirmWith(name: string) {
  await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name }))
}

// rules are the app's default branch rules, with any changes.
function rules(overrides: Partial<BranchRules> = {}): BranchRules {
  return { own: true, pullRequest: true, merged: true, protected: ['main', 'master'], ...overrides }
}

// prBranch is the pull request's own branch feature-x, checked out in the worktree.
const prBranch = ownBranch({ name: 'feature-x', kind: 'pullRequest', head: 'a'.repeat(40) })

afterEach(() => {
  vi.useRealTimers()
})

describe('WorktreesView', () => {
  it('lists again only when Refresh is clicked, never on a timer', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await screen.findByRole('region', { name: 'example-repo' })

    vi.useFakeTimers()
    act(() => {
      vi.advanceTimersByTime(30 * 60_000)
    })
    vi.useRealTimers()

    expect(platform.listWorktrees).toHaveBeenCalledOnce()
    expect(platform.on).not.toHaveBeenCalledWith('inbox:updated', expect.any(Function))

    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [], cleanCount: 0 }))
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    expect(await screen.findByText('No clone under ~/projects has linked worktrees.')).toBeInTheDocument()
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it("shows each worktree's branch, path, status, age, and origin", async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree({ lastCommitUnix: Date.parse('2026-06-12T12:00:00Z') / 1000 }),
              worktree({ lastCommitUnix: Date.parse('2026-06-15T09:00:00Z') / 1000 }),
              worktree({
                path: `${repoPath}-scratch`,
                displayPath: '~/projects/example-repo-scratch',
                branch: '',
                detached: true,
                dirty: true,
                dirtyCount: 1,
                managed: false,
                legacy: false,
                canRemove: false,
                canForceRemove: true,
              }),
              worktree({
                path: `${repoPath}/.claude/worktrees/issue-9`,
                displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
                branch: 'worktree-issue-9',
                prunable: true,
                prunableReason: 'gitdir file points to non-existent location',
                canRemove: false,
              }),
            ],
          }),
          worktreeRepo({
            path: '/home/user/projects/broken-repo',
            displayPath: '~/projects/broken-repo',
            name: 'broken-repo',
            error: 'fatal: not a git repository',
            worktrees: [],
          }),
        ],
      }),
    )
    render(<WorktreesView clock={fixedNow} />)

    const example = await repo('example-repo')
    expect(example.getByText('4 worktrees')).toBeInTheDocument()

    const main = row('~/projects/example-repo')
    expect(main.getByText('main')).toBeInTheDocument()
    expect(main.getByText('clean')).toBeInTheDocument()
    expect(main.getByText('3d ago')).toHaveAttribute('dateTime', '2026-06-12T12:00:00.000Z')
    expect(main.queryByText(/made by/)).not.toBeInTheDocument()

    const linked = row(linkedDisplay)
    expect(linked.getByText('worktree-pr-7')).toBeInTheDocument()
    expect(linked.getByText('clean')).toBeInTheDocument()
    expect(linked.getByText('made by Claude (legacy)')).toBeInTheDocument()
    expect(linked.getByText('3h ago')).toBeInTheDocument()
    expect(linked.getByRole('button', { name: 'Remove' })).toBeInTheDocument()
    expect(linked.queryByRole('button', { name: 'Force remove' })).not.toBeInTheDocument()

    const scratch = row('~/projects/example-repo-scratch')
    expect(scratch.getByText('detached')).toBeInTheDocument()
    expect(scratch.getByText('dirty · 1 uncommitted file')).toBeInTheDocument()
    expect(scratch.queryByText(/last commit/)).not.toBeInTheDocument()
    expect(scratch.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()
    expect(scratch.getByRole('button', { name: 'Force remove' })).toBeInTheDocument()

    const gone = row('~/projects/example-repo/.claude/worktrees/issue-9')
    expect(gone.getByText('prunable')).toBeInTheDocument()
    expect(gone.getByText(/gitdir file points to non-existent location/)).toBeInTheDocument()
    expect(gone.queryByRole('button')).not.toBeInTheDocument()

    const broken = await repo('broken-repo')
    expect(broken.getByText('fatal: not a git repository')).toBeInTheDocument()
    expect(broken.getByText('0 worktrees')).toBeInTheDocument()
  })

  it('shows a worktree whose status could not be read', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [mainWorktree(), worktree({ canRemove: false, error: 'could not read the status: exit 128' })],
          }),
        ],
      }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    const linked = row(linkedDisplay)
    expect(linked.getByText('could not read the status: exit 128')).toBeInTheDocument()
    expect(linked.queryByText('clean')).not.toBeInTheDocument()
  })

  it('main worktree has no remove button', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    const main = row('~/projects/example-repo')
    expect(main.getByText('main worktree')).toBeInTheDocument()
    expect(main.queryByRole('button')).not.toBeInTheDocument()
  })

  it('explains each kind of lock and offers Unlock only for an ended session', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              worktree({
                locked: true,
                lockReason: 'claude session pr-7 (pid 4242)',
                agentLock: true,
                staleLock: true,
                canUnlock: true,
                dirty: true,
                dirtyCount: 2,
                canRemove: false,
              }),
              worktree({
                path: `${repoPath}/.claude/worktrees/pr-8`,
                displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
                locked: true,
                lockReason: 'claude session pr-8 (pid 99)',
                agentLock: true,
                stage: 'in-use',
                canRemove: false,
              }),
              worktree({
                path: `${repoPath}/.claude/worktrees/issue-9`,
                displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
                locked: true,
                canRemove: false,
              }),
            ],
          }),
        ],
        cleanCount: 0,
      }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    const ended = row(linkedDisplay)
    expect(ended.getByText('locked')).toBeInTheDocument()
    expect(
      ended.getByText(
        'Locked by an agent session that has ended (claude session pr-7 (pid 4242)). Unlock it to remove it.',
      ),
    ).toBeInTheDocument()
    expect(ended.getByRole('button', { name: 'Unlock' })).toBeInTheDocument()
    expect(ended.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()
    expect(ended.queryByText(/git worktree unlock/)).not.toBeInTheDocument()

    const running = row('~/projects/example-repo/.claude/worktrees/pr-8')
    expect(running.getByText('in use')).toBeInTheDocument()
    expect(running.getByText(/An agent session is running in this worktree \(claude session pr-8/)).toBeInTheDocument()
    // A worktree a session runs in can only have its path copied.
    expect(running.getAllByRole('button').map((b) => b.textContent)).toEqual(['Copy path'])

    const byHand = row('~/projects/example-repo/.claude/worktrees/issue-9')
    expect(byHand.getByText(/^Locked\. It was locked by hand/)).toBeInTheDocument()
    expect(
      byHand.getByText('git worktree unlock ~/projects/example-repo/.claude/worktrees/issue-9'),
    ).toBeInTheDocument()
    expect(byHand.getAllByRole('button').map((b) => b.textContent)).toEqual(['Copy path'])
  })

  it('unlocks a worktree whose session has ended, then lists again', async () => {
    const stale = worktree({
      locked: true,
      lockReason: 'claude session pr-7 (pid 4242)',
      agentLock: true,
      staleLock: true,
      canUnlock: true,
      canRemove: false,
    })
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), stale] })], cleanCount: 0 }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Unlock' }))

    expect(platform.unlockWorktree).toHaveBeenCalledWith(repoPath, linkedPath)
    expect(await screen.findByRole('status', { name: 'Unlocked example-repo/pr-7' })).toHaveTextContent(
      'The lock its ended session left was released, so it can be removed.',
    )
    expect(await row(linkedDisplay).findByRole('button', { name: 'Remove' })).toBeInTheDocument()

    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), stale] })], cleanCount: 0 }),
    )
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    platform.unlockWorktree.mockRejectedValue(
      new Error('the agent session that locked the worktree may still be running'),
    )
    await userEvent.click(await row(linkedDisplay).findByRole('button', { name: 'Unlock' }))

    expect(await row(linkedDisplay).findByRole('alert')).toHaveTextContent(
      'the agent session that locked the worktree may still be running',
    )
  })

  it('links each worktree to its pull request or issue and shows its stage', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              finishedWorktree(),
              worktree({
                path: `${repoPath}/.claude/worktrees/issue-9`,
                displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
                branch: 'worktree-issue-9',
                unpushed: 2,
                onGitHub: false,
                item: linkedItem({
                  kind: 'issue',
                  number: 9,
                  url: 'https://github.com/acme-org/example-repo/issues/9',
                  state: 'closed',
                  closedAt: '2026-06-14T12:00:00Z',
                }),
                stage: 'needs-review',
              }),
              worktree({
                path: `${repoPath}/.claude/worktrees/pr-12`,
                displayPath: '~/projects/example-repo/.claude/worktrees/pr-12',
                branch: 'worktree-pr-12',
                item: linkedItem({ number: 12, state: 'open', closedAt: '' }),
                stage: 'active',
              }),
              worktree({
                path: `${repoPath}/.claude/worktrees/pr-13`,
                displayPath: '~/projects/example-repo/.claude/worktrees/pr-13',
                branch: 'worktree-pr-13',
                item: linkedItem({ number: 13, closedAt: '' }),
                unpushed: -1,
                onGitHub: false,
              }),
            ],
          }),
        ],
        finishedCount: 1,
        reviewCount: 1,
      }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    const finished = row(linkedDisplay)
    expect(finished.getByText('ready to clean up')).toBeInTheDocument()
    expect(finished.getByRole('button', { name: 'PR #7' })).toHaveAttribute(
      'title',
      'acme-org/example-repo#7: Add the widget cache',
    )
    expect(finished.getByText('2h ago')).toHaveAttribute('dateTime', '2026-06-15T10:00:00Z')
    expect(finished.getByText(/merged/)).toBeInTheDocument()
    expect(finished.getByRole('button', { name: 'Clean up' })).toHaveAttribute(
      'title',
      'Removes the worktree, whose every commit is on GitHub; asks which branches go with it',
    )
    expect(finished.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()
    expect(finished.queryByText(/not on GitHub/)).not.toBeInTheDocument()

    await userEvent.click(finished.getByRole('button', { name: 'PR #7' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')

    const review = row('~/projects/example-repo/.claude/worktrees/issue-9')
    expect(review.getByText('needs a look')).toBeInTheDocument()
    expect(review.getByText('2 commits not on GitHub')).toBeInTheDocument()
    expect(review.getByRole('button', { name: 'Issue #9' })).toBeInTheDocument()
    expect(review.getByText(/closed/)).toBeInTheDocument()
    expect(review.queryByRole('button', { name: 'Clean up' })).not.toBeInTheDocument()

    const open = row('~/projects/example-repo/.claude/worktrees/pr-12')
    expect(open.getByText(/open$/)).toBeInTheDocument()
    expect(open.queryByText('ready to clean up')).not.toBeInTheDocument()

    const undated = row('~/projects/example-repo/.claude/worktrees/pr-13')
    expect(undated.getByText(/merged$/)).toBeInTheDocument()
    expect(undated.queryByText(/not on GitHub/)).not.toBeInTheDocument()

    expect(screen.getByRole('button', { name: 'Clean up finished (1)' })).toBeEnabled()
  })

  it('cleans up a finished worktree once confirmed, with its branch ticked, and says what it did', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              finishedWorktree({
                locked: true,
                lockReason: 'claude session pr-7 (pid 4242)',
                agentLock: true,
                staleLock: true,
                canUnlock: true,
                canRemove: false,
              }),
            ],
          }),
        ],
        finishedCount: 1,
      }),
    )
    platform.cleanUpWorktree.mockResolvedValue({
      repoPath,
      path: linkedPath,
      displayPath: linkedDisplay,
      removed: true,
      deletedBranches: ['worktree-pr-7'],
      item: 'acme-org/example-repo#7',
      reason: '',
    })
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    const finished = row(linkedDisplay)
    expect(finished.getByText(/Clean up releases the lock first\./)).toBeInTheDocument()
    expect(finished.queryByRole('button', { name: 'Unlock' })).not.toBeInTheDocument()

    await userEvent.click(finished.getByRole('button', { name: 'Clean up' }))

    const dialog = screen.getByRole('dialog', { name: 'Clean up this worktree?' })
    expect(dialog).toHaveTextContent(
      `This deletes the folder ${linkedDisplay}, whose work is finished: it has no uncommitted changes, and every ` +
        'commit on it is on GitHub.Made for pull request #7, merged.',
    )
    const box = within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })
    expect(box).toBeChecked()
    expect(box).toHaveAccessibleName(/the branch made for this worktree · at fc1c397$/)
    expect(platform.cleanUpWorktree).not.toHaveBeenCalled()

    await confirmWith('Clean up')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.cleanUpWorktree).toHaveBeenCalledWith(repoPath, linkedPath, ['worktree-pr-7'])
    expect(await screen.findByRole('status', { name: 'Cleaned up example-repo/pr-7' })).toHaveTextContent(
      'Its branch worktree-pr-7 was deleted.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('says when a cleaned-up worktree kept its branch, and shows a refusal', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [mainWorktree(), finishedWorktree({ ownBranch: false, branch: 'feat/x', branches: [] })],
          }),
        ],
      }),
    )
    const result = {
      repoPath,
      path: linkedPath,
      displayPath: linkedDisplay,
      removed: true,
      deletedBranches: [],
      item: 'acme-org/example-repo#7',
      reason: '',
    }
    platform.cleanUpWorktree.mockResolvedValueOnce(result)
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Clean up' }))
    const dialog = screen.getByRole('dialog', { name: 'Clean up this worktree?' })
    expect(dialog).toHaveTextContent(
      'Its branch feat/x stays: it is neither the branch made for this worktree nor its pull request’s.',
    )
    expect(within(dialog).queryByRole('checkbox')).not.toBeInTheDocument()

    await confirmWith('Clean up')
    expect(platform.cleanUpWorktree).toHaveBeenCalledWith(repoPath, linkedPath, [])
    expect(await screen.findByRole('status', { name: 'Cleaned up example-repo/pr-7' })).toHaveTextContent(
      'No branch was deleted.',
    )

    platform.cleanUpWorktree.mockResolvedValueOnce({
      ...result,
      reason: 'kept the branch worktree-pr-7: git: cannot lock ref',
    })
    await userEvent.click(await row(linkedDisplay).findByRole('button', { name: 'Clean up' }))
    await confirmWith('Clean up')
    expect(await screen.findByRole('status', { name: 'Cleaned up example-repo/pr-7' })).toHaveTextContent(
      'No branch was deleted. Kept the branch worktree-pr-7: git: cannot lock ref.',
    )

    platform.cleanUpWorktree.mockRejectedValueOnce(
      new Error('the worktree is not finished: it has uncommitted changes'),
    )
    await userEvent.click(await row(linkedDisplay).findByRole('button', { name: 'Clean up' }))
    await confirmWith('Clean up')
    expect(await row(linkedDisplay).findByRole('alert')).toHaveTextContent(
      'the worktree is not finished: it has uncommitted changes',
    )
  })

  it('confirms clean up finished, listing each worktree and its branches, and reports each outcome', async () => {
    const pr8Path = `${repoPath}/.claude/worktrees/pr-8`
    const pr8 = finishedWorktree({
      path: pr8Path,
      displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
      branch: 'feature-x',
      item: linkedItem({ number: 8, headRef: 'feature-x' }),
      branches: [ownBranch({ name: 'worktree-pr-8' }), prBranch],
    })
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [worktreeRepo({ worktrees: [mainWorktree(), finishedWorktree(), pr8] })],
        finishedCount: 2,
      }),
    )
    platform.cleanUpFinishedWorktrees.mockResolvedValue([
      {
        repoPath,
        path: linkedPath,
        displayPath: linkedDisplay,
        removed: true,
        deletedBranches: ['worktree-pr-7'],
        item: 'acme-org/example-repo#7',
        reason: '',
      },
      {
        repoPath,
        path: `${repoPath}/.claude/worktrees/pr-8`,
        displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
        removed: false,
        deletedBranches: [],
        item: 'acme-org/example-repo#8',
        reason: 'the worktree changed since it was checked; refresh and try again',
      },
    ])
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (2)' }))

    const dialog = screen.getByRole('dialog', { name: 'Clean up finished worktrees?' })
    expect(dialog).toHaveTextContent('The pull request or issue of each of these is finished')
    const listed = within(within(dialog).getByRole('list', { name: 'Worktrees' }))
    expect(listed.getByRole('checkbox', { name: `${linkedDisplay}pull request #7, merged` })).toBeChecked()
    expect(listed.getByRole('checkbox', { name: /pr-8pull request #8, merged$/ })).toBeChecked()
    const prBox = within(
      listed.getByRole('list', { name: 'Branches of ~/projects/example-repo/.claude/worktrees/pr-8' }),
    ).getByRole('checkbox', { name: /^Delete branch feature-x/ })
    expect(prBox).toHaveAccessibleName(/the pull request’s branch, checked out here · at aaaaaaa$/)
    expect(prBox).toBeChecked()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(platform.cleanUpFinishedWorktrees).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (2)' }))
    await userEvent.click(screen.getByRole('checkbox', { name: /^Delete branch feature-x/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 2 worktrees' }))

    expect(platform.cleanUpFinishedWorktrees).toHaveBeenCalledExactlyOnceWith([
      { repoPath, path: linkedPath, branches: ['worktree-pr-7'] },
      { repoPath, path: pr8Path, branches: ['worktree-pr-8'] },
    ])
    const results = await screen.findByRole('status')
    expect(results).toHaveAccessibleName('Cleaned up 1 of 2 finished worktrees')
    expect(results).toHaveTextContent('1 branch deleted · 1 left in place, as noted below.')
    expect(results).toHaveTextContent(
      '~/projects/example-repo/.claude/worktrees/pr-8: the worktree changed since it was checked; refresh and try again',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('leaves out a worktree unticked, holding its branches, and confirms nothing with none ticked', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [worktreeRepo({ worktrees: [mainWorktree(), finishedWorktree()] })],
        finishedCount: 1,
      }),
    )
    render(<WorktreesView clock={fixedNow} branchRules={rules({ own: false })} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (1)' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Clean up finished worktrees?' }))
    const branch = dialog.getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })
    expect(branch).not.toBeChecked()
    expect(branch).toBeEnabled()

    await userEvent.click(dialog.getByRole('checkbox', { name: /^~\/projects/ }))

    expect(branch).toBeDisabled()
    expect(dialog.getByRole('button', { name: 'Clean up 0 worktrees' })).toBeDisabled()

    await userEvent.click(dialog.getByRole('checkbox', { name: /^~\/projects/ }))
    await userEvent.click(branch)
    await confirmWith('Clean up 1 worktree')

    expect(platform.cleanUpFinishedWorktrees).toHaveBeenCalledExactlyOnceWith([
      { repoPath, path: linkedPath, branches: ['worktree-pr-7'] },
    ])
  })

  it('reports a clean up finished that had nothing to do or failed', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [worktreeRepo({ worktrees: [mainWorktree(), finishedWorktree()] })],
        finishedCount: 1,
      }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 worktree' }))
    expect(await screen.findByRole('status')).toHaveTextContent('There were no finished worktrees to remove')

    platform.cleanUpFinishedWorktrees.mockRejectedValue(
      new Error('GitHub could not be asked which worktrees are finished'),
    )
    await userEvent.click(await screen.findByRole('button', { name: 'Clean up finished (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 worktree' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('GitHub could not be asked which worktrees are finished')
  })

  it('says when GitHub could not be asked', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ linkError: 'Could not read the pull requests and issues from GitHub: offline.' }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    expect(
      screen.getByText(
        'Could not read the pull requests and issues from GitHub: offline. Worktrees are shown without their pull requests and issues, so none counts as finished.',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clean up finished (0)' })).toBeDisabled()
  })

  it('lists again when reloadKey changes and reports each listing', async () => {
    const onSnapshot = vi.fn()
    const listing = worktreesSnapshot({ finishedCount: 1 })
    platform.listWorktrees.mockResolvedValue(listing)
    const { rerender } = render(<WorktreesView clock={fixedNow} reloadKey={1} onSnapshot={onSnapshot} />)
    await repo('example-repo')

    await vi.waitFor(() => {
      expect(onSnapshot).toHaveBeenCalledWith(listing)
    })

    rerender(<WorktreesView clock={fixedNow} reloadKey={1} onSnapshot={vi.fn()} />)
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    rerender(<WorktreesView clock={fixedNow} reloadKey={2} onSnapshot={onSnapshot} />)
    await vi.waitFor(() => {
      expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    })
    await vi.waitFor(() => {
      expect(onSnapshot).toHaveBeenCalledTimes(2)
    })
  })

  it('removes a clean worktree once confirmed, saying what becomes of its branch, then lists again', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree()] })], cleanCount: 0 }),
    )

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))

    const dialog = screen.getByRole('dialog', { name: 'Remove this worktree?' })
    expect(dialog).toHaveTextContent(`This deletes the folder ${linkedDisplay}, which has no uncommitted changes.`)
    expect(within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })).toBeChecked()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(platform.removeWorktree).not.toHaveBeenCalled()

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))
    await confirmWith('Remove')

    expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, ['worktree-pr-7'])
    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'Its branch worktree-pr-7 was deleted.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    expect(await screen.findByRole('button', { name: 'Remove all clean (0)' })).toBeDisabled()
    expect(screen.queryByText(linkedDisplay)).not.toBeInTheDocument()
  })

  it("shows git's refusal on the row", async () => {
    platform.removeWorktree.mockRejectedValue(
      new Error(`git refused to remove ${linkedDisplay}: has uncommitted changes`),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove' }))

    expect(await row(linkedDisplay).findByRole('alert')).toHaveTextContent(
      `git refused to remove ${linkedDisplay}: has uncommitted changes`,
    )
    expect(platform.listWorktrees).toHaveBeenCalledOnce()
    expect(row(linkedDisplay).getByRole('button', { name: 'Remove' })).toBeEnabled()
  })

  it('says a branch with commits not on GitHub stays, and that the removal kept it', async () => {
    const kept = worktree({
      onGitHub: false,
      unpushed: 2,
      item: linkedItem({ kind: 'issue', number: 202, state: 'open' }),
      branches: [ownBranch({ deletable: false, reason: "2 commits on it aren't on GitHub" })],
    })
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), kept] })] }),
    )
    platform.removeWorktree.mockResolvedValue(
      removeResult({ deletedBranches: [], reason: 'kept the branch worktree-pr-7, which has 2 commits not on GitHub' }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))

    const dialog = screen.getByRole('dialog', { name: 'Remove this worktree?' })
    const box = within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })
    expect(box).not.toBeChecked()
    expect(box).toBeDisabled()
    expect(dialog).toHaveTextContent("Stays: 2 commits on it aren't on GitHub.")
    expect(dialog).toHaveTextContent('Made for issue #202, open.')

    await confirmWith('Remove')

    expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, [])
    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'No branch was deleted. Kept the branch worktree-pr-7, which has 2 commits not on GitHub.',
    )
  })

  // removals are worktrees to remove, what the confirmation says of their branches, and
  // the branches the removal deletes, as ticked to start with.
  const removals: {
    name: string
    overrides: Partial<Worktree>
    branchRules?: BranchRules
    text: string
    branches: string[]
  }[] = [
    {
      name: 'its own branch, ticked as the rules say',
      overrides: {},
      text: 'Delete branch worktree-pr-7the branch made for this worktree · at fc1c397',
      branches: ['worktree-pr-7'],
    },
    {
      name: 'its own branch, unticked when the rules keep it',
      overrides: {},
      branchRules: rules({ own: false }),
      text: 'Delete branch worktree-pr-7',
      branches: [],
    },
    {
      name: 'the pull request’s branch as well',
      overrides: { branch: 'feature-x', branches: [ownBranch(), prBranch] },
      text: 'Delete branch feature-xthe pull request’s branch, checked out here · at aaaaaaa',
      branches: ['worktree-pr-7', 'feature-x'],
    },
    {
      name: 'the pull request’s branch, unticked when the rules keep it',
      overrides: { branch: 'feature-x', branches: [prBranch] },
      branchRules: rules({ pullRequest: false }),
      text: 'Delete branch feature-x',
      branches: [],
    },
    {
      name: 'a branch it did not make',
      overrides: { branch: 'feature', ownBranch: false, branches: [] },
      text: 'Its branch feature stays: it is neither the branch made for this worktree nor its pull request’s.',
      branches: [],
    },
    {
      name: 'no branch',
      overrides: { branch: '', detached: true, branches: [] },
      text: 'It has no branch.',
      branches: [],
    },
  ]

  it.each(removals)(
    'says what removing a worktree with $name does, and deletes what is ticked',
    async ({ overrides, branchRules, text, branches }) => {
      platform.listWorktrees.mockResolvedValue(
        worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), worktree(overrides)] })] }),
      )
      render(<WorktreesView clock={fixedNow} branchRules={branchRules} />)
      await repo('example-repo')

      await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))

      expect(screen.getByRole('dialog', { name: 'Remove this worktree?' })).toHaveTextContent(text)

      await confirmWith('Remove')

      expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, branches)
    },
  )

  it('reports a removal that deleted no branch, as the user unticked it', async () => {
    platform.removeWorktree.mockResolvedValue(removeResult({ deletedBranches: [] }))
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))
    await userEvent.click(screen.getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ }))
    await confirmWith('Remove')

    expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath, [])
    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'No branch was deleted.',
    )
  })

  it('names every branch a removal deleted', async () => {
    platform.removeWorktree.mockResolvedValue(removeResult({ deletedBranches: ['worktree-pr-7', 'feature-x'] }))
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))
    await confirmWith('Remove')

    expect(await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })).toHaveTextContent(
      'Its branches worktree-pr-7 and feature-x were deleted.',
    )
  })

  it('force remove confirms with the uncommitted count', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              worktree({ dirty: true, dirtyCount: 3, canRemove: false, canForceRemove: true }),
            ],
          }),
        ],
        cleanCount: 0,
      }),
    )
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Force remove' }))

    const dialog = screen.getByRole('dialog', { name: 'Force remove this worktree?' })
    expect(dialog).toHaveTextContent(`${linkedDisplay} has 3 uncommitted files`)
    expect(within(dialog).getByRole('checkbox', { name: /^Delete branch worktree-pr-7/ })).toBeChecked()

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.forceRemoveWorktree).not.toHaveBeenCalled()

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Force remove' }))
    await confirmWith('Force remove')

    expect(platform.forceRemoveWorktree).toHaveBeenCalledWith(repoPath, linkedPath, ['worktree-pr-7'])
    expect(platform.removeWorktree).not.toHaveBeenCalled()
    expect(await screen.findByRole('status', { name: 'Force removed example-repo/pr-7' })).toHaveTextContent(
      'Its branch worktree-pr-7 was deleted.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('confirms remove all clean, listing each worktree, and reports each outcome', async () => {
    const issue9 = worktree({
      path: `${repoPath}/.claude/worktrees/issue-9`,
      displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
      branch: 'worktree-issue-9',
      branches: [ownBranch({ name: 'worktree-issue-9' })],
    })
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), worktree(), issue9] })], cleanCount: 2 }),
    )
    platform.removeAllCleanWorktrees.mockResolvedValue([
      {
        repoPath,
        path: linkedPath,
        displayPath: linkedDisplay,
        removed: true,
        deletedBranches: [],
        item: '',
        reason: '',
      },
      {
        repoPath,
        path: `${repoPath}/.claude/worktrees/issue-9`,
        displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
        removed: false,
        deletedBranches: [],
        item: '',
        reason: 'has uncommitted changes',
      },
    ])
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (2)' }))

    const dialog = screen.getByRole('dialog', { name: 'Remove clean worktrees?' })
    expect(dialog).toHaveTextContent('Each of these has no uncommitted changes and is unlocked.')
    expect(within(dialog).getAllByRole('checkbox', { checked: true })).toHaveLength(4)

    await userEvent.keyboard('{Escape}')
    expect(platform.removeAllCleanWorktrees).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (2)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove 2 worktrees' }))

    expect(platform.removeAllCleanWorktrees).toHaveBeenCalledExactlyOnceWith([
      { repoPath, path: linkedPath, branches: ['worktree-pr-7'] },
      { repoPath, path: `${repoPath}/.claude/worktrees/issue-9`, branches: ['worktree-issue-9'] },
    ])
    const results = await screen.findByRole('status')
    expect(results).toHaveAccessibleName('Removed 1 of 2 clean worktrees')
    expect(results).toHaveTextContent('No branch was deleted · 1 left in place, as noted below.')
    expect(results).toHaveTextContent('~/projects/example-repo/.claude/worktrees/issue-9: has uncommitted changes')
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('reports a remove all clean that had nothing to do or failed', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    expect(await screen.findByRole('status')).toHaveTextContent('There were no clean worktrees to remove')

    platform.removeAllCleanWorktrees.mockRejectedValue(new Error('scan the clone root: permission denied'))
    await userEvent.click(await screen.findByRole('button', { name: 'Remove all clean (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('scan the clone root: permission denied')
  })

  it('reports a fully successful remove all clean', async () => {
    platform.removeAllCleanWorktrees.mockResolvedValue([
      {
        repoPath,
        path: linkedPath,
        displayPath: linkedDisplay,
        removed: true,
        deletedBranches: [],
        item: '',
        reason: '',
      },
    ])
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    const results = await screen.findByRole('status')
    expect(results).toHaveAccessibleName('Removed 1 clean worktree')
    expect(within(results).queryByRole('listitem')).not.toBeInTheDocument()
  })

  it('prune button only for repos with stale entries', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo(),
          worktreeRepo({
            path: '/home/user/projects/other-repo',
            displayPath: '~/projects/other-repo',
            name: 'other-repo',
            hasStale: true,
            worktrees: [
              mainWorktree({ path: '/home/user/projects/other-repo', displayPath: '~/projects/other-repo' }),
              worktree({
                path: '/home/user/projects/other-repo/.claude/worktrees/pr-3',
                displayPath: '~/projects/other-repo/.claude/worktrees/pr-3',
                prunable: true,
                canRemove: false,
              }),
            ],
          }),
        ],
      }),
    )
    render(<WorktreesView clock={fixedNow} />)

    expect((await repo('example-repo')).queryByRole('button', { name: 'Prune' })).not.toBeInTheDocument()

    await userEvent.click((await repo('other-repo')).getByRole('button', { name: 'Prune' }))

    expect(platform.pruneWorktrees).toHaveBeenCalledWith('/home/user/projects/other-repo')
    expect(await screen.findByRole('status', { name: 'Pruned ~/projects/other-repo' })).toHaveTextContent(
      'The entries of worktrees whose folders are gone were cleared.',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('shows a prune failure on its repository', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [worktreeRepo({ hasStale: true })] }))
    platform.pruneWorktrees.mockRejectedValue(new Error('prune ~/projects/example-repo: exit status 128'))
    render(<WorktreesView clock={fixedNow} />)

    await userEvent.click((await repo('example-repo')).getByRole('button', { name: 'Prune' }))

    expect(await (await repo('example-repo')).findByRole('alert')).toHaveTextContent(
      'prune ~/projects/example-repo: exit status 128',
    )
    expect(row('~/projects/example-repo').queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a listing failure and recovers on Refresh', async () => {
    platform.listWorktrees.mockRejectedValue(new Error('scan the clone root: no such directory'))
    render(<WorktreesView clock={fixedNow} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('scan the clone root: no such directory')
    expect(screen.queryByText('Listing the worktrees…')).not.toBeInTheDocument()

    platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('lists quietly after a background sweep, holding nothing', async () => {
    const { rerender } = render(<WorktreesView clock={fixedNow} reloadKey={1} />)
    await repo('example-repo')

    let finish: (snap: ReturnType<typeof worktreesSnapshot>) => void = () => undefined
    platform.listWorktrees.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    rerender(<WorktreesView clock={fixedNow} reloadKey={2} />)

    await vi.waitFor(() => {
      expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
    })
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    expect(screen.getByRole('button', { name: /^Remove all clean/ })).toBeEnabled()

    await act(async () => {
      finish(worktreesSnapshot())
      await Promise.resolve()
    })
  })

  it('holds the actions while a listing it was asked for runs', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    let finish: (snap: ReturnType<typeof worktreesSnapshot>) => void = () => undefined
    platform.listWorktrees.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    expect(screen.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: /^Remove all clean/ })).toBeDisabled()

    await act(async () => {
      finish(worktreesSnapshot())
      await Promise.resolve()
    })

    expect(await screen.findByRole('button', { name: 'Refresh' })).toBeEnabled()
  })

  it('says it is listing until the first listing arrives', async () => {
    let finish: (snap: ReturnType<typeof worktreesSnapshot>) => void = () => undefined
    platform.listWorktrees.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    render(<WorktreesView clock={fixedNow} />)

    expect(screen.getByText('Listing the worktrees…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()

    await act(async () => {
      finish(worktreesSnapshot({ repos: [], cleanCount: 0 }))
      await Promise.resolve()
    })

    expect(await screen.findByText('No clone under ~/projects has linked worktrees.')).toBeInTheDocument()
    expect(screen.getByText('Worktrees of the clones under ~/projects')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
  })
})

describe('WorktreesView sessions and origin', () => {
  const managedPath = `/home/user/projects/.${brand.slug}/worktrees/example-repo/pr-7`
  const managedDisplay = `~/projects/.${brand.slug}/worktrees/example-repo/pr-7`

  it('shows who made a worktree, the session working in it, and the last launch', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              worktree({
                path: managedPath,
                displayPath: managedDisplay,
                legacy: false,
                agent: 'codex',
                lastLaunchedAt: '2026-06-15T09:00:00Z',
                launches: 2,
                inUse: [
                  { pid: 4242, command: '2.1.319', agent: 'claude' },
                  { pid: 4300, command: 'zsh', agent: '' },
                ],
                stage: 'in-use',
                canRemove: false,
              }),
            ],
          }),
        ],
      }),
    )
    render(<WorktreesView clock={fixedNow} agentName={(id) => (id === 'codex' ? 'Codex' : 'Claude')} />)
    await repo('example-repo')

    const managed = row(managedDisplay)
    expect(managed.getByText(`made by ${brand.name}`)).toBeInTheDocument()
    expect(managed.getByText('in use · Claude (pid 4242), zsh (pid 4300)')).toBeInTheDocument()
    // The process names the session, so the stage's own "in use" is not shown twice.
    expect(managed.queryByText('in use')).not.toBeInTheDocument()
    expect(managed.getByText(/Codex/)).toHaveTextContent('Codex 3h ago')
    expect(managed.getByText('3h ago', { selector: 'time' })).toHaveAttribute('dateTime', '2026-06-15T09:00:00Z')
  })

  it('copies a worktree’s path', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    const linked = row(linkedDisplay)
    await userEvent.click(linked.getByRole('button', { name: 'Copy path' }))

    expect(platform.copyText).toHaveBeenCalledWith(linkedPath)
    expect(await linked.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(row('~/projects/example-repo').queryByRole('button', { name: 'Copy path' })).not.toBeInTheDocument()
  })

  it('puts the launch control on a worktree whose item the inbox lists', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [worktreeRepo({ worktrees: [mainWorktree(), worktree({ item: linkedItem({ state: 'open' }) })] })],
      }),
    )
    const launchFor = vi.fn((item: LinkedItem) => <button type="button">Open #{item.number}</button>)
    render(<WorktreesView clock={fixedNow} launchFor={launchFor} />)
    await repo('example-repo')

    expect(row(linkedDisplay).getByRole('button', { name: 'Open #7' })).toBeInTheDocument()
    expect(launchFor).toHaveBeenCalledWith(expect.objectContaining({ number: 7 }))
    expect(row('~/projects/example-repo').queryByRole('button', { name: /Open/ })).not.toBeInTheDocument()
  })

  it('says when the system cannot list the running processes', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ sessionsUnknown: true }))
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    expect(screen.getByText(/can't list the running processes/)).toBeInTheDocument()
  })
})

describe('WorktreesView history', () => {
  // history waits for the recently cleaned-up list and returns the queries scoped to it.
  async function history() {
    return within(await screen.findByRole('region', { name: 'Recently cleaned up' }))
  }

  it('lists what was cleaned up, how, and when', async () => {
    platform.recentCleanups.mockResolvedValue([
      worktreeCleanup(),
      worktreeCleanup({
        id: 2,
        cleanedAt: '2026-06-13T12:00:00Z',
        automatic: false,
        worktree: 'issue-9',
        displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
        deletedBranch: '',
        head: '',
        item: linkedItem({
          kind: 'issue',
          number: 9,
          title: 'Crash on start',
          url: 'https://github.com/acme-org/example-repo/issues/9',
          state: 'closed',
        }),
      }),
      worktreeCleanup({ id: 3, worktree: 'scratch', item: null }),
    ])
    render(<WorktreesView clock={fixedNow} />)

    const list = await history()
    expect(list.getByText('Everything in them was on GitHub')).toBeInTheDocument()

    const rows = list.getAllByRole('listitem')
    expect(rows).toHaveLength(3)

    const auto = within(rows[0] as HTMLElement)
    expect(auto.getByText('example-repo')).toBeInTheDocument()
    expect(auto.getByText('pr-7')).toHaveAttribute('title', '~/projects/example-repo/.claude/worktrees/pr-7')
    expect(auto.getByText(/Add the widget cache/)).toBeInTheDocument()
    expect(rows[0]).toHaveTextContent(
      'Cleaned up automatically after the pull request merged · deleted worktree-pr-7 · was at fc1c397',
    )
    expect(auto.getByText('fc1c397')).toHaveAttribute('title', 'fc1c3975011e66491785ace891c893e4fc716587')
    const when = auto.getByText('2h ago')
    expect(when).toHaveAttribute('dateTime', '2026-06-15T10:00:00Z')
    expect(when.getAttribute('title')).toMatch(/2026/)

    await userEvent.click(auto.getByRole('button', { name: 'PR #7' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')

    const manual = within(rows[1] as HTMLElement)
    expect(manual.getByRole('button', { name: 'Issue #9' })).toHaveAttribute('title', 'acme-org/example-repo#9')
    expect(rows[1]).toHaveTextContent('Cleaned up by you after the issue closed · its branch was kept')
    expect(rows[1]).not.toHaveTextContent('was at')
    expect(manual.getByText('2d ago')).toBeInTheDocument()

    expect(rows[2]).toHaveTextContent('Cleaned up automatically · deleted worktree-pr-7')
    expect(within(rows[2] as HTMLElement).queryByRole('button')).not.toBeInTheDocument()

    expect(list.queryByRole('button', { name: /more/ })).not.toBeInTheDocument()
  })

  it('shows the newest five, and the rest on request', async () => {
    platform.recentCleanups.mockResolvedValue(
      Array.from({ length: 7 }, (_, i) => worktreeCleanup({ id: i + 1, worktree: `pr-${String(i + 1)}` })),
    )
    render(<WorktreesView clock={fixedNow} />)

    const list = await history()
    expect(list.getAllByRole('listitem')).toHaveLength(5)

    const more = list.getByRole('button', { name: 'Show 2 more' })
    expect(more).toHaveAttribute('aria-expanded', 'false')

    await userEvent.click(more)
    expect(list.getAllByRole('listitem')).toHaveLength(7)
    expect(list.getByText('pr-7')).toBeInTheDocument()

    await userEvent.click(list.getByRole('button', { name: 'Show fewer' }))
    expect(list.getAllByRole('listitem')).toHaveLength(5)
  })

  it('is left out when nothing was cleaned up', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    expect(platform.recentCleanups).toHaveBeenCalledOnce()
    expect(screen.queryByRole('region', { name: 'Recently cleaned up' })).not.toBeInTheDocument()
  })

  it('clears the list, and shows a clear that failed', async () => {
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    platform.clearCleanups.mockRejectedValueOnce(new Error('database is locked'))
    render(<WorktreesView clock={fixedNow} />)

    await userEvent.click((await history()).getByRole('button', { name: 'Clear' }))
    expect(await (await history()).findByRole('alert')).toHaveTextContent('database is locked')

    await userEvent.click((await history()).getByRole('button', { name: 'Clear' }))

    expect(platform.clearCleanups).toHaveBeenCalledTimes(2)
    await vi.waitFor(() => {
      expect(screen.queryByRole('region', { name: 'Recently cleaned up' })).not.toBeInTheDocument()
    })
  })

  it('lists again after a change, so a cleanup shows at once', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), finishedWorktree()] })] }),
    )
    platform.cleanUpWorktree.mockResolvedValue({
      repoPath,
      path: linkedPath,
      displayPath: linkedDisplay,
      removed: true,
      deletedBranches: ['worktree-pr-7'],
      item: 'acme-org/example-repo#7',
      reason: '',
    })
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    platform.recentCleanups.mockResolvedValue([worktreeCleanup({ automatic: false })])
    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Clean up' }))
    await confirmWith('Clean up')

    expect((await history()).getByText(/Cleaned up by you/)).toBeInTheDocument()
  })

  it('scrolls to the list once when asked to', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    const { rerender } = render(<WorktreesView clock={fixedNow} focusHistory />)

    await history()
    await vi.waitFor(() => {
      expect(scroll).toHaveBeenCalledExactlyOnceWith({ block: 'start' })
    })

    rerender(<WorktreesView clock={fixedNow} focusHistory reloadKey={1} />)
    await vi.waitFor(() => {
      expect(platform.recentCleanups).toHaveBeenCalledTimes(2)
    })
    expect(scroll).toHaveBeenCalledExactlyOnceWith({ block: 'start' })
  })

  it('scrolls to the list again when asked again, as a page kept between visits is', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    const { rerender } = render(<WorktreesView clock={fixedNow} focusHistory />)

    await history()
    await vi.waitFor(() => {
      expect(scroll).toHaveBeenCalledOnce()
    })

    rerender(<WorktreesView clock={fixedNow} />)
    rerender(<WorktreesView clock={fixedNow} focusHistory />)

    expect(scroll).toHaveBeenCalledTimes(2)
  })

  it('does not scroll when not asked to', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
    render(<WorktreesView clock={fixedNow} />)

    await history()
    expect(scroll).not.toHaveBeenCalled()
  })

  it('logs a list that could not be read and still lists the worktrees', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.recentCleanups.mockRejectedValue(new Error('database is locked'))
    render(<WorktreesView clock={fixedNow} />)

    await repo('example-repo')
    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not read the recently cleaned-up worktrees', expect.any(Error))
    })
    expect(screen.queryByRole('region', { name: 'Recently cleaned up' })).not.toBeInTheDocument()
    error.mockRestore()
  })
})

describe('WorktreesView merged branches', () => {
  const otherPath = '/home/user/projects/other-repo'

  // merged is a merged branch named name, in example-repo unless overrides say otherwise.
  function merged(name: string, number: number, overrides: Partial<MergedBranch> = {}): MergedBranch {
    const base = mergedBranch()

    return mergedBranch({
      branch: { ...base.branch, name, head: String(number).repeat(40).slice(0, 40) },
      pullRequest: { ...base.pullRequest, number, title: `Change ${String(number)}` },
      ...overrides,
    })
  }

  // busy is a merged branch the clone has checked out, so it can't go.
  const busy = merged('busy', 187, {
    branch: { ...mergedBranch().branch, name: 'busy', deletable: false, reason: 'it is the clone’s current branch' },
  })

  // result is the outcome of deleting one merged branch.
  function result(name: string, overrides: Partial<BranchResult> = {}): BranchResult {
    return {
      repoPath,
      displayRepo: '~/projects/example-repo',
      name,
      head: 'fc1c3975011e66491785ace891c893e4fc716587',
      deleted: true,
      reason: '',
      ...overrides,
    }
  }

  async function openMerged(branchRules?: BranchRules) {
    render(<WorktreesView clock={fixedNow} branchRules={branchRules} />)
    await repo('example-repo')
    await userEvent.click(screen.getByRole('button', { name: 'Clean up merged branches…' }))

    return within(screen.getByRole('dialog', { name: 'Delete merged branches?' }))
  }

  // clone returns the row of one clone in the dialog: its box, its toggle, and the queries
  // scoped to it.
  function clone(dialog: BoundFunctions<typeof queries>, displayRepo: string) {
    const toggle = dialog.getByRole('button', { name: displayRepo })
    const li = toggle.closest('li')

    if (li === null) {
      throw new Error(`no row shows ${displayRepo}`)
    }

    return { toggle, box: dialog.getByRole('checkbox', { name: `Every branch in ${displayRepo}` }), row: within(li) }
  }

  it('checks the branches against GitHub, groups them by clone, closed, and deletes those ticked', async () => {
    let answer: (found: MergedBranch[]) => void = () => undefined
    platform.mergedBranches.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    platform.deleteBranches.mockResolvedValue([
      result('feature-x'),
      result('feature-z', {
        repoPath: otherPath,
        displayRepo: '~/projects/other-repo',
        deleted: false,
        reason: 'the branch moved since it was checked; refresh and try again',
      }),
    ])

    const dialog = await openMerged()
    const checking = dialog.getByRole('status')
    expect(checking).toHaveTextContent('Checking your clones’ branches against GitHub…')
    expect(checking).toHaveTextContent('then GitHub is asked which of their pull requests merged')
    expect(dialog.getByRole('button', { name: 'Delete branches' })).toBeDisabled()

    act(() => {
      answer([
        busy,
        mergedBranch(),
        merged('feature-y', 185),
        merged('feature-z', 186, { repoPath: otherPath, displayRepo: '~/projects/other-repo' }),
      ])
    })

    expect(await dialog.findByText('4 branches in 2 clones · 3 ticked · 1 can’t be deleted')).toBeInTheDocument()

    const example = clone(dialog, '~/projects/example-repo')
    expect(example.toggle).toHaveAttribute('aria-expanded', 'false')
    expect(example.row.getByText('3 branches · 2 ticked')).toBeInTheDocument()
    expect(example.row.getByRole('img', { name: '1 branch can’t be deleted' })).toBeInTheDocument()
    expect(example.box).toBeChecked()
    expect(dialog.queryByRole('checkbox', { name: /^feature-x/ })).not.toBeInTheDocument()

    const other = clone(dialog, '~/projects/other-repo')
    expect(other.row.getByText('1 branch · 1 ticked')).toBeInTheDocument()
    expect(other.row.queryByRole('img')).not.toBeInTheDocument()

    await userEvent.click(example.toggle)
    expect(example.toggle).toHaveAttribute('aria-expanded', 'true')

    const branches = within(dialog.getByRole('list', { name: 'Branches in ~/projects/example-repo' }))
    const feature = branches.getByRole('checkbox', { name: /^feature-x/ })
    expect(feature).toBeChecked()
    expect(feature).toHaveAccessibleName(/merged 3d ago · at fc1c397#184 Add locktimes$/)
    const held = branches.getByRole('checkbox', { name: /^busy/ })
    expect(held).toBeDisabled()
    expect(held).not.toBeChecked()
    expect(branches.getByRole('img', { name: 'Can’t be deleted' })).toBeInTheDocument()
    expect(branches.getByText('Stays: it is the clone’s current branch.')).toBeInTheDocument()

    await userEvent.click(branches.getByRole('checkbox', { name: /^feature-y/ }))
    expect(example.box).toBePartiallyChecked()
    expect(example.row.getByText('3 branches · 1 ticked')).toBeInTheDocument()

    await userEvent.click(example.toggle)
    expect(dialog.queryByRole('list', { name: 'Branches in ~/projects/example-repo' })).not.toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Delete 2 branches' }))

    expect(platform.deleteBranches).toHaveBeenCalledExactlyOnceWith([
      { repoPath, name: 'feature-x', head: 'fc1c3975011e66491785ace891c893e4fc716587' },
      { repoPath: otherPath, name: 'feature-z', head: '1861861861861861861861861861861861861861' },
    ])

    const results = await screen.findByRole('status')
    expect(results).toHaveAccessibleName('Deleted 1 of 2 branches')
    expect(results).toHaveTextContent('Across 1 clone · 1 kept, as noted below.')
    expect(within(results).getByRole('list', { name: 'Branches kept' })).toHaveTextContent(
      '~/projects/other-repo: feature-z kept: the branch moved since it was checked; refresh and try again',
    )
    expect(within(results).getByRole('list', { name: 'Branches deleted' })).toHaveTextContent(
      '~/projects/example-repo: feature-x at fc1c397',
    )
    const disclosure = within(results).getByText('The branch deleted, with the commit each pointed at')
    const details = disclosure.closest('details')
    expect(details).not.toHaveAttribute('open')
    await userEvent.click(disclosure)
    expect(details).toHaveAttribute('open')
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('ticks and unticks a whole clone, and every branch at once', async () => {
    platform.mergedBranches.mockResolvedValue([
      mergedBranch(),
      merged('feature-y', 185),
      merged('feature-z', 186, { repoPath: otherPath, displayRepo: '~/projects/other-repo' }),
    ])
    const dialog = await openMerged(rules({ merged: false }))

    const example = clone(dialog, '~/projects/example-repo')
    expect(await dialog.findByText('3 branches in 2 clones · 0 ticked')).toBeInTheDocument()
    expect(example.box).not.toBeChecked()

    await userEvent.click(example.box)
    expect(example.box).toBeChecked()
    expect(dialog.getByRole('button', { name: 'Delete 2 branches' })).toBeEnabled()

    await userEvent.click(example.box)
    expect(dialog.getByRole('button', { name: 'Delete 0 branches' })).toBeDisabled()

    await userEvent.click(dialog.getByRole('button', { name: 'Select all' }))
    expect(dialog.getByRole('button', { name: 'Delete 3 branches' })).toBeEnabled()
    expect(clone(dialog, '~/projects/other-repo').box).toBeChecked()

    await userEvent.click(dialog.getByRole('button', { name: 'Select none' }))
    expect(dialog.getByRole('button', { name: 'Delete 0 branches' })).toBeDisabled()
  })

  it('holds the box of a clone none of whose branches can go', async () => {
    platform.mergedBranches.mockResolvedValue([busy])
    const dialog = await openMerged()

    const example = clone(dialog, '~/projects/example-repo')
    expect(await dialog.findByText('1 branch in 1 clone · 0 ticked · 1 can’t be deleted')).toBeInTheDocument()
    expect(example.box).toBeDisabled()
    expect(example.box).not.toBeChecked()
    expect(example.row.getByText('1 branch · 0 ticked')).toBeInTheDocument()
  })

  it('says when no branch belongs to a merged pull request', async () => {
    const dialog = await openMerged()

    expect(
      await dialog.findByText(/No local branch in your clones belongs to a merged pull request/),
    ).toBeInTheDocument()
    expect(dialog.queryByRole('button', { name: 'Select all' })).not.toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.deleteBranches).not.toHaveBeenCalled()
  })

  it('shows why the branches could not be checked, and why they could not be deleted', async () => {
    platform.mergedBranches.mockRejectedValueOnce(new Error('GitHub is not available: offline'))
    const dialog = await openMerged()

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Couldn’t check the branches: GitHub is not available: offline',
    )
    await userEvent.keyboard('{Escape}')

    platform.mergedBranches.mockResolvedValue([mergedBranch()])
    platform.deleteBranches.mockRejectedValue(new Error('scan the clone root: permission denied'))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up merged branches…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 branch' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('scan the clone root: permission denied')
  })

  it('reports deleting every branch ticked, and copies the commands that bring them back', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.mergedBranches.mockResolvedValue([mergedBranch(), merged("it's-done", 185)])
    platform.deleteBranches.mockResolvedValue([
      result('feature-x'),
      result("it's-done", { head: '1851851851851851851851851851851851851851' }),
    ])
    await openMerged()

    await userEvent.click(await screen.findByRole('button', { name: 'Delete 2 branches' }))

    const results = await screen.findByRole('status')
    expect(results).toHaveAccessibleName('Deleted 2 branches')
    expect(results).toHaveTextContent('Across 1 clone.')
    expect(results).toHaveClass('border-success/40')
    expect(within(results).queryByRole('list', { name: 'Branches kept' })).not.toBeInTheDocument()
    expect(results).toHaveTextContent('The 2 branches deleted, with the commit each pointed at')

    platform.copyText.mockRejectedValueOnce(new Error('clipboard refused'))
    await userEvent.click(within(results).getByRole('button', { name: 'Copy restore commands' }))
    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not copy the restore commands', expect.any(Error))
    })

    await userEvent.click(within(results).getByRole('button', { name: 'Copy restore commands' }))

    expect(platform.copyText).toHaveBeenLastCalledWith(
      `git -C '${repoPath}' branch 'feature-x' fc1c3975011e66491785ace891c893e4fc716587\n` +
        `git -C '${repoPath}' branch 'it'\\''s-done' 1851851851851851851851851851851851851851`,
    )
    expect(await within(results).findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    error.mockRestore()
  })
})

describe('WorktreesView summaries', () => {
  it('reports what each change did, or why it failed, and puts the result away when dismissed', async () => {
    const onSummary = vi.fn()
    render(<WorktreesView clock={fixedNow} onSummary={onSummary} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))
    await confirmWith('Remove')

    const card = await screen.findByRole('status', { name: 'Removed example-repo/pr-7' })
    expect(onSummary).toHaveBeenLastCalledWith({
      title: 'Removed example-repo/pr-7',
      detail: 'Its branch worktree-pr-7 was deleted.',
      tone: 'success',
    })

    await userEvent.click(within(card).getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    platform.removeWorktree.mockRejectedValueOnce(new Error('git refused: has uncommitted changes'))
    await userEvent.click(await row(linkedDisplay).findByRole('button', { name: 'Remove' }))
    await confirmWith('Remove')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Couldn’t remove example-repo/pr-7',
        detail: 'git refused: has uncommitted changes',
        tone: 'error',
      })
    })

    platform.removeAllCleanWorktrees.mockRejectedValueOnce(new Error('scan the clone root: permission denied'))
    await userEvent.click(await screen.findByRole('button', { name: 'Remove all clean (1)' }))
    await confirmWith('Remove 1 worktree')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Couldn’t remove the clean worktrees',
        detail: 'scan the clone root: permission denied',
        tone: 'error',
      })
    })

    platform.mergedBranches.mockResolvedValueOnce([mergedBranch()])
    platform.deleteBranches.mockRejectedValueOnce(new Error('offline'))
    await userEvent.click(await screen.findByRole('button', { name: 'Clean up merged branches…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 branch' }))
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Couldn’t delete the branches',
        detail: 'offline',
        tone: 'error',
      })
    })
  })

  it('names a clean-up that failed, and reports a bulk action and a deletion of branches', async () => {
    const onSummary = vi.fn()
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [worktreeRepo({ worktrees: [mainWorktree(), finishedWorktree()] })],
        finishedCount: 1,
      }),
    )
    platform.cleanUpWorktree.mockRejectedValueOnce(new Error('the worktree is not finished'))
    platform.cleanUpFinishedWorktrees.mockResolvedValueOnce([removeResult()])
    platform.mergedBranches.mockResolvedValueOnce([mergedBranch()])
    platform.deleteBranches.mockResolvedValueOnce([
      {
        repoPath,
        displayRepo: '~/projects/example-repo',
        name: 'feature-x',
        head: 'fc1c3975011e66491785ace891c893e4fc716587',
        deleted: true,
        reason: '',
      },
    ])
    render(<WorktreesView clock={fixedNow} onSummary={onSummary} />)
    await repo('example-repo')

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Clean up' }))
    await confirmWith('Clean up')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith(
        expect.objectContaining({ title: 'Couldn’t clean up example-repo/pr-7', tone: 'error' }),
      )
    })

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (1)' }))
    await confirmWith('Clean up 1 worktree')
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Cleaned up 1 finished worktree',
        detail: '1 branch deleted.',
        tone: 'success',
      })
    })

    await userEvent.click(await screen.findByRole('button', { name: 'Clean up merged branches…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Delete 1 branch' }))
    await vi.waitFor(() => {
      expect(onSummary).toHaveBeenLastCalledWith({
        title: 'Deleted 1 branch',
        detail: 'Across 1 clone.',
        tone: 'success',
      })
    })
  })
})

describe('WorktreesView filter', () => {
  // listing is example-repo with its main worktree, pr-7, and a dirty worktree, and
  // other-tool with its main worktree; the history holds one cleanup.
  function listing() {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              worktree(),
              worktree({
                path: `${repoPath}/.claude/worktrees/scratch`,
                displayPath: '~/projects/example-repo/.claude/worktrees/scratch',
                branch: 'scratch',
                dirty: true,
                dirtyCount: 1,
                canRemove: false,
                canForceRemove: true,
              }),
            ],
          }),
          worktreeRepo({
            path: '/home/user/projects/other-tool',
            displayPath: '~/projects/other-tool',
            name: 'other-tool',
            worktrees: [mainWorktree({ path: '/home/user/projects/other-tool', displayPath: '~/projects/other-tool' })],
          }),
        ],
      }),
    )
    platform.recentCleanups.mockResolvedValue([worktreeCleanup()])
  }

  it('shows only the clones and worktrees that match, counting what each clone shows', async () => {
    listing()
    const { rerender } = render(<WorktreesView clock={fixedNow} query="is:dirty" />)

    const example = await repo('example-repo')
    expect(example.getByText('1 of 3 worktrees')).toBeInTheDocument()
    expect(example.getByText('~/projects/example-repo/.claude/worktrees/scratch')).toBeInTheDocument()
    expect(example.queryByText(linkedDisplay)).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'other-tool' })).not.toBeInTheDocument()
    expect(screen.queryByText(/^No worktree matches/)).not.toBeInTheDocument()

    // The bulk actions and the cleanup history still cover everything.
    expect(screen.getByRole('button', { name: 'Remove all clean (1)' })).toBeEnabled()
    expect(await screen.findByRole('region', { name: 'Recently cleaned up' })).toBeInTheDocument()

    // A query that names a clone shows all of it.
    rerender(<WorktreesView clock={fixedNow} query="other-tool" />)
    expect((await repo('other-tool')).getByText('1 worktree')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'example-repo' })).not.toBeInTheDocument()

    // Clearing the query shows everything again.
    rerender(<WorktreesView clock={fixedNow} query="" />)
    expect((await repo('example-repo')).getByText('3 worktrees')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'other-tool' })).toBeInTheDocument()
  })

  it('says at the top when no worktree matches, keeping a worktree that shows an error', async () => {
    platform.pruneWorktrees.mockRejectedValue(new Error('prune: exit status 128'))
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            hasStale: true,
            worktrees: [mainWorktree(), worktree({ canRemove: false, error: 'could not read the status: exit 128' })],
          }),
        ],
      }),
    )
    const { rerender } = render(<WorktreesView clock={fixedNow} />)

    // A prune fails, so its clone shows an error too.
    await userEvent.click((await repo('example-repo')).getByRole('button', { name: 'Prune' }))
    expect(await (await repo('example-repo')).findByRole('alert')).toHaveTextContent('prune: exit status 128')

    rerender(<WorktreesView clock={fixedNow} query="  nothing-here " />)

    const message = screen.getByText('No worktree matches “nothing-here”.')
    const example = within(screen.getByRole('region', { name: 'example-repo' }))
    expect(example.getByText('1 of 2 worktrees')).toBeInTheDocument()
    expect(example.getByText('could not read the status: exit 128')).toBeInTheDocument()
    expect(example.queryByText('main worktree')).not.toBeInTheDocument()
    // The message comes before the clones.
    expect(message.compareDocumentPosition(screen.getByRole('region', { name: 'example-repo' }))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )
  })

  it('says nothing of a filter while no clone has worktrees', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [], cleanCount: 0 }))
    render(<WorktreesView clock={fixedNow} query="anything" />)

    expect(await screen.findByText('No clone under ~/projects has linked worktrees.')).toBeInTheDocument()
    expect(screen.queryByText(/^No worktree matches/)).not.toBeInTheDocument()
  })
})
