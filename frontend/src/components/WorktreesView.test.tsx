import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { App } from '../App'
import { mainWorktree, profile, settings, snapshot, worktree, worktreeRepo, worktreesSnapshot } from '../test/fixtures'
import { WorktreesView } from './WorktreesView'

const platform = vi.hoisted(() => ({
  getSettings: vi.fn(),
  listProfiles: vi.fn(),
  setTerminal: vi.fn(),
  setCloneRoot: vi.fn(),
  getInboxSnapshot: vi.fn(),
  refreshInbox: vi.fn(),
  onInboxUpdated: vi.fn(),
  openInBrowser: vi.fn(),
  launch: vi.fn(),
  listWorktrees: vi.fn(),
  removeWorktree: vi.fn(),
  forceRemoveWorktree: vi.fn(),
  removeAllCleanWorktrees: vi.fn(),
  pruneWorktrees: vi.fn(),
}))

vi.mock('../platform', () => platform)

const now = new Date('2026-06-15T12:00:00Z')
const clock = () => now

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

beforeEach(() => {
  vi.clearAllMocks()
  platform.getSettings.mockResolvedValue(settings())
  platform.listProfiles.mockResolvedValue([profile()])
  platform.getInboxSnapshot.mockResolvedValue(snapshot())
  platform.onInboxUpdated.mockReturnValue(() => undefined)
  platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
  platform.removeWorktree.mockResolvedValue(undefined)
  platform.forceRemoveWorktree.mockResolvedValue(undefined)
  platform.removeAllCleanWorktrees.mockResolvedValue([])
  platform.pruneWorktrees.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.useRealTimers()
})

