import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import type { AgentID, Worktree } from '../platform'
import { FIXED_NOW, isoAgo } from '../test/clock'
import {
  agentSession,
  copiedPR,
  copiedWorktree,
  details,
  item,
  linkedItem,
  mainWorktree,
  partlyCopiedWorktree,
  recordedWorktree,
  sessionsSnapshot,
  worktree,
  worktreeCleanup,
  worktreeCopies,
  worktreeRepo,
} from '../test/fixtures'
import {
  agentLine,
  agoWords,
  atItemHead,
  byHand,
  checksOf,
  clockOf,
  cloneOf,
  copiedOf,
  prOfCopies,
  copiedTag,
  facetOn,
  facetQuery,
  facetsOf,
  factsOf,
  fileStatus,
  folderOf,
  itemFacts,
  itemRef,
  kindOf,
  linkWords,
  liveSessions,
  lockInfo,
  madeBy,
  restoreCommand,
  running,
  runsIn,
  splitBranch,
  stateTones,
  stateWords,
  unpushedKnown,
  unpushedUnknown,
} from './worktreeParts'

const now = FIXED_NOW

const agentName = (id: AgentID) => (id === 'codex' ? 'Codex' : 'Claude')

// managed is a worktree's path under the clone root's managed root.
function managed(name: string, clone = 'example-repo'): string {
  return `~/projects/.${brand.slug}/worktrees/${clone}/${name}`
}

// row is a worktree the app made for pull request #7, open and clean.
function row(overrides: Partial<Worktree> = {}): Worktree {
  return worktree({
    legacy: false,
    displayPath: managed('pr-7'),
    stage: 'active',
    item: linkedItem({ state: 'open', closedAt: '' }),
    ...overrides,
  })
}

describe('the row’s predicates', () => {
  it.each([
    ['a process', row({ inUse: [{ pid: 1, command: 'node', agent: '', by: '' }] }), true],
    ['a live agent lock', row({ stage: 'in-use' }), true],
    ['nothing', row(), false],
  ])('knows something runs in a worktree: %s', (_, wt, want) => {
    expect(running(wt)).toBe(want)
  })

  it('knows a lock someone made by hand from an agent’s', () => {
    expect(byHand(row({ locked: true }))).toBe(true)
    expect(byHand(row({ locked: true, agentLock: true }))).toBe(false)
    expect(byHand(row())).toBe(false)
  })

  it.each([
    [row({ unpushed: 3, onGitHub: false }), 3, false],
    [row({ unpushed: 3, onGitHub: true }), 0, false],
    [row({ unpushed: 0 }), 0, false],
    [row({ unpushed: -1, onGitHub: false }), 0, true],
    [row({ unpushed: -1, onGitHub: false, prunable: true }), 0, false],
    [mainWorktree({ unpushed: -1, onGitHub: false }), 0, false],
  ])('counts the commits not on GitHub of %#, and knows when it couldn’t', (wt, known, unknown) => {
    expect(unpushedKnown(wt)).toBe(known)
    expect(unpushedUnknown(wt)).toBe(unknown)
  })

  it('names a worktree’s folder', () => {
    expect(folderOf(row())).toBe('pr-7')
    expect(folderOf(row({ displayPath: '' }))).toBe('')
  })

  it.each([
    ['at its pull request’s head', row({ item: linkedItem({ headOid: 'a'.repeat(40) }), head: 'a'.repeat(40) }), true],
    ['past it', row({ item: linkedItem({ headOid: 'a'.repeat(40) }), head: 'b'.repeat(40) }), false],
    ['for an item with no head', row({ item: linkedItem({ headOid: '' }), head: '' }), false],
    ['for no item', row({ item: null }), false],
  ])('knows a worktree %s', (_, wt, want) => {
    expect(atItemHead(wt)).toBe(want)
  })
})

