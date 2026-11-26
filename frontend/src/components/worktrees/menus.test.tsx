import { describe, expect, it, vi } from 'vitest'

import type { Worktree, WorktreeRepo } from '../../platform'
import {
  agentName,
  agentSession,
  finishedWorktree,
  linkedItem,
  worktree,
  worktreeDetails,
  worktreeFiles,
  worktreeRepo,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import type { MenuItem } from '../Menu'
import { cleanupMenuItems, goneWhy, rowMenuItems, type CleanupMenuOptions, type RowMenuOptions } from './menus'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const er = worktreeRepo()

// options are a row menu's, for wt, with every handler a mock.
function options(wt: Worktree, overrides: Partial<RowMenuOptions> = {}): RowMenuOptions {
  return {
    repo: er,
    worktree: wt,
    read: undefined,
    session: undefined,
    disabled: false,
    agentName,
    onTerminal: vi.fn(),
    onReveal: vi.fn(),
    onShowTerminal: vi.fn(),
    onCleanUp: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onUnlock: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onRemove: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onForceRemove: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onPrune: vi.fn<(repo: WorktreeRepo) => void>(),
    ...overrides,
  }
}

// shown is each item's label, with why it can't run when it can't, as the menu shows it.
function shown(items: readonly MenuItem[]): string[] {
  return items.map((i) =>
    i.disabled === true ? `${i.label} (off${i.note === undefined ? '' : `: ${i.note}`})` : i.label,
  )
}

// pick is the menu's item called label.
function pick(items: readonly MenuItem[], label: string): MenuItem {
  const found = items.find((i) => i.label === label)

  if (found === undefined) {
    throw new Error(`no item ${label}`)
  }

  return found
}

const open = linkedItem({ state: 'open', closedAt: '' })

describe('rowMenuItems', () => {
  it('offers a clean worktree’s every action, Remove… last', () => {
    expect(shown(rowMenuItems(options(worktree({ item: open }))))).toEqual([
      'Open in terminal',
      'Reveal in Finder',
      'Copy path',
      'Copy branch',
      'Open PR #7 on GitHub',
      'Remove…',
    ])
  })

  it('offers Show terminal first while a session it can show works there', () => {
    const o = options(worktree({ item: open }), { session: agentSession({ state: 'working' }) })
    const items = rowMenuItems(o)

    expect(shown(items).slice(0, 2)).toEqual(['Show terminal', 'Open in terminal'])
    expect(pick(items, 'Show terminal').title).toBe('Bring forward the iTerm2 tab Claude runs in')

    pick(items, 'Show terminal').onSelect?.()
    expect(o.onShowTerminal).toHaveBeenCalledOnce()
    expect(o.onTerminal).not.toHaveBeenCalled()

    pick(items, 'Open in terminal').onSelect?.()
    expect(o.onTerminal).toHaveBeenCalledOnce()
  })

  it.each([
    ['a session still starting', agentSession({ state: 'starting', canFocus: false })],
    ['an agent’s run in the background', agentSession({ state: 'working', canFocus: false, runId: 4 })],
    ['a session that ended', agentSession({ state: 'ended', endedAt: '2026-06-15T11:58:00Z' })],
  ])('offers no Show terminal for %s', (_label, session) => {
    expect(shown(rowMenuItems(options(worktree(), { session })))).not.toContain('Show terminal')
  })

  it('sets the removals apart', () => {
    const items = rowMenuItems(options(worktree()))

    expect(pick(items, 'Remove…').divided).toBe(true)
    expect(items.filter((i) => i.divided === true)).toHaveLength(1)
  })

  it('offers Clean up… first for a finished one, the removals after it', () => {
    const items = rowMenuItems(options(finishedWorktree()))

    expect(shown(items).slice(-2)).toEqual(['Clean up…', 'Remove…'])
    expect(pick(items, 'Clean up…').divided).toBe(true)
    expect(pick(items, 'Remove…').divided).toBe(false)
  })

  it('offers Force remove… last, in red, for one with uncommitted files, saying why Remove… can’t run', () => {
    const items = rowMenuItems(
      options(worktree({ dirty: true, dirtyCount: 3, canRemove: false, canForceRemove: true })),
    )

    expect(shown(items).slice(-2)).toEqual([
      'Remove… (off: It has uncommitted files: Force remove… discards them)',
      'Force remove…',
    ])
    expect(pick(items, 'Force remove…').danger).toBe(true)
    expect(pick(items, 'Remove…').title).toBe('It has uncommitted files: Force remove… discards them')
  })

  it('offers Remove…, and no Force remove…, for one whose files are only missing from its folder', () => {
    const items = rowMenuItems(options(worktree({ item: open, dirty: true, dirtyCount: 3, missingCount: 3 })))

    expect(shown(items).at(-1)).toBe('Remove…')
    expect(shown(items)).not.toContain('Force remove…')
  })

  it('counts the removals as the details read them, holding back what the listing allowed', () => {
    const read = { state: 'read', details: worktreeDetails({ files: worktreeFiles(3), fileCount: 3 }) } as const
    const items = rowMenuItems(options(worktree(), { read }))

    expect(shown(items).slice(-2)).toEqual([
      'Remove… (off: It has uncommitted files: Force remove… discards them)',
      'Force remove… (off: It can’t be force-removed right now: Check again)',
    ])
  })

  it('says why neither removal runs while something runs in it', () => {
    const wt = worktree({
      dirty: true,
      dirtyCount: 3,
      inUse: [{ pid: 60311, command: 'codex', agent: 'codex', by: '', args: '', detached: false }],
      stage: 'in-use',
      canRemove: false,
    })

    expect(shown(rowMenuItems(options(wt))).slice(-2)).toEqual([
      'Remove… (off: Codex runs in it)',
      'Force remove… (off: Codex runs in it)',
    ])
  })

  it('offers Unlock for a lock an ended session left', () => {
    const items = rowMenuItems(
      options(worktree({ locked: true, agentLock: true, staleLock: true, canUnlock: true, canRemove: false })),
    )

    expect(pick(items, 'Unlock').note).toBe('Releases the lock of the session that ended here')
    expect(pick(items, 'Remove…').note).toBe('A Claude session that has ended still locks it: Unlock releases it')
  })

  it('offers Prune for a folder that’s gone, and no Terminal, Finder, or removal', () => {
    const items = rowMenuItems(options(worktree({ prunable: true, canRemove: false })))

    expect(shown(items)).toEqual([
      'Open in terminal (off: Its folder is gone)',
      'Reveal in Finder (off: Its folder is gone)',
      'Copy path',
      'Copy branch',
      'Prune example-repo',
    ])
    expect(pick(items, 'Prune example-repo').title).toBe(
      'Clears git’s entries for worktrees whose folders are gone; touches no worktree that exists, and no branch',
    )
  })

  it('offers no Terminal or Finder for one git couldn’t read', () => {
    const items = rowMenuItems(options(worktree({ error: 'git status failed', canRemove: false })))

    expect(shown(items).slice(0, 2)).toEqual([
      'Open in terminal (off: git couldn’t read it)',
      'Reveal in Finder (off: git couldn’t read it)',
    ])
    expect(pick(items, 'Open in terminal').title).toBe('git couldn’t read it')
  })

  it('offers no Copy branch for a detached worktree', () => {
    expect(shown(rowMenuItems(options(worktree({ detached: true }))))).not.toContain('Copy branch')
    expect(shown(rowMenuItems(options(worktree({ branch: '' }))))).not.toContain('Copy branch')
  })

  it('holds every change while one runs, but still opens and copies', () => {
    const items = rowMenuItems(
      options(
        worktree({
          item: finishedWorktree().item,
          canCleanUp: true,
          canUnlock: true,
          dirty: true,
          dirtyCount: 1,
          canForceRemove: true,
        }),
        { disabled: true },
      ),
    )

    expect(items.filter((i) => i.disabled === true).map((i) => i.label)).toEqual([
      'Unlock',
      'Clean up…',
      'Remove…',
      'Force remove…',
    ])
  })

  it('holds Prune while a change runs', () => {
    expect(
      pick(rowMenuItems(options(worktree({ prunable: true }), { disabled: true })), 'Prune example-repo').disabled,
    ).toBe(true)
  })

  it('runs each item’s action', () => {
    const wt = worktree({
      item: open,
      canUnlock: true,
      canCleanUp: true,
      dirty: true,
      dirtyCount: 1,
      canForceRemove: true,
    })
    const o = options(wt)
    const items = rowMenuItems(o)

    for (const label of [
      'Open in terminal',
      'Reveal in Finder',
      'Open PR #7 on GitHub',
      'Unlock',
      'Clean up…',
      'Force remove…',
    ]) {
      pick(items, label).onSelect?.()
    }

    expect(o.onTerminal).toHaveBeenCalledOnce()
    expect(o.onReveal).toHaveBeenCalledOnce()
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/7')
    expect(o.onUnlock).toHaveBeenCalledExactlyOnceWith(er, wt)
    expect(o.onCleanUp).toHaveBeenCalledExactlyOnceWith(er, wt)
    expect(o.onForceRemove).toHaveBeenCalledExactlyOnceWith(er, wt)
  })

  it('removes and prunes from their items', () => {
    const wt = worktree()
    const o = options(wt)
    pick(rowMenuItems(o), 'Remove…').onSelect?.()
    expect(o.onRemove).toHaveBeenCalledExactlyOnceWith(er, wt)

    const gone = options(worktree({ prunable: true }))
    pick(rowMenuItems(gone), 'Prune example-repo').onSelect?.()
    expect(gone.onPrune).toHaveBeenCalledExactlyOnceWith(er)
  })

  it('copies the path and the branch', async () => {
    const wt = worktree()
    const items = rowMenuItems(options(wt))

    pick(items, 'Copy path').onSelect?.()
    pick(items, 'Copy branch').onSelect?.()

    await vi.waitFor(() => {
      expect(platform.copyText.mock.calls).toEqual([[wt.path], ['worktree-pr-7']])
    })
  })

  it('logs a copy the clipboard refuses', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.copyText.mockRejectedValueOnce(new Error('denied'))

    pick(rowMenuItems(options(worktree())), 'Copy path').onSelect?.()

    await vi.waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not copy the worktree path', expect.any(Error))
    })
  })
})

