import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { fixedNow } from '../test/clock'
import {
  finishedWorktree,
  linkedItem,
  mainWorktree,
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
                claudeCreated: false,
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
    expect(main.queryByText('created by Claude')).not.toBeInTheDocument()

    const linked = row(linkedDisplay)
    expect(linked.getByText('worktree-pr-7')).toBeInTheDocument()
    expect(linked.getByText('clean')).toBeInTheDocument()
    expect(linked.getByText('created by Claude')).toBeInTheDocument()
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
    expect(running.queryByRole('button')).not.toBeInTheDocument()

    const byHand = row('~/projects/example-repo/.claude/worktrees/issue-9')
    expect(byHand.getByText(/^Locked\. It was locked by hand/)).toBeInTheDocument()
    expect(
      byHand.getByText('git worktree unlock ~/projects/example-repo/.claude/worktrees/issue-9'),
    ).toBeInTheDocument()
    expect(byHand.queryByRole('button')).not.toBeInTheDocument()
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
    expect(await screen.findByRole('status')).toHaveTextContent(`Unlocked ${linkedDisplay}.`)
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
      'Removes the worktree and its branch worktree-pr-7; every commit is on GitHub',
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

  it('cleans up a finished worktree without asking and says what it did', async () => {
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
      deletedBranch: 'worktree-pr-7',
      item: 'acme-org/example-repo#7',
      reason: '',
    })
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    const finished = row(linkedDisplay)
    expect(finished.getByText(/Clean up releases the lock first\./)).toBeInTheDocument()
    expect(finished.queryByRole('button', { name: 'Unlock' })).not.toBeInTheDocument()

    await userEvent.click(finished.getByRole('button', { name: 'Clean up' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.cleanUpWorktree).toHaveBeenCalledWith(repoPath, linkedPath)
    expect(await screen.findByRole('status')).toHaveTextContent(
      `Cleaned up ${linkedDisplay} and deleted its branch worktree-pr-7.`,
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('says when a cleaned-up worktree kept its branch, and shows a refusal', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({ worktrees: [mainWorktree(), finishedWorktree({ ownBranch: false, branch: 'feat/x' })] }),
        ],
      }),
    )
    const result = {
      repoPath,
      path: linkedPath,
      displayPath: linkedDisplay,
      removed: true,
      deletedBranch: '',
      item: 'acme-org/example-repo#7',
      reason: '',
    }
    platform.cleanUpWorktree.mockResolvedValueOnce(result)
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    expect(row(linkedDisplay).getByRole('button', { name: 'Clean up' })).toHaveAttribute(
      'title',
      'Removes the worktree; every commit is on GitHub, and its branch is kept',
    )
    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Clean up' }))
    expect(await screen.findByRole('status')).toHaveTextContent(`Cleaned up ${linkedDisplay}.`)

    platform.cleanUpWorktree.mockResolvedValueOnce({
      ...result,
      reason: 'kept the branch worktree-pr-7: git: cannot lock ref',
    })
    await userEvent.click(await row(linkedDisplay).findByRole('button', { name: 'Clean up' }))
    expect(await screen.findByRole('status')).toHaveTextContent(
      `Cleaned up ${linkedDisplay}; kept the branch worktree-pr-7: git: cannot lock ref.`,
    )

    platform.cleanUpWorktree.mockRejectedValueOnce(
      new Error('the worktree is not finished: it has uncommitted changes'),
    )
    await userEvent.click(await row(linkedDisplay).findByRole('button', { name: 'Clean up' }))
    expect(await row(linkedDisplay).findByRole('alert')).toHaveTextContent(
      'the worktree is not finished: it has uncommitted changes',
    )
  })

  it('confirms clean up finished with the count and reports each outcome', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ finishedCount: 2 }))
    platform.cleanUpFinishedWorktrees.mockResolvedValue([
      {
        repoPath,
        path: linkedPath,
        displayPath: linkedDisplay,
        removed: true,
        deletedBranch: 'worktree-pr-7',
        item: 'acme-org/example-repo#7',
        reason: '',
      },
      {
        repoPath,
        path: `${repoPath}/.claude/worktrees/pr-8`,
        displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
        removed: false,
        deletedBranch: '',
        item: 'acme-org/example-repo#8',
        reason: 'the worktree changed since it was checked; refresh and try again',
      },
    ])
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (2)' }))

    const dialog = screen.getByRole('dialog', { name: 'Clean up finished worktrees?' })
    expect(dialog).toHaveTextContent('This removes 2 worktrees whose pull request merged or issue closed.')
    expect(dialog).toHaveTextContent('own worktree-… branch is deleted with it')

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(platform.cleanUpFinishedWorktrees).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (2)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 2 worktrees' }))

    expect(platform.cleanUpFinishedWorktrees).toHaveBeenCalledOnce()
    const results = await screen.findByRole('status')
    expect(results).toHaveTextContent('Cleaned up 1 of 2 finished worktrees.')
    expect(results).toHaveTextContent(
      '~/projects/example-repo/.claude/worktrees/pr-8: the worktree changed since it was checked; refresh and try again',
    )
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('reports a clean up finished that had nothing to do or failed', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ finishedCount: 1 }))
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Clean up finished (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clean up 1 worktree' }))
    expect(await screen.findByRole('status')).toHaveTextContent('There were no finished worktrees to remove.')

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

  it('removes a clean worktree without asking, then lists again', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree()] })], cleanCount: 0 }),
    )

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Remove' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.removeWorktree).toHaveBeenCalledWith(repoPath, linkedPath)
    expect(await screen.findByRole('status')).toHaveTextContent(`Removed ${linkedDisplay}.`)
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

    expect(await row(linkedDisplay).findByRole('alert')).toHaveTextContent(
      `git refused to remove ${linkedDisplay}: has uncommitted changes`,
    )
    expect(platform.listWorktrees).toHaveBeenCalledOnce()
    expect(row(linkedDisplay).getByRole('button', { name: 'Remove' })).toBeEnabled()
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
    expect(dialog).toHaveTextContent('Its branch is kept.')

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.forceRemoveWorktree).not.toHaveBeenCalled()

    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Force remove' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Force remove' }))

    expect(platform.forceRemoveWorktree).toHaveBeenCalledWith(repoPath, linkedPath)
    expect(platform.removeWorktree).not.toHaveBeenCalled()
    expect(await screen.findByRole('status')).toHaveTextContent(`Force removed ${linkedDisplay}.`)
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('confirms remove all clean with the count', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ cleanCount: 2 }))
    platform.removeAllCleanWorktrees.mockResolvedValue([
      {
        repoPath,
        path: linkedPath,
        displayPath: linkedDisplay,
        removed: true,
        deletedBranch: '',
        item: '',
        reason: '',
      },
      {
        repoPath,
        path: `${repoPath}/.claude/worktrees/issue-9`,
        displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
        removed: false,
        deletedBranch: '',
        item: '',
        reason: 'has uncommitted changes',
      },
    ])
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (2)' }))

    const dialog = screen.getByRole('dialog', { name: 'Remove all clean worktrees?' })
    expect(dialog).toHaveTextContent('This removes 2 clean, unlocked worktrees. Their branches are kept.')

    await userEvent.keyboard('{Escape}')
    expect(platform.removeAllCleanWorktrees).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (2)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove 2 worktrees' }))

    expect(platform.removeAllCleanWorktrees).toHaveBeenCalledOnce()
    const results = await screen.findByRole('status')
    expect(results).toHaveTextContent('Removed 1 of 2 clean worktrees.')
    expect(results).toHaveTextContent('~/projects/example-repo/.claude/worktrees/issue-9: has uncommitted changes')
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('reports a remove all clean that had nothing to do or failed', async () => {
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    expect(await screen.findByRole('status')).toHaveTextContent('There were no clean worktrees to remove.')

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
        deletedBranch: '',
        item: '',
        reason: '',
      },
    ])
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    await userEvent.click(screen.getByRole('button', { name: 'Remove all clean (1)' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove 1 worktree' }))

    const results = await screen.findByRole('status')
    expect(results).toHaveTextContent('Removed 1 of 1 clean worktree.')
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
    expect(await screen.findByRole('status')).toHaveTextContent('Pruned the stale entries of ~/projects/other-repo.')
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
      deletedBranch: 'worktree-pr-7',
      item: 'acme-org/example-repo#7',
      reason: '',
    })
    render(<WorktreesView clock={fixedNow} />)
    await repo('example-repo')

    platform.recentCleanups.mockResolvedValue([worktreeCleanup({ automatic: false })])
    await userEvent.click(row(linkedDisplay).getByRole('button', { name: 'Clean up' }))

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