describe('copiedOf', () => {
  it('says where commits on GitHub only as copies are, the pull request they went into, and as linked', () => {
    expect(copiedOf(copiedWorktree())).toEqual({
      n: 10,
      ref: 'origin/feature-x',
      branch: 'feature-x',
      pr: 42,
      item: copiedPR(),
      recorded: false,
      gone: false,
    })
  })

  it('names the branch as the backend tells it apart, on a remote whose name holds a slash', () => {
    const wt = copiedWorktree({
      copies: worktreeCopies({ ref: 'origin/mirror/main', branch: 'main', count: 10, identical: 10, pr: 42 }),
    })

    expect(copiedOf(wt)).toMatchObject({ ref: 'origin/mirror/main', branch: 'main' })
  })

  it('names the branch by its whole ref when no remote the clone names is its', () => {
    const wt = copiedWorktree({ copies: worktreeCopies({ ref: 'stranger/x', count: 10, identical: 10, pr: 42 }) })

    expect(copiedOf(wt)).toMatchObject({ branch: 'stranger/x' })
  })

  it('says copies a record vouches for, and whether their branch is gone', () => {
    expect(copiedOf(recordedWorktree())).toMatchObject({
      n: 10,
      pr: 42,
      item: { number: 42, state: 'merged' },
      recorded: true,
      gone: true,
    })
    expect(
      copiedOf(
        recordedWorktree({
          copies: worktreeCopies({
            ref: 'origin/feature-x',
            branch: 'feature-x',
            count: 10,
            identical: 10,
            recorded: true,
            pr: 42,
          }),
        }),
      ),
    ).toMatchObject({ recorded: true, gone: false })
  })

  it.each([
    ['linked by its name', copiedWorktree({ item: linkedItem(), linkedBy: 'name' }), 42],
    ['linked to nothing', copiedWorktree({ item: null, linkedBy: '' }), 42],
    ['linked by copies, its item not read', copiedWorktree({ item: null }), 42],
    [
      'linked by copies to another pull request than the one its branch heads',
      copiedWorktree({
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10, pr: 43 }),
      }),
      43,
    ],
    [
      'whose branch no pull request is known to head',
      copiedWorktree({
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10 }),
      }),
      0,
    ],
  ])('names the pull request copies went into, by its number alone, for one %s', (_, wt, pr) => {
    expect(copiedOf(wt)).toMatchObject({ pr, item: null })
  })

  it.each([
    ['the backend counts them not on GitHub', copiedWorktree({ onGitHub: false })],
    ['every commit is on GitHub by its ID', copiedWorktree({ unpushed: 0 })],
    ['its commits couldn’t be counted', copiedWorktree({ unpushed: -1 })],
    ['the comparison counted other commits', copiedWorktree({ unpushed: 11 })],
    [
      'some have no identical copy',
      copiedWorktree({
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 9, edited: 1 }),
      }),
    ],
    [
      'the backend says why they don’t count',
      copiedWorktree({
        copies: worktreeCopies({ ref: 'origin/feature-x', branch: 'feature-x', count: 10, identical: 10, note: 'No.' }),
      }),
    ],
    ['no branch has them', copiedWorktree({ copies: worktreeCopies({ count: 10, identical: 10 }) })],
    ['it’s at its pull request’s head', copiedWorktree({ item: copiedPR({ headOid: copiedWorktree().head }) })],
  ])('says nothing of copies when %s', (_, wt) => {
    expect(copiedOf(wt)).toBeNull()
  })

  it('names the pull request copies went into, or says it has one when its number isn’t known', () => {
    expect(prOfCopies({ n: 1, ref: 'origin/x', branch: 'x', pr: 42, item: null, recorded: true, gone: true })).toBe(
      'PR #42',
    )
    expect(prOfCopies({ n: 1, ref: 'origin/x', branch: 'x', pr: 0, item: null, recorded: true, gone: true })).toBe(
      'its pull request',
    )
  })
})

describe('how a worktree is linked', () => {
  it.each([
    [
      'one linked by its copies, the pull request holding them',
      copiedWorktree(),
      'PR #42 is the pull request its commits were copied onto',
    ],
    [
      'one linked by its copies, the pull request no longer holding them all',
      copiedWorktree({ onGitHub: false }),
      'Its commits were copied onto PR #42’s branch, but PR #42 doesn’t hold them all now',
    ],
    ['one linked by its name', row(), ''],
    ['one linked by its branch', row({ linkedBy: 'branch' }), ''],
    ['one linked by its copies, its item not read', copiedWorktree({ item: null }), ''],
  ])('words the copied tag of %s', (_, wt, want) => {
    expect(copiedTag(wt)).toBe(want)
  })

  it('says how each kind of link names its item', () => {
    expect(linkWords).toEqual({
      '': 'Made for',
      name: 'Made for',
      branch: 'On the branch of',
      copy: 'Its commits were copied onto',
    })
  })
})

