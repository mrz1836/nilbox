import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import type { LinkedItem, Worktree } from '../platform'
import { FIXED_NOW, isoAgo } from '../test/clock'
import {
  agentName,
  agentSession,
  copiedPR,
  copiedWorktree,
  linkedItem,
  mainWorktree,
  managed,
  partlyCopiedWorktree,
  recordedWorktree,
  worktree,
  worktreeCopies,
  worktreeRepo,
} from '../test/fixtures'
import {
  attentionSort,
  cloneSummary,
  edgeTitle,
  laneOf,
  lanes,
  removeWhy,
  safety,
  stateOf,
  verbOf,
  wordsText,
  workNotes,
  type Safety,
} from './worktrees'

// The rows the tests read: one of each state the page's gallery shows, at the tests' clock.

const now = FIXED_NOW

const hour = 60
const day = 24 * hour

// open is pull request #7, still open.
function open(overrides: Partial<LinkedItem> = {}): LinkedItem {
  return linkedItem({ state: 'open', closedAt: '', ...overrides })
}

// row is a worktree the app made for pull request #7, open, clean, and at its head.
function row(overrides: Partial<Worktree> = {}): Worktree {
  return worktree({ legacy: false, displayPath: managed('pr-7'), stage: 'active', item: open(), ...overrides })
}

const claudeHere = { pid: 47890, command: '2.1.328', agent: 'claude', by: '' } as const
const codexHere = { pid: 60311, command: 'codex', agent: 'codex', by: '' } as const
const routineHere = { pid: 61022, command: '2.1.328', agent: 'claude', by: '⚡ Fix checks' } as const

const runningClaude = row({
  branch: 'feat/inspect-point',
  inUse: [claudeHere],
  stage: 'in-use',
  dirty: true,
  dirtyCount: 30,
  unpushed: 1,
  onGitHub: false,
  canRemove: false,
})
const runningCodex = row({ inUse: [codexHere], stage: 'in-use', canRemove: false })
const routine = row({ inUse: [routineHere], stage: 'in-use', canRemove: false })
const liveLock = row({
  locked: true,
  agentLock: true,
  lockReason: 'claude session pr-252 (pid 70113 start Mon Jun 15 11:30:03 2026)',
  stage: 'in-use',
  dirty: true,
  dirtyCount: 1,
  canRemove: false,
})
const ready = row({ item: linkedItem(), stage: 'finished', canCleanUp: true })
const reviewCommits = row({
  branch: 'worktree-pr-131',
  item: linkedItem({ number: 131, closedAt: isoAgo({ m: 6 * day }) }),
  stage: 'needs-review',
  unpushed: 2,
  onGitHub: false,
})
const reviewDirty = row({
  item: linkedItem({ number: 150, closedAt: isoAgo({ m: 2 * day }) }),
  stage: 'needs-review',
  dirty: true,
  dirtyCount: 3,
  canRemove: false,
  canForceRemove: true,
})
const openClean = row()
const both = row({
  branch: 'worktree-pr-221',
  dirty: true,
  dirtyCount: 7,
  unpushed: 2,
  onGitHub: false,
  canRemove: false,
  canForceRemove: true,
})
const upOnly = row({ unpushed: 3, onGitHub: false })
const unknown = row({ unpushed: -1, onGitHub: false })
const staleLock = row({
  locked: true,
  agentLock: true,
  staleLock: true,
  canUnlock: true,
  lockReason: 'claude session pr-253 (pid 66120 start Mon Jun 15 08:14:09 2026)',
  canRemove: false,
})
const handLocked = row({
  displayPath: managed('pr-254'),
  locked: true,
  lockReason: 'kept for the release branch',
  stage: '',
  canRemove: false,
})
const prunable = row({
  prunable: true,
  prunableReason: 'gitdir file points to non-existent location',
  unpushed: -1,
  onGitHub: false,
  stage: '',
  canRemove: false,
})
const erred = row({
  error: 'git status refused: this clone’s own git config sets core.fsmonitor, which isn’t run',
  unpushed: -1,
  onGitHub: false,
  canRemove: false,
})
const detached = row({
  displayPath: '~/projects/dotfiles-experiment',
  branch: '',
  detached: true,
  item: null,
  stage: '',
  managed: false,
})

// The copies of a sub-agent's commits: copied is on GitHub as identical copies on pull
// request #42's branch, open; recorded once #42 merged and its branch was deleted; partly
// has only an edited copy there, so it isn't on GitHub.
const copied = copiedWorktree()
const recorded = recordedWorktree()
const partly = partlyCopiedWorktree()

// unlinked is copied onto a branch no pull request is known to head, linked to nothing:
// they're named by the branch they're on.
const unlinked = copiedWorktree({
  item: null,
  linkedBy: '',
  copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10 }),
})

// elsewhere is recorded, linked by its name to pull request #7 rather than by its copies:
// they're named by the pull request they went into, #42, whose state it isn't told.
const elsewhere = recordedWorktree({ item: linkedItem({ headOid: '' }), linkedBy: 'name' })

// held is recorded while #42's branch is still there, merged, but force-pushed since
// without the copies: the record vouches for them, the branch no longer holds them all.
const held = recordedWorktree({
  copies: worktreeCopies({
    ref: 'origin/feature-x',
    branch: 'feature-x',
    count: 10,
    identical: 10,
    recorded: true,
    pr: 42,
  }),
})

// ahead is partly, with n commits not on GitHub that compare with origin/feature-x as
// copies says.
function ahead(n: number, copies: Partial<Worktree['copies']>): Worktree {
  return partlyCopiedWorktree({
    unpushed: n,
    copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: n, ...copies }),
  })
}

describe('laneOf', () => {
  it.each([
    ['Claude running', runningClaude, 'running'],
    ['Codex running', runningCodex, 'running'],
    ['a routine’s agent', routine, 'running'],
    ['a live agent lock, no process listed', liveLock, 'running'],
    ['merged, all on GitHub', ready, 'ready'],
    ['merged, commits not on GitHub', reviewCommits, 'review'],
    ['merged, uncommitted files', reviewDirty, 'review'],
    ['open and clean', openClean, 'open'],
    ['open with files and commits', both, 'risk'],
    ['commits not on GitHub alone', upOnly, 'risk'],
    ['commits it couldn’t count', unknown, 'risk'],
    ['a lock its ended session left', staleLock, 'open'],
    ['locked by hand', handLocked, 'problem'],
    ['its folder gone', prunable, 'problem'],
    ['git couldn’t read it', erred, 'problem'],
    ['an error, though something runs in it', row({ error: 'unreadable', inUse: [codexHere] }), 'problem'],
    ['detached, no item', detached, 'open'],
    ['copied onto an open pull request’s branch', copied, 'open'],
    ['copied into a pull request since merged', recorded, 'ready'],
    ['with only an edited copy', partly, 'risk'],
  ])('puts a worktree %s in its lane', (_, wt, want) => {
    expect(laneOf(wt)).toBe(want)
  })

  it('ranks the lanes in the attention order, each with its words and color', () => {
    expect(lanes).toEqual({
      running: { rank: 0, words: 'running', tone: 'info' },
      review: { rank: 1, words: 'needs a look', tone: 'warning' },
      risk: { rank: 2, words: 'holds work', tone: 'warning' },
      open: { rank: 3, words: 'open', tone: '' },
      ready: { rank: 4, words: 'ready to clean up', tone: 'success' },
      problem: { rank: 5, words: 'problem', tone: 'danger' },
    })
  })
})