describe('WorktreesView', () => {
  it('loads only when opened', async () => {
    render(<App />)
    await screen.findByRole('region', { name: 'Review requested' })

    expect(platform.listWorktrees).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    // Leaving the view unmounts it; opening it again lists afresh.
    await userEvent.click(screen.getByRole('button', { name: 'Inbox' }))
    await screen.findByRole('region', { name: 'Review requested' })
    expect(platform.listWorktrees).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))
    await screen.findByRole('region', { name: 'example-repo' })
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('lists again only when Refresh is clicked, never on a timer', async () => {
    render(<WorktreesView clock={clock} />)
    await screen.findByRole('region', { name: 'example-repo' })

    vi.useFakeTimers()
    act(() => {
      vi.advanceTimersByTime(30 * 60_000)
    })
    vi.useRealTimers()

    expect(platform.listWorktrees).toHaveBeenCalledOnce()
    expect(platform.onInboxUpdated).not.toHaveBeenCalled()

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
            worktrees: null,
          }),
        ],
      }),
    )
    render(<WorktreesView clock={clock} />)

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
    render(<WorktreesView clock={clock} />)
    await repo('example-repo')

    const linked = row(linkedDisplay)
    expect(linked.getByText('could not read the status: exit 128')).toBeInTheDocument()
    expect(linked.queryByText('clean')).not.toBeInTheDocument()
  })

  it('main worktree has no remove button', async () => {
    render(<WorktreesView clock={clock} />)
    await repo('example-repo')

    const main = row('~/projects/example-repo')
    expect(main.getByText('main worktree')).toBeInTheDocument()
    expect(main.queryByRole('button')).not.toBeInTheDocument()
  })

  it('locked row has no removal and shows the unlock hint', async () => {
    platform.listWorktrees.mockResolvedValue(
      worktreesSnapshot({
        repos: [
          worktreeRepo({
            worktrees: [
              mainWorktree(),
              worktree({
                locked: true,
                lockReason: 'claude session pr-7 (pid 4242)',
                staleLock: true,
                dirty: true,
                dirtyCount: 2,
                canRemove: false,
                canForceRemove: false,
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
    render(<WorktreesView clock={clock} />)
    await repo('example-repo')

    const live = row(linkedDisplay)
    expect(live.getByText('locked')).toBeInTheDocument()
    expect(live.getByText(/Locked: claude session pr-7 \(pid 4242\)\./)).toBeInTheDocument()
    expect(live.getByText(`git worktree unlock ${linkedDisplay}`)).toBeInTheDocument()
    expect(live.getByText('The session appears to have ended (stale lock).')).toBeInTheDocument()
    expect(live.queryByRole('button')).not.toBeInTheDocument()

    const quiet = row('~/projects/example-repo/.claude/worktrees/issue-9')
    expect(quiet.getByText(/^Locked\. A locked worktree/)).toBeInTheDocument()
    expect(quiet.queryByText(/stale lock/)).not.toBeInTheDocument()
    expect(quiet.queryByRole('button')).not.toBeInTheDocument()
  })

  it('removes a clean worktree without asking, then lists again', async () => {
    render(<WorktreesView clock={clock} />)
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
    render(<WorktreesView clock={clock} />)
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
    render(<WorktreesView clock={clock} />)
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
      { repoPath, path: linkedPath, displayPath: linkedDisplay, removed: true, reason: '' },
      {
        repoPath,
        path: `${repoPath}/.claude/worktrees/issue-9`,
        displayPath: '~/projects/example-repo/.claude/worktrees/issue-9',
        removed: false,
        reason: 'has uncommitted changes',
      },
    ])
    render(<WorktreesView clock={clock} />)
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
    render(<WorktreesView clock={clock} />)
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
      { repoPath, path: linkedPath, displayPath: linkedDisplay, removed: true, reason: '' },
    ])
    render(<WorktreesView clock={clock} />)
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
    render(<WorktreesView clock={clock} />)

    expect((await repo('example-repo')).queryByRole('button', { name: 'Prune' })).not.toBeInTheDocument()

    await userEvent.click((await repo('other-repo')).getByRole('button', { name: 'Prune' }))

    expect(platform.pruneWorktrees).toHaveBeenCalledWith('/home/user/projects/other-repo')
    expect(await screen.findByRole('status')).toHaveTextContent('Pruned the stale entries of ~/projects/other-repo.')
    expect(platform.listWorktrees).toHaveBeenCalledTimes(2)
  })

  it('shows a prune failure on its repository', async () => {
    platform.listWorktrees.mockResolvedValue(worktreesSnapshot({ repos: [worktreeRepo({ hasStale: true })] }))
    platform.pruneWorktrees.mockRejectedValue(new Error('prune ~/projects/example-repo: exit status 128'))
    render(<WorktreesView clock={clock} />)

    await userEvent.click((await repo('example-repo')).getByRole('button', { name: 'Prune' }))

    expect(await (await repo('example-repo')).findByRole('alert')).toHaveTextContent(
      'prune ~/projects/example-repo: exit status 128',
    )
    expect(row('~/projects/example-repo').queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a listing failure and recovers on Refresh', async () => {
    platform.listWorktrees.mockRejectedValue(new Error('scan the clone root: no such directory'))
    render(<WorktreesView clock={clock} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('scan the clone root: no such directory')
    expect(screen.queryByText('Listing the worktrees…')).not.toBeInTheDocument()

    platform.listWorktrees.mockResolvedValue(worktreesSnapshot())
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    expect(await screen.findByRole('region', { name: 'example-repo' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says it is listing until the first listing arrives', async () => {
    let finish: (snap: ReturnType<typeof worktreesSnapshot>) => void = () => undefined
    platform.listWorktrees.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    render(<WorktreesView clock={clock} />)

    expect(screen.getByText('Listing the worktrees…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()

    await act(async () => {
      finish(worktreesSnapshot({ repos: null, cleanCount: 0 }))
      await Promise.resolve()
    })

    expect(await screen.findByText('No clone under ~/projects has linked worktrees.')).toBeInTheDocument()
    expect(screen.getByText('Worktrees of the clones under ~/projects')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
  })
})