describe('the cells', () => {
  it('writes a time of day in 24 hours', () => {
    expect(clockOf('2026-06-15T09:05:00Z')).toBe('09:05')
    expect(clockOf('2026-06-15T23:41:59Z')).toBe('23:41')
  })

  it.each([
    ['worktree-pr-214', ['worktree-', 'pr-214']],
    ['feat/retry', ['feat/', 'retry']],
    ['dependabot/github_actions/actions/setup-go-6', ['dependabot/github_actions/actions/', 'setup-go-6']],
    ['main', ['', 'main']],
  ])('splits %j into what gives way and the name', (branch, want) => {
    expect(splitBranch(branch)).toEqual(want)
  })

  it('names a pull request or an issue', () => {
    expect(itemRef(linkedItem({ number: 214 }))).toBe('PR #214')
    expect(itemRef(linkedItem({ kind: 'issue', number: 88 }))).toBe('Issue #88')
  })

  it.each([
    ['an open pull request', linkedItem({ state: 'open', closedAt: '' }), false, 'pr', 'success', 'Pull request, open'],
    ['a draft', linkedItem({ state: 'open', closedAt: '' }), true, 'pr', 'muted', 'Draft pull request'],
    ['a merged one', linkedItem(), false, 'prMerged', 'info', 'Pull request, merged 2h ago'],
    [
      'one closed without merging',
      linkedItem({ state: 'closed' }),
      false,
      'prClosed',
      'danger',
      'Pull request, closed without merging 2h ago',
    ],
    [
      'an open issue',
      linkedItem({ kind: 'issue', state: 'open', closedAt: '' }),
      false,
      'issue',
      'success',
      'Issue, open',
    ],
    [
      'a closed issue',
      linkedItem({ kind: 'issue', state: 'closed' }),
      false,
      'issueClosed',
      'info',
      'Issue, closed 2h ago',
    ],
    ['a merged one, when unknown', linkedItem({ closedAt: '' }), false, 'prMerged', 'info', 'Pull request, merged'],
    [
      'one closed without merging, when unknown',
      linkedItem({ state: 'closed', closedAt: '' }),
      false,
      'prClosed',
      'danger',
      'Pull request, closed without merging',
    ],
    [
      'a closed issue, when unknown',
      linkedItem({ kind: 'issue', state: 'closed', closedAt: '' }),
      false,
      'issueClosed',
      'info',
      'Issue, closed',
    ],
  ])('marks %s', (_, linked, draft, icon, tone, words) => {
    expect(kindOf(linked, draft, now)).toEqual({ icon, tone, words })
  })

  it('says how long ago something was, after a space, or nothing when it isn’t known', () => {
    expect(agoWords(now, '2026-06-15T10:00:00Z')).toBe(' 2h ago')
    expect(agoWords(now, '2026-06-15T12:00:00Z')).toBe(' just now')
    expect(agoWords(now, '')).toBe('')
  })

  it.each([
    ['SUCCESS', { tone: 'success', words: 'Checks passed', pending: false }],
    ['FAILURE', { tone: 'danger', words: 'Checks failing', pending: false }],
    ['ERROR', { tone: 'danger', words: 'Checks failing', pending: false }],
    ['PENDING', { tone: 'warning', words: 'Checks running', pending: true }],
    ['EXPECTED', null],
    ['', null],
  ])('dots checks that are %j', (state, want) => {
    expect(checksOf(state)).toEqual(want)
  })

  it.each([
    [' M', 'M', 'modified', false, 'Modified (git status “ M”)'],
    ['M ', 'M', 'modified', true, 'Modified, staged (git status “M ”)'],
    ['MM', 'M', 'modified', true, 'Modified, partly staged (git status “MM”)'],
    ['A ', 'A', 'added', true, 'Added, staged (git status “A ”)'],
    [' D', 'D', 'deleted', false, 'Deleted (git status “ D”)'],
    ['R ', 'R', 'renamed', true, 'Renamed, staged (git status “R ”)'],
    ['??', 'U', 'untracked: new, not added', false, 'Untracked: new, not added (git status “??”)'],
    ['UU', 'C', 'unmerged: a conflict', true, 'Unmerged: a conflict, partly staged (git status “UU”)'],
    [' U', 'C', 'unmerged: a conflict', false, 'Unmerged: a conflict (git status “ U”)'],
    ['AA', 'C', 'unmerged: a conflict', true, 'Unmerged: a conflict, partly staged (git status “AA”)'],
    ['DD', 'C', 'unmerged: a conflict', true, 'Unmerged: a conflict, partly staged (git status “DD”)'],
    ['C ', 'C', 'changed', true, 'Changed, staged (git status “C ”)'],
    [' T', 'T', 'changed', false, 'Changed (git status “ T”)'],
  ])('reads the status %j', (code, letter, words, staged, title) => {
    expect(fileStatus({ code, path: 'web/src/lib/clipboard.ts' })).toEqual({
      letter,
      words,
      staged,
      title,
      dir: 'web/src/lib/',
      name: 'clipboard.ts',
    })
  })

  it('splits a file at the top of the worktree into no folder and its name', () => {
    expect(fileStatus({ code: ' M', path: 'README.md' })).toMatchObject({ dir: '', name: 'README.md' })
  })
})