describe('edgeTitle', () => {
  it.each([
    ['running', runningCodex, 'Something runs in it'],
    ['a pull request done', reviewCommits, 'Its pull request is done, but it holds work that isn’t on GitHub'],
    [
      'an issue done',
      row({ item: linkedItem({ kind: 'issue', state: 'closed' }), stage: 'needs-review', dirtyCount: 1 }),
      'Its issue is done, but it holds work that isn’t on GitHub',
    ],
    [
      'done with no item read',
      row({ item: null, stage: 'needs-review' }),
      'Its pull request is done, but it holds work that isn’t on GitHub',
    ],
    ['holding work', both, 'It holds work that isn’t on GitHub yet'],
    ['ready', ready, 'Done on GitHub; cleaning it up loses nothing'],
    ['open, with no edge', openClean, ''],
    ['its folder gone', prunable, 'Its folder is gone'],
    ['locked by hand', handLocked, 'Locked by hand'],
    ['unreadable', erred, 'git couldn’t read it'],
  ])('says why a row stands in its lane: %s', (_, wt, want) => {
    expect(edgeTitle(wt)).toBe(want)
  })
})

describe('attentionSort', () => {
  it('orders by attention, then the newest commit, then as listed', () => {
    const at = (wt: Worktree, path: string, lastCommitUnix: number) => ({ ...wt, path, lastCommitUnix })
    const rows = [
      at(ready, 'ready', 100),
      at(openClean, 'open-older', 300),
      at(handLocked, 'locked', 50),
      at(runningCodex, 'codex', 10),
      at(reviewCommits, 'review', 20),
      at(both, 'both', 5),
      at(unknown, 'unknown', 5),
      at(openClean, 'open-newer', 400),
    ]

    expect(attentionSort(rows).map((wt) => wt.path)).toEqual([
      'codex',
      'review',
      'both',
      'unknown',
      'open-newer',
      'open-older',
      'ready',
      'locked',
    ])
    expect(rows[0]?.path).toBe('ready')
  })
})

describe('stateOf', () => {
  it.each([
    [erred, 'alert', 'danger', 'git couldn’t read it'],
    [prunable, 'ghost', 'danger', 'its folder is gone'],
    [runningCodex, 'ping', 'info', 'something runs in it'],
    [liveLock, 'ping', 'info', 'something runs in it'],
    [handLocked, 'lock', 'danger', 'locked by hand'],
    [staleLock, 'lock', 'muted', 'locked by a session that has ended'],
    [both, 'dot', 'warning', 'uncommitted changes'],
    [upOnly, 'up', 'caution', 'commits that aren’t on GitHub'],
    [unknown, 'ring', 'muted', 'couldn’t tell whether every commit is on GitHub'],
    [ready, 'check', 'success', 'clean, and every commit is on GitHub'],
    [openClean, 'check', 'muted', 'clean, and every commit is on GitHub'],
    [copied, 'check', 'muted', 'clean; each commit’s change is on GitHub, copied onto PR #42’s branch'],
    [unlinked, 'check', 'muted', 'clean; each commit’s change is on GitHub, copied onto origin/feature-x'],
    [recorded, 'check', 'success', 'clean; each commit’s change went into PR #42'],
    [elsewhere, 'check', 'success', 'clean; each commit’s change went into PR #42'],
    [held, 'check', 'success', 'clean; each commit’s change went into PR #42'],
    [
      copiedWorktree({
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10, pr: 43 }),
      }),
      'check',
      'muted',
      'clean; each commit’s change is on GitHub, copied onto PR #43’s branch',
    ],
    [partly, 'up', 'caution', 'commits that aren’t on GitHub'],
    [copiedWorktree({ dirty: true, dirtyCount: 1 }), 'dot', 'warning', 'uncommitted changes'],
  ])('marks the state of %#', (wt, glyph, tone, words) => {
    expect(stateOf(wt)).toEqual({ glyph, tone, words })
  })

  it('says a worktree at its pull request’s head is on GitHub by its commit IDs, copies or not', () => {
    expect(stateOf(copiedWorktree({ item: copiedPR({ headOid: copied.head }) })).words).toBe(
      'clean, and every commit is on GitHub',
    )
  })
})