describe('goneWhy', () => {
  it('says why a worktree can’t be opened', () => {
    expect(goneWhy(worktree())).toBe('')
    expect(goneWhy(worktree({ prunable: true, error: 'gone' }))).toBe('Its folder is gone')
    expect(goneWhy(worktree({ error: 'git status failed' }))).toBe('git couldn’t read it')
  })
})

describe('cleanupMenuItems', () => {
  // menu is the cleanup ⌄'s items, with every handler a mock.
  function menu(overrides: Partial<CleanupMenuOptions> = {}) {
    const o: CleanupMenuOptions = {
      finishedCount: 0,
      cleanCount: 0,
      disabled: false,
      onRemoveAllClean: vi.fn(),
      onCleanUpMerged: vi.fn(),
      ...overrides,
    }

    return { o, items: cleanupMenuItems(o) }
  }

  it('says why each cleanup has nothing to take', () => {
    expect(shown(menu().items)).toEqual([
      'Clean up finished… (off: None is finished: no worktree whose pull request merged or issue closed has all its work on GitHub)',
      'Remove clean worktrees… (off: None is clean and unlocked)',
      'Clean up merged branches…',
    ])
  })

  it('counts the clean worktrees it would remove, leaving Clean up finished to its own button', () => {
    const { items } = menu({ finishedCount: 2, cleanCount: 3 })

    expect(shown(items)).toEqual(['Remove 3 clean worktrees…', 'Clean up merged branches…'])
    expect(pick(items, 'Remove 3 clean worktrees…').note).toBe(
      'Each is unlocked and holds nothing uncommitted to lose; you choose which, and their branches',
    )
    expect(pick(items, 'Clean up merged branches…').note).toBe(
      'The local branches whose pull requests merged, in every clone',
    )
    expect(shown(menu({ cleanCount: 1 }).items)).toContain('Remove 1 clean worktree…')
  })

  it('asks before each cleanup it offers', () => {
    const { o, items } = menu({ cleanCount: 1 })

    pick(items, 'Remove 1 clean worktree…').onSelect?.()
    pick(items, 'Clean up merged branches…').onSelect?.()

    expect(o.onRemoveAllClean).toHaveBeenCalledOnce()
    expect(o.onCleanUpMerged).toHaveBeenCalledOnce()
  })

  it('holds each cleanup while a change runs', () => {
    const { items } = menu({ cleanCount: 1, disabled: true })

    expect(items.every((i) => i.disabled === true)).toBe(true)
  })
})