describe('lockInfo', () => {
  it.each([
    [
      'claude session pr-252 (pid 70113 start Mon Jun 15 11:30:03 2026)',
      { session: 'pr-252', pid: 70113, start: '2026-06-15T11:30:03.000Z' },
    ],
    [
      'claude agent inspect point (pid 4242 start Fri Nov  6 17:14:26 2026)',
      { session: 'inspect point', pid: 4242, start: '2026-11-06T17:14:26.000Z' },
    ],
    ['claude session pr-7 (pid 4242)', { session: 'pr-7', pid: 4242, start: '' }],
    ['claude session pr-7 (pid 4242 start yesterday)', { session: 'pr-7', pid: 4242, start: '' }],
    ['claude session pr-7 (pid 4242 start Mon Foo 15 11:30:03 2026)', { session: 'pr-7', pid: 4242, start: '' }],
    ['kept for the release branch', null],
    ['', null],
  ])('reads the lock %j, its start in the machine’s own time', (lockReason, want) => {
    expect(lockInfo(row({ locked: true, lockReason }))).toEqual(want)
  })
})

describe('agentLine', () => {
  const claudeHere = { pid: 47890, command: '2.1.328', agent: 'claude', by: '' } as const
  const followed = agentSession({ state: 'needs_input', launchedAt: isoAgo({ m: 40 }) })

  it.each([
    [
      'a routine’s agent, as the session followed there stands',
      row({ inUse: [{ pid: 61022, command: '2.1.328', agent: 'claude', by: '⚡ Fix checks' }] }),
      agentSession({ state: 'working', launchedAt: isoAgo({ m: 3 }) }),
      {
        kind: 'routine',
        name: 'Claude',
        version: '2.1.328',
        pid: 61022,
        by: 'Fix checks',
        state: 'working',
        since: isoAgo({ m: 3 }),
      },
    ],
    [
      'a routine’s agent before its session is followed',
      row({ inUse: [{ pid: 0, command: 'codex', agent: 'codex', by: '⚡Triage' }] }),
      undefined,
      { kind: 'routine', name: 'Codex', version: '', pid: 0, by: 'Triage', state: '', since: '' },
    ],
    [
      'a session the app follows',
      row({ inUse: [claudeHere] }),
      followed,
      {
        kind: 'live',
        name: 'Claude',
        version: '2.1.328',
        pid: 47890,
        followed: true,
        state: 'needs_input',
        since: isoAgo({ m: 40 }),
      },
    ],
    [
      'a session the app follows, not yet listed',
      row({ inUse: [{ pid: 60311, command: 'codex', agent: 'codex', by: '' }] }),
      undefined,
      { kind: 'live', name: 'Codex', version: '', pid: 60311, followed: true, state: '', since: '' },
    ],
    [
      'Claude Code started outside the app, by its version',
      row({ inUse: [{ ...claudeHere, agent: '' }] }),
      followed,
      { kind: 'live', name: 'Claude', version: '2.1.328', pid: 47890, followed: false, state: '', since: '' },
    ],
    [
      'another program',
      row({ inUse: [{ pid: 7, command: 'node', agent: '', by: '' }] }),
      undefined,
      { kind: 'live', name: 'node', version: '', pid: 7, followed: false, state: '', since: '' },
    ],
    [
      'the agent’s process over a shell before it',
      row({
        inUse: [
          { pid: 6, command: 'zsh', agent: '', by: '' },
          { pid: 8, command: 'codex', agent: 'codex', by: '' },
        ],
      }),
      undefined,
      { kind: 'live', name: 'Codex', version: '', pid: 8, followed: true, state: '', since: '' },
    ],
    [
      'a live lock with no process listed',
      row({
        locked: true,
        agentLock: true,
        lockReason: 'claude session pr-252 (pid 70113 start Mon Jun 15 11:30:03 2026)',
      }),
      undefined,
      { kind: 'lock', name: 'Claude', pid: 70113, since: '2026-06-15T11:30:03.000Z' },
    ],
    [
      'a live lock it can’t read',
      row({ locked: true, agentLock: true, lockReason: 'claude' }),
      undefined,
      { kind: 'lock', name: 'Claude', pid: 0, since: '' },
    ],
    [
      'the last launch, past an ended session’s lock',
      row({ locked: true, agentLock: true, staleLock: true, agent: 'claude', lastLaunchedAt: isoAgo({ m: 265 }) }),
      undefined,
      { kind: 'past', name: 'Claude', at: isoAgo({ m: 265 }) },
    ],
    ['nothing, never launched there', row(), undefined, null],
    ['nothing, with no launch time', row({ agent: 'codex' }), undefined, null],
  ])('reads %s', (_, wt, session, want) => {
    expect(agentLine(wt, session, agentName)).toEqual(want)
  })

  it('words a session’s states as a row shows them, colored', () => {
    expect(stateWords.unknown).toBe('running')
    expect(stateWords.needs_approval).toBe('needs approval')
    expect([stateTones.working, stateTones.needs_input, stateTones.idle, stateTones.failed]).toEqual([
      'info',
      'warning',
      'success',
      'danger',
    ])
  })

  it.each([
    ['nothing it knows of', row({ stage: 'in-use' }), 'a session runs in it'],
    [
      'a routine’s agent',
      row({ inUse: [{ pid: 1, command: 'codex', agent: 'codex', by: '⚡ Fix checks' }] }),
      '⚡ Fix checks runs its agent in it',
    ],
    [
      'a live lock',
      row({ locked: true, agentLock: true, lockReason: '' }),
      'the Claude session that locked it still runs',
    ],
    ['a program', row({ inUse: [{ pid: 1, command: 'node', agent: '', by: '' }] }), 'node runs in it'],
    ['the last launch', row({ stage: 'in-use', agent: 'codex', lastLaunchedAt: isoAgo({ m: 5 }) }), 'Codex runs in it'],
  ])('says what runs in it: %s', (_, wt, want) => {
    expect(runsIn(wt, agentName)).toBe(want)
  })

  it.each([
    ['Claude Code', row({ legacy: true }), 'Claude Code (legacy), in the clone’s .claude/worktrees'],
    ['a routine', row({ inUse: [{ pid: 1, command: 'codex', agent: 'codex', by: '⚡ Fix checks' }] }), '⚡ Fix checks'],
    ['the app', row(), brand.name],
    ['someone else', row({ managed: false }), 'outside the app'],
  ])('says %s made it', (_, wt, want) => {
    expect(madeBy(wt)).toBe(want)
  })
})