describe('workNotes', () => {
  it('says nothing for a clean worktree with every commit on GitHub', () => {
    expect(workNotes(openClean)).toEqual([])
  })

  it('counts the uncommitted files and the commits not on GitHub', () => {
    expect(workNotes(both)).toEqual([
      {
        kind: 'dirty',
        tone: 'warning',
        text: '7 files',
        sr: 'uncommitted: ',
        title: '7 files have uncommitted changes, counting untracked files',
      },
      { kind: 'up', tone: 'caution', text: '2', sr: 'not on GitHub: ', title: '2 commits aren’t on GitHub' },
    ])
  })

  it.each([
    ['one of each', row({ dirtyCount: 1, unpushed: 1, onGitHub: false }), ['1 file', '1']],
    ['grouping the digits', row({ dirtyCount: 1204, unpushed: 1204, onGitHub: false }), ['1,204 files', '1,204']],
    ['with commits it couldn’t count', row({ dirtyCount: 2, unpushed: -1, onGitHub: false }), ['2 files', '?']],
    ['commits on GitHub through its pull request’s head', row({ unpushed: 3, onGitHub: true }), []],
  ])('notes the work %s', (_, wt, want) => {
    expect(workNotes(wt).map((n) => n.text)).toEqual(want)
  })

  it('words one file and one commit, and the commits it couldn’t count', () => {
    expect(workNotes(row({ dirtyCount: 1, unpushed: 1, onGitHub: false })).map((n) => n.title)).toEqual([
      '1 file has uncommitted changes, counting untracked files',
      '1 commit isn’t on GitHub',
    ])
    expect(workNotes(unknown)).toEqual([
      {
        kind: 'unknown',
        tone: 'muted',
        text: '?',
        sr: 'commits not on GitHub: ',
        title: 'Couldn’t count the commits that aren’t on GitHub',
      },
    ])
  })

  it('notes commits on GitHub only as copies, muted, saying where', () => {
    expect(workNotes(copied)).toEqual([
      {
        kind: 'copied',
        tone: 'muted',
        text: '10 on #42',
        sr: 'on GitHub as copies: ',
        title: 'Its 10 commits aren’t on GitHub by ID, but each one’s change is on origin/feature-x (PR #42)',
      },
    ])
  })

  it.each([
    [
      'onto a branch no pull request links them by',
      unlinked,
      '10 on feature-x',
      'Its 10 commits aren’t on GitHub by ID, but each one’s change is on origin/feature-x',
    ],
    [
      'onto the main branch of a remote whose name holds a slash',
      copiedWorktree({
        item: null,
        linkedBy: '',
        copies: worktreeCopies({ ref: 'origin/mirror/main', branch: 'main', count: 10, identical: 10 }),
      }),
      '10 on main',
      'Its 10 commits aren’t on GitHub by ID, but each one’s change is on origin/mirror/main',
    ],
    [
      'into a pull request whose branch is gone',
      recorded,
      '10 on #42',
      'Its 10 commits aren’t on GitHub by ID, but each one’s change went into PR #42, from origin/feature-x, a branch that’s gone now',
    ],
    [
      'through a branch that’s gone, into a pull request it isn’t linked to',
      elsewhere,
      '10 on #42',
      'Its 10 commits aren’t on GitHub by ID, but each one’s change went into PR #42, from origin/feature-x, a branch that’s gone now',
    ],
    [
      'into a merged pull request whose branch no longer holds them all',
      held,
      '10 on #42',
      'Its 10 commits aren’t on GitHub by ID, but each one’s change went into PR #42; origin/feature-x no longer holds all of them',
    ],
    [
      'into a pull request whose number isn’t known',
      recordedWorktree({
        item: null,
        linkedBy: '',
        copies: worktreeCopies({
          ref: 'origin/feature-x',
          branch: 'feature-x',
          count: 10,
          identical: 10,
          recorded: true,
          gone: true,
        }),
      }),
      '10 on feature-x',
      'Its 10 commits aren’t on GitHub by ID, but each one’s change went into its pull request, from origin/feature-x, a branch that’s gone now',
    ],
    [
      'onto the branch of another pull request than the one it is linked to',
      copiedWorktree({
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10, pr: 43 }),
      }),
      '10 on #43',
      'Its 10 commits aren’t on GitHub by ID, but each one’s change is on origin/feature-x (PR #43)',
    ],
    [
      'one commit',
      copiedWorktree({
        unpushed: 1,
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 1, identical: 1, pr: 42 }),
      }),
      '1 on #42',
      'Its 1 commit isn’t on GitHub by ID, but its change is on origin/feature-x (PR #42)',
    ],
    [
      'with the digits grouped, onto another remote’s branch with a slash in its name',
      copiedWorktree({
        unpushed: 1204,
        item: null,
        linkedBy: '',
        copies: worktreeCopies({ ref: 'upstream/feat/x', branch: 'feat/x', count: 1204, identical: 1204 }),
      }),
      '1,204 on feat/x',
      'Its 1,204 commits aren’t on GitHub by ID, but each one’s change is on upstream/feat/x',
    ],
  ])('notes commits copied %s', (_, wt, text, title) => {
    expect(workNotes(wt)).toEqual([{ kind: 'copied', tone: 'muted', text, sr: 'on GitHub as copies: ', title }])
  })

  it('notes uncommitted files beside commits on GitHub only as copies', () => {
    expect(workNotes(copiedWorktree({ dirtyCount: 3 })).map((n) => [n.kind, n.text])).toEqual([
      ['dirty', '3 files'],
      ['copied', '10 on #42'],
    ])
  })

  it.each([
    ['with only an edited copy', partly, '1 commit isn’t on GitHub: 1 has only an edited copy on origin/feature-x'],
    [
      'most with an identical copy, one with only an edited one',
      ahead(10, { identical: 9, edited: 1 }),
      '10 commits aren’t on GitHub: 9 have an identical copy on origin/feature-x, 1 only an edited one',
    ],
    [
      'one with an identical copy, the others with none',
      ahead(3, { identical: 1, none: 2 }),
      '3 commits aren’t on GitHub: 1 has an identical copy on origin/feature-x, 2 none',
    ],
    [
      'with edited copies alone',
      ahead(2, { edited: 2 }),
      '2 commits aren’t on GitHub: 2 have only an edited copy on origin/feature-x',
    ],
    [
      'with an edited copy and none',
      ahead(2, { edited: 1, none: 1 }),
      '2 commits aren’t on GitHub: 1 has only an edited copy on origin/feature-x, 1 none',
    ],
    [
      'with copies spread over several branches',
      ahead(10, {
        identical: 7,
        edited: 2,
        none: 1,
        note: 'Each of its commits has an identical copy on one of the pushed branches, but no one branch has them all.',
      }),
      '10 commits aren’t on GitHub: 7 have an identical copy on origin/feature-x, 2 only an edited one, 1 none. Each of its commits has an identical copy on one of the pushed branches, but no one branch has them all.',
    ],
    [
      'that couldn’t be compared',
      ahead(3, { ref: '', branch: '', note: 'Couldn’t compare its commits with the pushed branches. Git refused it.' }),
      '3 commits aren’t on GitHub. Couldn’t compare its commits with the pushed branches. Git refused it.',
    ],
    ['with no copy anywhere', ahead(3, { ref: '', branch: '', none: 3 }), '3 commits aren’t on GitHub'],
    [
      'compared when there were others',
      partlyCopiedWorktree({
        unpushed: 3,
        copies: worktreeCopies({
          ref: 'origin/feature-x',
          branch: 'feature-x',
          count: 2,
          edited: 2,
          note: 'Something.',
        }),
      }),
      '3 commits aren’t on GitHub',
    ],
  ])('says how commits not on GitHub compare with the pushed branches: %s', (_, wt, title) => {
    expect(workNotes(wt)).toEqual([
      { kind: 'up', tone: 'caution', text: String(wt.unpushed), sr: 'not on GitHub: ', title },
    ])
  })

  it.each([
    ['as many as compared, but with a note', ahead(3, { identical: 3, note: 'Not counted.' })],
    ['more than were compared', copiedWorktree({ unpushed: 11 })],
    ['on no branch', copiedWorktree({ copies: worktreeCopies({ count: 10, identical: 10 }) })],
  ])('notes nothing of copies the backend counts on GitHub that don’t back it: %s', (_, wt) => {
    expect(workNotes({ ...wt, onGitHub: true })).toEqual([])
  })
})

describe('verbOf', () => {
  const removes = 'the worktree, whose every commit is on GitHub; asks which branches go with it'

  it.each([
    [
      'Clean up for a finished one',
      ready,
      true,
      { act: 'cleanUp', label: 'Clean up', tone: 'success', title: `Removes ${removes}` },
    ],
    [
      'Clean up that releases an ended session’s lock first',
      row({ item: linkedItem(), stage: 'finished', canCleanUp: true, locked: true, agentLock: true, staleLock: true }),
      false,
      {
        act: 'cleanUp',
        label: 'Clean up',
        tone: 'success',
        title: `Releases the ended session’s lock, then removes ${removes}`,
      },
    ],
    [
      'Unlock for a lock its ended session left',
      staleLock,
      true,
      {
        act: 'unlock',
        label: 'Unlock',
        tone: '',
        title: 'Releases the lock of the Claude session that ended here, so it can be removed',
      },
    ],
    [
      'a launch on an open pull request the inbox lists',
      openClean,
      true,
      { act: 'launch', title: 'Launches on PR #7 in this worktree, which resumes it (your launch defaults)' },
    ],
    [
      'a launch on an open issue',
      row({ item: open({ kind: 'issue', number: 88 }) }),
      true,
      { act: 'launch', title: 'Launches on Issue #88 in this worktree, which resumes it (your launch defaults)' },
    ],
    ['nothing for an item the inbox doesn’t list', openClean, false, null],
    ['nothing while something runs in it', runningCodex, true, null],
    ['nothing while it’s locked by hand', handLocked, true, null],
    ['nothing for a merged one that needs a look', reviewCommits, true, null],
    ['nothing without an item', detached, true, null],
    [
      'Clean up for one whose commits went into a pull request since merged, as copies',
      recorded,
      true,
      {
        act: 'cleanUp',
        label: 'Clean up',
        tone: 'success',
        title: 'Removes the worktree, whose every commit’s change is on GitHub; asks which branches go with it',
      },
    ],
    ['no launch on the pull request its commits were copied onto', copied, true, null],
    ['no launch on the pull request whose branch it is on', row({ linkedBy: 'branch' }), true, null],
  ])('offers %s', (_, wt, listed, want) => {
    expect(verbOf(wt, listed)).toEqual(want)
  })
})

describe('removeWhy', () => {
  it.each([
    [
      'its folder gone',
      prunable,
      'Its folder is gone: Prune clears the entry',
      'Its folder is gone: Prune clears the entry',
    ],
    ['unreadable', erred, 'git couldn’t read it', 'git couldn’t read it'],
    ['Codex running', runningCodex, 'Codex runs in it', 'Codex runs in it'],
    ['a routine’s agent', routine, '⚡ Fix checks runs its agent in it', '⚡ Fix checks runs its agent in it'],
    [
      'a live agent lock',
      liveLock,
      'The Claude session that locked it still runs',
      'The Claude session that locked it still runs',
    ],
    ['locked by hand', handLocked, 'It’s locked by hand', 'It’s locked by hand'],
    [
      'a lock its ended session left',
      staleLock,
      'A Claude session that has ended still locks it: Unlock releases it',
      'A Claude session that has ended still locks it: Unlock releases it',
    ],
    ['with uncommitted files', both, 'It has uncommitted files: Force remove… discards them', ''],
    ['clean', openClean, '', 'Nothing to discard: Remove… takes it'],
    [
      'with files the backend won’t force-remove, as while the running processes couldn’t be listed',
      row({ dirty: true, dirtyCount: 2, canRemove: false, canForceRemove: false }),
      'It has uncommitted files: Force remove… discards them',
      'It can’t be force-removed right now: Check again',
    ],
    [
      'clean, that the backend won’t remove for a reason the listing doesn’t show',
      row({ canRemove: false }),
      'It can’t be removed right now: Check again',
      'Nothing to discard: Remove… takes it',
    ],
  ])('says why a removal can’t run on one %s', (_, wt, remove, force) => {
    expect(removeWhy(wt, false, agentName)).toBe(remove)
    expect(removeWhy(wt, true, agentName)).toBe(force)
  })
})

describe('wordsText', () => {
  it('reads a sentence’s code parts as plain text', () => {
    expect(wordsText(['On ', { code: 'main' }, ', clean.'])).toBe('On main, clean.')
    expect(wordsText([])).toBe('')
  })
})