describe('the window’s joins', () => {
  it('maps each item the inbox lists to what it read of it, and finds a worktree’s', () => {
    const pr = item()
    const issue = item({
      id: 2,
      kind: 'issue',
      number: 88,
      repo: 'Acme-Org/API-Gateway',
      details: details({
        checks: { ...details().checks, state: 'FAILURE' },
        reviews: { ...details().reviews, decision: 'CHANGES_REQUESTED' },
      }),
    })
    const draft = item({ id: 3, number: 244, isDraft: true })
    const facts = itemFacts([pr, issue, draft])

    expect([...facts.keys()]).toEqual([
      'acme-org/example-repo#7',
      'acme-org/api-gateway#88',
      'acme-org/example-repo#244',
    ])
    expect(factsOf(facts, linkedItem())).toEqual({ item: pr, checks: '', review: '', draft: false })
    expect(factsOf(facts, linkedItem({ kind: 'issue', name: 'api-gateway', number: 88 }))).toEqual({
      item: issue,
      checks: 'FAILURE',
      review: 'CHANGES_REQUESTED',
      draft: false,
    })
    expect(factsOf(facts, linkedItem({ number: 244 }))?.draft).toBe(true)
    expect(factsOf(facts, linkedItem({ number: 99 }))).toBeUndefined()
    expect(factsOf(facts, null)).toBeUndefined()
  })

  it('maps each folder to the newest live session the app follows there', () => {
    const dir = managed('pr-7')
    const newest = agentSession({ id: 'a', dir, state: 'working' })
    const sessions = [
      newest,
      agentSession({ id: 'b', dir, state: 'idle' }),
      agentSession({ id: 'c', dir: managed('pr-8'), state: 'ended' }),
      agentSession({ id: 'd', dir: '', state: 'working' }),
      agentSession({ id: 'e', dir: managed('issue-9'), state: 'unknown' }),
    ]

    expect(liveSessions(sessionsSnapshot(sessions))).toEqual(
      new Map([
        [dir, newest],
        [managed('issue-9'), sessions[4]],
      ]),
    )
    expect(liveSessions(sessionsSnapshot(sessions, false)).size).toBe(0)
    expect(liveSessions(null).size).toBe(0)
  })
})