describe('safety', () => {
  const clone = worktreeRepo({ name: 'example-repo', displayPath: '~/projects/example-repo' })

  // said reads a safety sentence as text, its code parts plain.
  function said(s: Safety) {
    return { tone: s.tone, icon: s.icon, text: wordsText(s.text), also: wordsText(s.also), command: s.command }
  }

  // read is what the drawer says of wt in the example clone, with the session given.
  function read(
    wt: Worktree,
    session = agentSession({ dir: wt.displayPath, state: 'working', launchedAt: isoAgo({ m: 12 }) }),
  ) {
    return said(safety(wt, clone, now, session, agentName))
  }

  it.each([
    [
      'git couldn’t read it',
      erred,
      {
        tone: 'danger',
        icon: 'alert',
        text: 'Couldn’t read this worktree, so nothing here can say what removing it would cost.',
        also: 'Git status refused: this clone’s own git config sets core.fsmonitor, which isn’t run.',
        command: '',
      },
    ],
    [
      'git couldn’t read it, in a sentence of its own',
      row({ error: 'could not read the status.' }),
      {
        tone: 'danger',
        icon: 'alert',
        text: 'Couldn’t read this worktree, so nothing here can say what removing it would cost.',
        also: 'Could not read the status.',
        command: '',
      },
    ],
    [
      'its folder gone',
      prunable,
      {
        tone: 'danger',
        icon: 'ghost',
        text: 'Its folder is gone, so there’s nothing left in it to lose.',
        also: 'git still lists it (gitdir file points to non-existent location). Prune example-repo clears the entry; it touches no worktree that exists, and no branch.',
        command: '',
      },
    ],
    [
      'its folder gone, git giving no reason',
      row({ prunable: true, prunableReason: '' }),
      {
        tone: 'danger',
        icon: 'ghost',
        text: 'Its folder is gone, so there’s nothing left in it to lose.',
        also: 'git still lists it. Prune example-repo clears the entry; it touches no worktree that exists, and no branch.',
        command: '',
      },
    ],
    [
      'Claude running, with files and a commit',
      runningClaude,
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while Claude runs in it (pid 47890, started 11:48, 12m ago).',
        also: 'Once it ends, removing it would discard 30 uncommitted files. Its 1 commit not on GitHub stays in the clone on feat/inspect-point, which removing it keeps.',
        command: '',
      },
    ],
    [
      'Codex running, with commits alone',
      row({ inUse: [codexHere], stage: 'in-use', branch: 'feat/retry', unpushed: 2, onGitHub: false }),
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while Codex runs in it (pid 60311, started 11:48, 12m ago).',
        also: 'Once it ends, its 2 commits not on GitHub stay in the clone on feat/retry, which removing it keeps.',
        command: '',
      },
    ],
    [
      'something running in a detached one, with commits and files',
      row({
        inUse: [codexHere],
        stage: 'in-use',
        detached: true,
        branch: '',
        dirtyCount: 2,
        unpushed: 2,
        onGitHub: false,
      }),
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while Codex runs in it (pid 60311, started 11:48, 12m ago).',
        also: 'Once it ends, removing it would discard 2 uncommitted files. 2 commits that no branch holds would be lost with it.',
        command: '',
      },
    ],
    [
      'something running in a detached one, with a commit alone',
      row({ inUse: [codexHere], stage: 'in-use', detached: true, branch: '', unpushed: 1, onGitHub: false }),
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while Codex runs in it (pid 60311, started 11:48, 12m ago).',
        also: 'Once it ends, 1 commit that no branch holds would be lost with it.',
        command: '',
      },
    ],
    [
      'a live agent lock',
      row({ ...liveLock, dirtyCount: 0, dirty: false }),
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while the Claude session that locked it still runs (pid 70113, started 11:30, 29m ago).',
        also: 'Once it ends, nothing in it would be lost: every commit is on GitHub.',
        command: '',
      },
    ],
    [
      'something running, with files and commits it couldn’t count',
      row({ inUse: [codexHere], stage: 'in-use', dirtyCount: 2, unpushed: -1, onGitHub: false }),
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while Codex runs in it (pid 60311, started 11:48, 12m ago).',
        also: 'Once it ends, removing it would discard 2 uncommitted files. Couldn’t count the commits that aren’t on GitHub, so removing it might lose some.',
        command: '',
      },
    ],
    [
      'something running in a detached one, clean, with commits it couldn’t count',
      row({ inUse: [codexHere], stage: 'in-use', detached: true, branch: '', unpushed: -1, onGitHub: false }),
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while Codex runs in it (pid 60311, started 11:48, 12m ago).',
        also: 'Once it ends, its folder is clean, but the commits that aren’t on GitHub couldn’t be counted, so removing it might lose some.',
        command: '',
      },
    ],
  ])('says what removing one costs: %s', (_, wt, want) => {
    expect(read(wt)).toEqual(want)
  })

  it.each([
    [
      'a routine’s agent, unfollowed',
      routine,
      'Can’t be removed while ⚡ Fix checks runs its agent in it (pid 61022).',
    ],
    [
      'a routine’s agent before it has a process',
      row({ inUse: [{ ...routineHere, pid: 0 }], stage: 'in-use' }),
      'Can’t be removed while ⚡ Fix checks runs its agent in it.',
    ],
    [
      'a session the app launched, read from its stage',
      row({ stage: 'in-use', agent: 'codex', lastLaunchedAt: isoAgo({ m: 5 }) }),
      'Can’t be removed while Codex runs in it.',
    ],
    ['a session it knows nothing of', row({ stage: 'in-use' }), 'Can’t be removed while a session runs in it.'],
  ])('names what runs in it without a session followed there: %s', (_, wt, want) => {
    expect(said(safety(wt, clone, now, undefined, agentName)).text).toBe(want)
  })

  it.each([
    [
      'clean',
      handLocked,
      'After that, nothing in it would be lost: every commit is on GitHub.',
      `git -C ~/projects/example-repo worktree unlock ~/projects/.${brand.slug}/worktrees/example-repo/pr-254`,
    ],
    [
      'with files and a commit its branch keeps',
      row({ ...handLocked, dirtyCount: 2, unpushed: 1, onGitHub: false }),
      'Then removing it would discard 2 uncommitted files. Its 1 commit not on GitHub stays in the clone on worktree-pr-7, which removing it keeps.',
      `git -C ~/projects/example-repo worktree unlock ~/projects/.${brand.slug}/worktrees/example-repo/pr-254`,
    ],
    [
      'detached, with files and commits no branch holds',
      row({ ...handLocked, detached: true, branch: '', dirtyCount: 1, unpushed: 2, onGitHub: false }),
      'Then removing it would discard 1 uncommitted file. 2 commits that no branch holds would be lost with it.',
      `git -C ~/projects/example-repo worktree unlock ~/projects/.${brand.slug}/worktrees/example-repo/pr-254`,
    ],
    [
      'with files and commits it couldn’t count',
      row({ ...handLocked, dirtyCount: 2, unpushed: -1, onGitHub: false }),
      'Then removing it would discard 2 uncommitted files. Couldn’t count the commits that aren’t on GitHub, so removing it might lose some.',
      `git -C ~/projects/example-repo worktree unlock ~/projects/.${brand.slug}/worktrees/example-repo/pr-254`,
    ],
    [
      'clean, with commits it couldn’t count',
      row({ ...handLocked, unpushed: -1, onGitHub: false }),
      'After that, its folder is clean, but the commits that aren’t on GitHub couldn’t be counted, so removing it might lose some.',
      `git -C ~/projects/example-repo worktree unlock ~/projects/.${brand.slug}/worktrees/example-repo/pr-254`,
    ],
    [
      'with commits alone',
      row({ ...handLocked, branch: 'release', unpushed: 3, onGitHub: false }),
      'Then its 3 commits not on GitHub stay in the clone on release, which removing it keeps.',
      `git -C ~/projects/example-repo worktree unlock ~/projects/.${brand.slug}/worktrees/example-repo/pr-254`,
    ],
    [
      'detached, with commits alone, in a folder whose name needs quotes',
      row({ ...handLocked, displayPath: '~/projects/My Work/pr 254', detached: true, unpushed: 3, onGitHub: false }),
      'Then 3 commits that no branch holds would be lost with it.',
      `git -C ~/projects/example-repo worktree unlock ~/'projects/My Work/pr 254'`,
    ],
  ])('gives the command that unlocks one locked by hand, %s', (_, wt, also, command) => {
    expect(read(wt)).toEqual({
      tone: 'danger',
      icon: 'lock',
      text: 'Locked by hand, so it can’t be removed here. Unlock it first:',
      also,
      command,
    })
  })

  it('unlocks one locked by hand in a clone it isn’t told of', () => {
    expect(safety(handLocked, null, now, undefined, agentName).command).toBe(
      `git -C '' worktree unlock ~/projects/.${brand.slug}/worktrees/example-repo/pr-254`,
    )
  })

  it.each([
    [
      'files and commits',
      both,
      'warning',
      'alert',
      'Removing it would discard 7 uncommitted files, counting untracked ones.',
      'Its 2 commits not on GitHub stay in the clone on worktree-pr-221, which removing it keeps.',
    ],
    [
      'files, merged without them',
      reviewDirty,
      'warning',
      'alert',
      'Removing it would discard 3 uncommitted files, counting untracked ones.',
      'PR #150 merged 2d ago, without this work.',
    ],
    [
      'one file, and nothing more to say',
      row({ dirtyCount: 1 }),
      'warning',
      'alert',
      'Removing it would discard 1 uncommitted file, counting untracked ones.',
      '',
    ],
    [
      'files and commits no branch holds',
      row({ detached: true, branch: '', dirtyCount: 2, unpushed: 2, onGitHub: false }),
      'warning',
      'alert',
      'Removing it would discard 2 uncommitted files, counting untracked ones.',
      '2 commits that no branch holds would be lost with it.',
    ],
    [
      'files and commits it couldn’t count, merged at a time it isn’t told',
      row({
        item: linkedItem({ number: 150, closedAt: '' }),
        stage: 'needs-review',
        detached: true,
        branch: '',
        dirtyCount: 4,
        unpushed: -1,
        onGitHub: false,
      }),
      'warning',
      'alert',
      'Removing it would discard 4 uncommitted files, counting untracked ones.',
      'Couldn’t count the commits that aren’t on GitHub, so removing it might lose some. PR #150 merged, without this work.',
    ],
    [
      'a commit no branch holds',
      row({ detached: true, branch: '', unpushed: 1, onGitHub: false }),
      'warning',
      'alert',
      '1 commit that no branch holds would be lost with it.',
      'Its folder is clean.',
    ],
    [
      'commits, merged without them',
      reviewCommits,
      'warning',
      'up',
      'Its folder is clean, but 2 commits on worktree-pr-131 aren’t on GitHub.',
      'PR #131 merged 6d ago, without this work. Removing the worktree keeps that branch, so they stay in the clone; the app won’t delete it while they’re only here.',
    ],
    [
      'one commit',
      row({ unpushed: 1, onGitHub: false }),
      'warning',
      'up',
      'Its folder is clean, but 1 commit on worktree-pr-7 isn’t on GitHub.',
      'Removing the worktree keeps that branch, so it stays in the clone; the app won’t delete it while it’s only here.',
    ],
    [
      'commits it couldn’t count',
      unknown,
      'warning',
      'ring',
      'Couldn’t count the commits that aren’t on GitHub, so removing it might lose some.',
      'Its folder is clean.',
    ],
    [
      'nothing, at its pull request’s head',
      openClean,
      'success',
      'shield',
      'Safe to remove: no uncommitted files, and every commit is on GitHub (it’s at PR #7’s head).',
      '',
    ],
    [
      'nothing, past its pull request’s head',
      row({ item: open({ headOid: '0b1c3975011e66491785ace891c893e4fc716588' }) }),
      'success',
      'shield',
      'Safe to remove: no uncommitted files, and every commit is on GitHub.',
      '',
    ],
    [
      'nothing, for an issue',
      row({ item: open({ kind: 'issue', headOid: '' }), head: '' }),
      'success',
      'shield',
      'Safe to remove: no uncommitted files, and every commit is on GitHub.',
      '',
    ],
    [
      'nothing, with no item',
      detached,
      'success',
      'shield',
      'Safe to remove: no uncommitted files, and every commit is on GitHub.',
      '',
    ],
    [
      'nothing, an ended session’s lock aside',
      staleLock,
      'success',
      'shield',
      'Safe to remove: no uncommitted files, and every commit is on GitHub (it’s at PR #7’s head).',
      'A Claude session that has ended still locks it; Unlock releases the lock first.',
    ],
    [
      'nothing, an ended session’s lock that Clean up releases',
      row({ ...ready, locked: true, agentLock: true, staleLock: true }),
      'success',
      'shield',
      'Safe to remove: no uncommitted files, and every commit is on GitHub (it’s at PR #7’s head).',
      'A Claude session that has ended still locks it; Clean up releases the lock first.',
    ],
  ])('says what removing one costs: %s', (_, wt, tone, icon, text, also) => {
    expect(read(wt)).toEqual({ tone, icon, text, also, command: '' })
  })

  it.each([
    [
      'onto an open pull request’s branch, which keeps its own',
      copied,
      'Safe to remove: no uncommitted files, and every commit’s change is on GitHub, copied onto PR #42’s branch.',
      'Removing it keeps its branch, worktree-agent-a1b2c3, with the original commits.',
    ],
    [
      'onto a branch no pull request links them by',
      unlinked,
      'Safe to remove: no uncommitted files, and every commit’s change is on GitHub, copied onto origin/feature-x.',
      'Removing it keeps its branch, worktree-agent-a1b2c3, with the original commits.',
    ],
    [
      'into a pull request since merged, whose branch is gone, its own free to go',
      recorded,
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, merged 2d ago.',
      '',
    ],
    [
      'into a pull request still open, whose branch is gone',
      recordedWorktree({ item: copiedPR() }),
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, which is open.',
      '',
    ],
    [
      'into a pull request closed at a time it isn’t told',
      recordedWorktree({ item: copiedPR({ state: 'closed', closedAt: '' }) }),
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, closed.',
      '',
    ],
    [
      'through a branch that’s gone, into a pull request it isn’t linked to, whose state it isn’t told',
      elsewhere,
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42.',
      '',
    ],
    [
      'into a pull request since merged, whose branch no longer holds them all',
      held,
      'Safe to remove: no uncommitted files, and every commit’s change went into PR #42, merged 2d ago; origin/feature-x no longer holds all of them.',
      '',
    ],
    [
      'onto the branch of another pull request than the one it is linked to',
      copiedWorktree({
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10, pr: 43 }),
      }),
      'Safe to remove: no uncommitted files, and every commit’s change is on GitHub, copied onto PR #43’s branch.',
      'Removing it keeps its branch, worktree-agent-a1b2c3, with the original commits.',
    ],
    [
      'from a detached worktree, whose original commits go with it',
      copiedWorktree({ detached: true, branch: '', branches: [] }),
      'Safe to remove: no uncommitted files, and every commit’s change is on GitHub, copied onto PR #42’s branch.',
      'Only the original commits, which no branch holds, go with it.',
    ],
    [
      'with its branch not offered, an ended session’s lock aside',
      copiedWorktree({ branches: [], locked: true, agentLock: true, staleLock: true, canUnlock: true }),
      'Safe to remove: no uncommitted files, and every commit’s change is on GitHub, copied onto PR #42’s branch.',
      'Removing it keeps its branch, worktree-agent-a1b2c3, with the original commits. A Claude session that has ended still locks it; Unlock releases the lock first.',
    ],
  ])('says nothing is lost with commits on GitHub only as copies: %s', (_, wt, text, also) => {
    expect(read(wt)).toEqual({ tone: 'success', icon: 'shield', text, also, command: '' })
  })

  it('says it’s at its pull request’s head rather than copied, when it is', () => {
    expect(read(copiedWorktree({ item: copiedPR({ headOid: copied.head }) })).text).toBe(
      'Safe to remove: no uncommitted files, and every commit is on GitHub (it’s at PR #42’s head).',
    )
  })

  it('sets the branch the copies are on, and the worktree’s own, in code', () => {
    expect(safety(unlinked, clone, now, undefined, agentName)).toMatchObject({
      text: [
        'Safe to remove: no uncommitted files, and every commit’s change ',
        'is on GitHub, copied onto ',
        { code: 'origin/feature-x' },
        '.',
      ],
      also: ['Removing it keeps its branch, ', { code: 'worktree-agent-a1b2c3' }, ', with the original commits.'],
    })
    expect(safety(held, clone, now, undefined, agentName).text).toEqual([
      'Safe to remove: no uncommitted files, and every commit’s change ',
      'went into PR #42',
      ', merged 2d ago',
      '; ',
      { code: 'origin/feature-x' },
      ' no longer holds all of them',
      '.',
    ])
  })

  it.each([
    [
      'something running',
      copiedWorktree({ inUse: [codexHere], stage: 'in-use' }),
      'Once it ends, nothing in it would be lost: every commit’s change is on GitHub.',
    ],
    [
      'locked by hand',
      copiedWorktree({ locked: true, lockReason: 'kept', stage: '' }),
      'After that, nothing in it would be lost: every commit’s change is on GitHub.',
    ],
    [
      'something running, detached',
      copiedWorktree({ inUse: [codexHere], stage: 'in-use', detached: true, branch: '', branches: [] }),
      'Once it ends, nothing in it would be lost: every commit’s change is on GitHub. Only the original commits, which no branch holds, go with it.',
    ],
    [
      'something running, detached, with files',
      copiedWorktree({ inUse: [codexHere], stage: 'in-use', detached: true, branch: '', dirtyCount: 2 }),
      'Once it ends, removing it would discard 2 uncommitted files. Only the original commits, which no branch holds, go with it.',
    ],
    [
      'locked by hand, detached',
      copiedWorktree({ locked: true, lockReason: 'kept', stage: '', detached: true, branch: '' }),
      'After that, nothing in it would be lost: every commit’s change is on GitHub. Only the original commits, which no branch holds, go with it.',
    ],
    [
      'locked by hand, detached, with files',
      copiedWorktree({ locked: true, lockReason: 'kept', stage: '', detached: true, branch: '', dirtyCount: 1 }),
      'Then removing it would discard 1 uncommitted file. Only the original commits, which no branch holds, go with it.',
    ],
    [
      'something running, a commit with only an edited copy',
      partlyCopiedWorktree({ inUse: [codexHere], stage: 'in-use' }),
      'Once it ends, its 1 commit not on GitHub stays in the clone on worktree-agent-d4e5f6, which removing it keeps. 1 has only an edited copy on origin/feature-x.',
    ],
    [
      'something running, files and a commit with only an edited copy',
      partlyCopiedWorktree({ inUse: [codexHere], stage: 'in-use', dirtyCount: 2 }),
      'Once it ends, removing it would discard 2 uncommitted files. Its 1 commit not on GitHub stays in the clone on worktree-agent-d4e5f6, which removing it keeps. 1 has only an edited copy on origin/feature-x.',
    ],
    [
      'locked by hand, a commit with only an edited copy',
      partlyCopiedWorktree({ locked: true, lockReason: 'kept', stage: '' }),
      'Then its 1 commit not on GitHub stays in the clone on worktree-agent-d4e5f6, which removing it keeps. 1 has only an edited copy on origin/feature-x.',
    ],
    [
      'locked by hand, files and a commit with only an edited copy',
      partlyCopiedWorktree({ locked: true, lockReason: 'kept', stage: '', dirtyCount: 1 }),
      'Then removing it would discard 1 uncommitted file. Its 1 commit not on GitHub stays in the clone on worktree-agent-d4e5f6, which removing it keeps. 1 has only an edited copy on origin/feature-x.',
    ],
  ])('says what would be lost later from one copied with %s', (_, wt, also) => {
    expect(read(wt).also).toBe(also)
  })

  it.each([
    [
      'commits, one with only an edited copy',
      partly,
      'up',
      'Its folder is clean, but 1 commit on worktree-agent-d4e5f6 isn’t on GitHub.',
      '1 has only an edited copy on origin/feature-x. Removing the worktree keeps that branch, so it stays in the clone; the app won’t delete it while it’s only here.',
    ],
    [
      'commits copied onto several branches',
      ahead(10, {
        identical: 7,
        edited: 2,
        none: 1,
        note: 'Each of its commits has an identical copy on one of the pushed branches, but no one branch has them all.',
      }),
      'up',
      'Its folder is clean, but 10 commits on worktree-agent-d4e5f6 aren’t on GitHub.',
      '7 have an identical copy on origin/feature-x, 2 only an edited one, 1 none. Each of its commits has an identical copy on one of the pushed branches, but no one branch has them all. Removing the worktree keeps that branch, so they stay in the clone; the app won’t delete it while they’re only here.',
    ],
    [
      'commits that couldn’t be compared',
      ahead(3, { ref: '', branch: '', note: 'Couldn’t compare its commits with the pushed branches.' }),
      'up',
      'Its folder is clean, but 3 commits on worktree-agent-d4e5f6 aren’t on GitHub.',
      'Couldn’t compare its commits with the pushed branches. Removing the worktree keeps that branch, so they stay in the clone; the app won’t delete it while they’re only here.',
    ],
    [
      'files, and a commit with only an edited copy',
      partlyCopiedWorktree({ dirtyCount: 2 }),
      'alert',
      'Removing it would discard 2 uncommitted files, counting untracked ones.',
      'Its 1 commit not on GitHub stays in the clone on worktree-agent-d4e5f6, which removing it keeps. 1 has only an edited copy on origin/feature-x.',
    ],
    [
      'a commit no branch holds, with only an edited copy',
      partlyCopiedWorktree({ detached: true, branch: '' }),
      'alert',
      '1 commit that no branch holds would be lost with it.',
      '1 has only an edited copy on origin/feature-x. Its folder is clean.',
    ],
    [
      'files, in a detached worktree whose commits are on GitHub as copies',
      copiedWorktree({ detached: true, branch: '', branches: [], dirtyCount: 2 }),
      'alert',
      'Removing it would discard 2 uncommitted files, counting untracked ones.',
      'Only the original commits, which no branch holds, go with it.',
    ],
  ])('says what of the work is copied where it holds work: %s', (_, wt, icon, text, also) => {
    expect(read(wt)).toEqual({ tone: 'warning', icon, text, also, command: '' })
  })

  it('names the clone a gone folder’s Prune is for, or the clone', () => {
    expect(said(safety(prunable, null, now, undefined, agentName)).also).toBe(
      'git still lists it (gitdir file points to non-existent location). Prune the clone clears the entry; it touches no worktree that exists, and no branch.',
    )
  })

  it('sets the branch in code', () => {
    expect(safety(reviewCommits, clone, now, undefined, agentName).text).toEqual([
      'Its folder is clean, but 2 commits on ',
      { code: 'worktree-pr-131' },
      ' aren’t on GitHub.',
    ])
    expect(safety(both, clone, now, undefined, agentName).also).toEqual([
      'Its 2 commits not on GitHub stay in the clone on ',
      { code: 'worktree-pr-221' },
      ', which removing it keeps.',
    ])
  })
})