describe('the restore command', () => {
  it.each([
    [managed('pr-201'), '~/projects/example-repo'],
    ['/srv/code/.' + brand.slug + '/worktrees/api/pr-1', '/srv/code/api'],
    ['~/projects/example-repo/.claude/worktrees/pr-7', '~/projects/example-repo'],
    ['~/projects/example-repo-hotfix', ''],
    ['~/projects/.other/worktrees/example-repo/pr-7', ''],
    ['pr-7', ''],
  ])('reads the clone of %j', (displayPath, want) => {
    expect(cloneOf(displayPath)).toBe(want)
  })

  it.each([
    [
      'with the branch deleted with it',
      worktreeCleanup({ displayPath: managed('pr-201'), deletedBranch: 'worktree-pr-201', head: 'a'.repeat(40) }),
      `git -C ~/projects/example-repo worktree add -b worktree-pr-201 ${managed('pr-201')} ${'a'.repeat(40)}`,
    ],
    [
      'whose branch was kept',
      worktreeCleanup({ deletedBranch: '' }),
      'git -C ~/projects/example-repo worktree add ~/projects/example-repo/.claude/worktrees/pr-7 fc1c3975011e66491785ace891c893e4fc716587',
    ],
    [
      'detached, whose several branches were deleted with it',
      worktreeCleanup({ deletedBranch: 'worktree-pr-7, feature-x' }),
      'git -C ~/projects/example-repo worktree add ~/projects/example-repo/.claude/worktrees/pr-7 fc1c3975011e66491785ace891c893e4fc716587',
    ],
    ['made outside the app’s folders, with no command', worktreeCleanup({ displayPath: '~/projects/hotfix' }), ''],
    [
      'in folders and on a branch whose names need quotes',
      worktreeCleanup({ displayPath: '~/projects/My Repo/.claude/worktrees/pr 7', deletedBranch: "it's" }),
      `git -C ~/'projects/My Repo' worktree add -b 'it'\\''s' ~/'projects/My Repo/.claude/worktrees/pr 7' fc1c3975011e66491785ace891c893e4fc716587`,
    ],
  ])('brings back a cleaned-up worktree %s', (_, cleanup, want) => {
    expect(restoreCommand(cleanup)).toBe(want)
  })
})