describe('cloneSummary', () => {
  it('names the clone by its owner, counts its lanes, and notes its main checkout', () => {
    const repo = worktreeRepo({
      worktrees: [mainWorktree(), runningCodex, reviewCommits, both, ready, prunable, erred, openClean],
    })

    expect(cloneSummary(repo)).toEqual({
      owner: 'acme-org',
      name: 'example-repo',
      note: {
        text: 'on main · clean',
        dirty: false,
        tone: 'muted',
        title: 'The main checkout is on main, with nothing uncommitted',
      },
      lanes: [
        { lane: 'running', n: 1, words: 'running', tone: 'info' },
        { lane: 'risk', n: 2, words: 'at risk', tone: 'warning' },
        { lane: 'ready', n: 1, words: 'ready', tone: 'success' },
        { lane: 'problem', n: 2, words: 'problems', tone: 'danger' },
      ],
      count: 7,
    })
  })

  it('notes a main checkout with uncommitted work in amber', () => {
    const repo = worktreeRepo({
      name: 'api-gateway',
      worktrees: [mainWorktree({ branch: 'feat/retry-budget', dirty: true, dirtyCount: 2 }), handLocked],
    })

    expect(cloneSummary(repo)).toMatchObject({
      note: {
        text: 'on feat/retry-budget · 2 uncommitted',
        dirty: true,
        tone: 'warning',
        title: 'The main checkout is on feat/retry-budget, with 2 uncommitted files',
      },
      lanes: [{ lane: 'problem', n: 1, words: 'problem', tone: 'danger' }],
    })
  })

  it('names a clone with no item by its folder, and a detached main checkout by its commit', () => {
    const repo = worktreeRepo({
      name: 'dotfiles',
      worktrees: [
        mainWorktree({ branch: '', detached: true, head: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678', dirtyCount: 1 }),
        detached,
      ],
    })

    expect(cloneSummary(repo)).toEqual({
      owner: '',
      name: 'dotfiles',
      note: {
        text: 'detached at a1b2c3d · 1 uncommitted',
        dirty: true,
        tone: 'warning',
        title: 'The main checkout is detached at a1b2c3d, with 1 uncommitted file',
      },
      lanes: [],
      count: 1,
    })
  })

  it('notes a main checkout git couldn’t read in red, never as clean', () => {
    const repo = worktreeRepo({
      worktrees: [mainWorktree({ error: 'could not read the status: exit status 128' }), openClean],
    })

    expect(cloneSummary(repo).note).toEqual({
      text: 'on main · couldn’t read',
      dirty: false,
      tone: 'danger',
      title: 'Couldn’t read the main checkout: could not read the status: exit status 128.',
    })
    expect(cloneSummary(worktreeRepo({ worktrees: [mainWorktree({ error: 'git refused it.' })] })).note?.title).toBe(
      'Couldn’t read the main checkout: git refused it.',
    )
  })

  it('has no note without a main checkout listed', () => {
    expect(cloneSummary(worktreeRepo({ worktrees: [openClean] })).note).toBeNull()
  })
})

describe('a worktree whose only changes are files missing from its folder', () => {
  const clone = worktreeRepo({ name: 'example-repo', displayPath: '~/projects/example-repo' })

  // halfRemoved is pull request #7's worktree, merged, at its head, with 17 files missing
  // from its folder, as a removal that stopped partway leaves it: finished, as the backend
  // stages it, and Remove… takes it.
  const halfRemoved = row({
    item: linkedItem(),
    stage: 'finished',
    canCleanUp: true,
    dirty: true,
    dirtyCount: 17,
    missingCount: 17,
  })

  const cause =
    'git counts a file missing from the folder as an uncommitted change; a removal that stopped partway leaves a folder like this.'

  // said is what the drawer says of wt in the example clone, with no session there.
  function said(wt: Worktree) {
    const s = safety(wt, clone, now, undefined, agentName)

    return { tone: s.tone, icon: s.icon, text: wordsText(s.text), also: wordsText(s.also) }
  }

  it('stands ready to clean up, or open, never at risk, its files a muted note', () => {
    const title =
      '17 files are missing from its folder, as a removal that stopped partway leaves them, but its last commit has each, so removing the worktree loses nothing'

    expect(laneOf(halfRemoved)).toBe('ready')
    expect(laneOf(row({ dirty: true, dirtyCount: 2, missingCount: 2 }))).toBe('open')
    expect(stateOf(halfRemoved)).toEqual({ glyph: 'ghost', tone: 'muted', words: title })
    expect(workNotes(halfRemoved)).toEqual([
      { kind: 'missing', tone: 'muted', text: '17 missing', sr: 'files: ', title },
    ])
    expect(workNotes(row({ dirty: true, dirtyCount: 1, missingCount: 1 }))[0]?.title).toBe(
      '1 file is missing from its folder, as a removal that stopped partway leaves them, but its last commit has it, so removing the worktree loses nothing',
    )
  })

  it('takes Remove…, with nothing for Force remove… to discard', () => {
    expect(removeWhy(halfRemoved, false, agentName)).toBe('')
    expect(removeWhy(halfRemoved, true, agentName)).toBe('Nothing to discard: Remove… takes it')
    expect(removeWhy({ ...halfRemoved, canRemove: false }, false, agentName)).toBe(
      'It can’t be removed right now: Check again',
    )
  })

  it.each([
    [
      'at its pull request’s head',
      halfRemoved,
      {
        tone: 'success',
        icon: 'shield',
        text: 'Safe to remove: the 17 files missing from its folder are in its last commit, and every commit is on GitHub (it’s at PR #7’s head).',
        also: cause,
      },
    ],
    [
      'with one file missing',
      row({ dirty: true, dirtyCount: 1, missingCount: 1, item: linkedItem({ headOid: 'elsewhere' }) }),
      {
        tone: 'success',
        icon: 'shield',
        text: 'Safe to remove: the file missing from its folder is in its last commit, and every commit is on GitHub.',
        also: cause,
      },
    ],
    [
      'beside commits not on GitHub',
      row({ dirty: true, dirtyCount: 2, missingCount: 2, branch: 'feat/retry', unpushed: 2, onGitHub: false }),
      {
        tone: 'warning',
        icon: 'up',
        text: '2 commits on feat/retry aren’t on GitHub.',
        also: 'Removing the worktree keeps that branch, so they stay in the clone; the app won’t delete it while they’re only here. 2 files are missing from its folder, but its last commit has each one, so removing the worktree loses none of them.',
      },
    ],
    [
      'whose commits couldn’t be counted',
      row({ dirty: true, dirtyCount: 2, missingCount: 2, unpushed: -1, onGitHub: false }),
      {
        tone: 'warning',
        icon: 'ring',
        text: 'Couldn’t count the commits that aren’t on GitHub, so removing it might lose some.',
        also: '2 files are missing from its folder, but its last commit has each one, so removing the worktree loses none of them.',
      },
    ],
    [
      'while Codex runs in it',
      row({ dirty: true, dirtyCount: 2, missingCount: 2, inUse: [codexHere], stage: 'in-use' }),
      {
        tone: 'info',
        icon: 'info',
        text: 'Can’t be removed while Codex runs in it (pid 60311).',
        also: 'Once it ends, nothing in it would be lost: every commit is on GitHub. 2 files are missing from its folder, but its last commit has each one, so removing the worktree loses none of them.',
      },
    ],
  ])('says removing one %s loses none of them', (_, wt, want) => {
    expect(said(wt)).toEqual(want)
  })

  it('counts a missing file beside a change of its own as uncommitted work, as before', () => {
    const mixed = row({ dirty: true, dirtyCount: 3, missingCount: 2, canRemove: false, canForceRemove: true })

    expect(laneOf(mixed)).toBe('risk')
    expect(stateOf(mixed).words).toBe('uncommitted changes')
    expect(workNotes(mixed)[0]?.kind).toBe('dirty')
    expect(removeWhy(mixed, false, agentName)).toBe('It has uncommitted files: Force remove… discards them')
    expect(said(mixed).text).toBe('Removing it would discard 3 uncommitted files, counting untracked ones.')
  })
})