describe('the chips', () => {
  const repos = [
    worktreeRepo({
      worktrees: [
        mainWorktree({ dirty: true, dirtyCount: 4 }),
        row({ inUse: [{ pid: 1, command: 'codex', agent: 'codex', by: '' }], stage: 'in-use' }),
        row({ dirtyCount: 2, unpushed: 2, onGitHub: false }),
        row({ dirtyCount: 1, stage: 'needs-review' }),
        row({ stage: 'finished' }),
        row({ unpushed: -1, onGitHub: false }),
      ],
    }),
    worktreeRepo({ name: 'api-gateway', worktrees: [mainWorktree(), row({ stage: 'finished' })] }),
  ]

  it('counts each state across every clone’s linked worktrees, leaving out the empty ones', () => {
    expect(facetsOf(repos, '')).toEqual([
      {
        key: 'in-use',
        label: 'running',
        tone: 'info',
        n: 1,
        on: false,
        title: 'Show the 1 worktree that something runs in (is:in-use)',
      },
      {
        key: 'dirty',
        label: 'uncommitted',
        tone: 'warning',
        n: 2,
        on: false,
        title: 'Show the 2 worktrees that have uncommitted files (is:dirty)',
      },
      {
        key: 'unpushed',
        label: 'not on GitHub',
        tone: 'caution',
        n: 1,
        on: false,
        title: 'Show the 1 worktree that has commits that aren’t on GitHub (is:unpushed)',
      },
      {
        key: 'review',
        label: 'needs a look',
        tone: 'warning',
        n: 1,
        on: false,
        title: 'Show the 1 worktree that is done on GitHub but holds work that isn’t (is:review)',
      },
      {
        key: 'finished',
        label: 'ready to clean up',
        tone: 'success',
        n: 2,
        on: false,
        title: 'Show the 2 worktrees that Clean up removes without losing anything (is:finished)',
      },
    ])
    expect(facetsOf([worktreeRepo({ worktrees: [mainWorktree({ dirtyCount: 3 }), row()] })], '')).toEqual([])
  })

  it('words each state of one worktree and of several', () => {
    const several = [
      worktreeRepo({
        worktrees: [
          row({ inUse: [{ pid: 1, command: 'node', agent: '', by: '' }], dirtyCount: 1, unpushed: 1, onGitHub: false }),
          row({ stage: 'in-use', unpushed: 1, onGitHub: false }),
          row({ stage: 'needs-review' }),
          row({ stage: 'needs-review' }),
          row({ stage: 'finished' }),
        ],
      }),
    ]

    expect(facetsOf(several, '').map((c) => c.title)).toEqual([
      'Show the 2 worktrees that something runs in (is:in-use)',
      'Show the 1 worktree that has uncommitted files (is:dirty)',
      'Show the 2 worktrees that have commits that aren’t on GitHub (is:unpushed)',
      'Show the 2 worktrees that are done on GitHub but hold work that isn’t (is:review)',
      'Show the 1 worktree that Clean up removes without losing anything (is:finished)',
    ])
  })

  it('counts as not on GitHub commits with only an edited copy, never those on GitHub as copies', () => {
    const copies = [
      worktreeRepo({ worktrees: [mainWorktree(), copiedWorktree(), recordedWorktree(), partlyCopiedWorktree()] }),
    ]

    expect(facetsOf(copies, '').map((c) => [c.key, c.n])).toEqual([
      ['unpushed', 1],
      ['finished', 1],
    ])
  })

  it('marks the chip the filter is alone, which a click clears', () => {
    const chips = facetsOf(repos, ' is:dirty ')

    expect(chips.filter((c) => c.on).map((c) => [c.key, c.title])).toEqual([['dirty', 'Clear the filter']])
    expect(facetOn(' is:dirty ', 'dirty')).toBe(true)
    expect(facetOn('is:dirty is:pr', 'dirty')).toBe(false)
    expect(facetQuery('is:dirty', 'dirty')).toBe('')
    expect(facetQuery('', 'dirty')).toBe('is:dirty')
    expect(facetQuery('is:review', 'unpushed')).toBe('is:unpushed')
  })
})
