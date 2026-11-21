import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { useWorktreeDetails, type WorktreeDetailsState } from '../../lib/useWorktreeDetails'
import type { RowWork } from '../../lib/useWorktrees'
import type { ItemFacts } from '../../lib/worktreeParts'
import type { Lane } from '../../lib/worktrees'
import type { AgentID, AgentSession, LinkedItem, Worktree, WorktreeDetails, WorktreeRepo } from '../../platform'
import { deferred } from '../../test/async'
import { at, FIXED_NOW } from '../../test/clock'
import {
  agentSession,
  copiedWorktree,
  item,
  linkedItem,
  partlyCopiedWorktree,
  worktree,
  worktreeCopies,
  worktreeDetails,
  worktreeFiles,
  worktreeRepo,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import type { LaunchPlace } from '../LaunchControls'
import { launchPlace } from './layout'
import { WorktreeLine, type WorktreeLineProps } from './WorktreeLine'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// The worktrees the tests draw: the mock's gallery, one of each state, at the tests' clock.

const now = FIXED_NOW
const minute = 60
const hour = 60 * minute
const day = 24 * hour

// ago is the time s seconds before the clock, and unixAgo the same in Unix seconds.
function ago(s: number): string {
  return new Date(now.getTime() - s * 1000).toISOString()
}

function unixAgo(s: number): number {
  return Math.floor(now.getTime() / 1000) - s
}

const agentName = (id: AgentID) => (id === 'codex' ? 'Codex' : 'Claude')

// managed and legacy are a worktree's path under the managed root, and in a clone's
// .claude/worktrees.
function managed(name: string, clone = 'example-repo'): string {
  return `~/projects/.${brand.slug}/worktrees/${clone}/${name}`
}

function legacy(name: string, clone = 'example-repo'): string {
  return `~/projects/${clone}/.claude/worktrees/${name}`
}

// pr and issue are a pull request and an issue of example-repo, open unless said.
function pr(number: number, title: string, overrides: Partial<LinkedItem> = {}): LinkedItem {
  return linkedItem({
    number,
    title,
    url: `https://github.com/acme-org/example-repo/pull/${String(number)}`,
    state: 'open',
    closedAt: '',
    headOid: '',
    ...overrides,
  })
}

function issue(number: number, title: string, overrides: Partial<LinkedItem> = {}): LinkedItem {
  return pr(number, title, {
    kind: 'issue',
    url: `https://github.com/acme-org/example-repo/issues/${String(number)}`,
    ...overrides,
  })
}

// row is a worktree the app made for a pull request, clean, on its own branch.
function row(name: string, overrides: Partial<Worktree> = {}): Worktree {
  return worktree({
    path: `/home/user/projects/.${brand.slug}/worktrees/example-repo/${name}`,
    displayPath: managed(name),
    branch: `worktree-${name}`,
    legacy: false,
    stage: 'active',
    lastCommitUnix: unixAgo(3 * hour),
    ...overrides,
  })
}

// facts is what the inbox read of an item it lists.
function facts(checks: string, overrides: Partial<ItemFacts> = {}): ItemFacts {
  return { item: item(), checks, review: '', draft: false, ...overrides }
}

// session is the session the app follows in a worktree, started s seconds ago.
function session(state: AgentSession['state'], s: number): AgentSession {
  return agentSession({ state, launchedAt: ago(s) })
}

const er = worktreeRepo()
const ag = worktreeRepo({
  path: '/home/user/projects/api-gateway',
  displayPath: '~/projects/api-gateway',
  name: 'api-gateway',
  hasStale: true,
})
const df = worktreeRepo({ path: '/home/user/projects/dotfiles', displayPath: '~/projects/dotfiles', name: 'dotfiles' })

const claudeHere = { pid: 47890, command: '2.1.328', agent: 'claude', by: '' } as const

const inspect = row('inspect-point', {
  path: '/home/user/projects/example-repo/.claude/worktrees/inspect-point',
  displayPath: legacy('inspect-point'),
  branch: 'feat/inspect-point',
  legacy: true,
  locked: true,
  agentLock: true,
  lockReason: 'claude session inspect-point (pid 47890 start Mon Jun 15 11:35:12 2026)',
  inUse: [claudeHere],
  dirty: true,
  dirtyCount: 30,
  files: worktreeFiles(12),
  unpushed: 1,
  onGitHub: false,
  lastCommitUnix: unixAgo(25 * minute),
  item: pr(192, 'Inspect the point under the cursor'),
  agent: 'claude',
  lastLaunchedAt: ago(12 * minute),
  launches: 3,
  stage: 'in-use',
  canRemove: false,
})
const codexRun = row('issue-91', {
  item: issue(91, 'Paste loses the trailing newline'),
  inUse: [{ pid: 60311, command: 'codex', agent: 'codex', by: '' }],
  agent: 'codex',
  lastLaunchedAt: ago(6 * minute),
  dirty: true,
  dirtyCount: 2,
  files: worktreeFiles(2),
  stage: 'in-use',
  canRemove: false,
})
const routine = row('pr-233', {
  item: pr(233, 'Bump the linter and fix what it finds'),
  inUse: [{ pid: 61022, command: '2.1.328', agent: 'claude', by: '⚡ Fix checks' }],
  dirty: true,
  dirtyCount: 3,
  stage: 'in-use',
  canRemove: false,
})
const issue88 = row('issue-88', {
  displayPath: managed('issue-88', 'api-gateway'),
  item: issue(88, 'Rate limits reset at the wrong hour', { name: 'api-gateway' }),
  inUse: [{ pid: 51207, command: 'codex', agent: 'codex', by: '' }],
  agent: 'codex',
  lastLaunchedAt: ago(40 * minute),
  dirty: true,
  dirtyCount: 4,
  stage: 'in-use',
  canRemove: false,
})
const pr209 = row('pr-209', {
  item: pr(209, 'Drop the v1 config loader', { state: 'merged', closedAt: ago(26 * hour) }),
  stage: 'finished',
  canCleanUp: true,
  agent: 'codex',
  lastLaunchedAt: ago(2 * day + 90 * minute),
  launches: 1,
})
const closedIssue = row('issue-64', {
  item: issue(64, 'Document the config precedence', { state: 'closed', closedAt: ago(5 * hour) }),
  stage: 'finished',
  canCleanUp: true,
  agent: 'codex',
  lastLaunchedAt: ago(8 * hour),
})
const pr131 = row('pr-131', {
  item: pr(131, 'Keep the cursor where it was on reload', { state: 'merged', closedAt: ago(6 * day) }),
  unpushed: 2,
  onGitHub: false,
  stage: 'needs-review',
  agent: 'claude',
  lastLaunchedAt: ago(7 * day + 30 * minute),
})
const mergedDirty = row('pr-150', {
  item: pr(150, 'Cache the avatar reads', { state: 'merged', closedAt: ago(2 * day) }),
  dirty: true,
  dirtyCount: 3,
  stage: 'needs-review',
  canRemove: false,
  canForceRemove: true,
  agent: 'claude',
  lastLaunchedAt: ago(2 * hour),
})
const pr214 = row('pr-214', {
  item: pr(214, 'Retry the upload when the token expires'),
  agent: 'claude',
  lastLaunchedAt: ago(2 * hour),
  launches: 2,
})
const both = row('pr-221', {
  item: pr(221, 'Remember the last ten searches'),
  dirty: true,
  dirtyCount: 7,
  unpushed: 2,
  onGitHub: false,
  canRemove: false,
  canForceRemove: true,
  agent: 'claude',
  lastLaunchedAt: ago(65 * minute),
})
const draft = row('pr-244', { item: pr(244, 'Try a virtualized file list'), agent: 'claude', lastLaunchedAt: ago(day) })
const dirtyOnly = row('pr-246', {
  item: pr(246, 'Add the keyboard shortcuts sheet'),
  dirty: true,
  dirtyCount: 5,
  canRemove: false,
  canForceRemove: true,
})
const upOnly = row('pr-247', { item: pr(247, 'Group the notifications by repository'), unpushed: 3, onGitHub: false })
const unknown = row('pr-249', {
  item: pr(249, 'Read the proxy settings from the system'),
  unpushed: -1,
  onGitHub: false,
})
const many = row('pr-248', {
  item: pr(248, 'Move the build to the new bundler'),
  dirty: true,
  dirtyCount: 1204,
  canRemove: false,
  canForceRemove: true,
})
const liveLock = row('pr-252', {
  item: pr(252, 'Show the merge queue position'),
  locked: true,
  agentLock: true,
  lockReason: 'claude session pr-252 (pid 70113 start Mon Jun 15 11:30:03 2026)',
  stage: 'in-use',
  dirty: true,
  dirtyCount: 1,
  canRemove: false,
})
const staleLock = row('pr-253', {
  item: pr(253, 'Retry a stuck merge once'),
  locked: true,
  agentLock: true,
  staleLock: true,
  canUnlock: true,
  lockReason: 'claude session pr-253 (pid 66120 start Mon Jun 15 07:14:09 2026)',
  canRemove: false,
  agent: 'claude',
  lastLaunchedAt: ago(4 * hour + 25 * minute),
})
const handLocked = row('pr-254', {
  item: pr(254, 'Pin the release branch'),
  locked: true,
  lockReason: 'kept for the release branch',
  stage: '',
  canRemove: false,
})
const pr98 = row('pr-98', {
  displayPath: managed('pr-98', 'api-gateway'),
  prunable: true,
  prunableReason: 'gitdir file points to non-existent location',
  unpushed: -1,
  onGitHub: false,
  stage: '',
  canRemove: false,
  lastCommitUnix: 0,
})
const erred = row('pr-255', {
  item: pr(255, 'Speed up the status read'),
  error: 'git status refused: this clone’s own git config sets core.fsmonitor, which isn’t run',
  unpushed: -1,
  onGitHub: false,
  lastCommitUnix: 0,
  branches: [],
  canRemove: false,
})
const experiment = row('dotfiles-experiment', {
  displayPath: '~/projects/dotfiles-experiment',
  managed: false,
  detached: true,
  branch: '',
  head: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678',
  stage: '',
  lastCommitUnix: unixAgo(12 * day),
  branches: [],
})
const assignees = row('pr-assignees', {
  displayPath: legacy('pr-assignees'),
  branch: 'feat/pr-assignees',
  legacy: true,
  dirty: true,
  dirtyCount: 69,
  stage: '',
  canRemove: false,
  canForceRemove: true,
  lastCommitUnix: unixAgo(4 * day),
})
const outside = row('example-repo-hotfix', {
  displayPath: '~/projects/example-repo-hotfix',
  managed: false,
  branch: 'hotfix/login-loop',
  stage: '',
  lastCommitUnix: unixAgo(9 * day),
  branches: [],
})
const dependabot = row('pr-260', {
  branch: 'dependabot/github_actions/actions/setup-go-6',
  ownBranch: false,
  item: pr(260, 'chore(deps): bump actions/setup-go from 5 to 6'),
})

// GalleryCase is one of the mock's gallery's rows, with what its row should say: the
// lane its edge colors, its state's words, its work, its agent, its verb, and whether ›_
// opens it in Terminal.
interface GalleryCase {
  wt: Worktree
  repo?: WorktreeRepo
  facts?: ItemFacts
  session?: AgentSession
  lane: Lane | null
  state: string
  work: string[]
  agent: string | null
  verb: string | null
  terminal: boolean
}

const clean = 'clean, and every commit is on GitHub'

const gallery: [string, GalleryCase][] = [
  [
    'Running · Claude',
    {
      wt: inspect,
      session: session('working', 12 * minute),
      lane: 'running',
      state: 'something runs in it',
      work: ['30 files', '1'],
      agent: 'Claude 2.1.328 · working · 12m',
      verb: null,
      terminal: true,
    },
  ],
  [
    'Running · Codex',
    {
      wt: codexRun,
      session: session('working', 6 * minute),
      lane: 'running',
      state: 'something runs in it',
      work: ['2 files'],
      agent: 'Codex · working · 6m',
      verb: null,
      terminal: true,
    },
  ],
  [
    'Running · an Autopilot routine',
    {
      wt: routine,
      facts: facts('FAILURE'),
      session: session('working', 3 * minute),
      lane: 'running',
      state: 'something runs in it',
      work: ['3 files'],
      agent: 'Fix checks · in the background',
      verb: null,
      terminal: true,
    },
  ],
  [
    'A session that needs input',
    {
      wt: issue88,
      repo: ag,
      session: session('needs_input', 40 * minute),
      lane: 'running',
      state: 'something runs in it',
      work: ['4 files'],
      agent: 'Codex · needs input · 40m',
      verb: null,
      terminal: true,
    },
  ],
  [
    'Ready to clean up · PR merged',
    {
      wt: pr209,
      lane: 'ready',
      state: clean,
      work: [],
      agent: 'Codex · 2d ago',
      verb: 'Clean up',
      terminal: true,
    },
  ],
  [
    'Ready to clean up · issue closed',
    {
      wt: closedIssue,
      lane: 'ready',
      state: clean,
      work: [],
      agent: 'Codex · 8h ago',
      verb: 'Clean up',
      terminal: true,
    },
  ],
  [
    'Needs a look · merged, commits not on GitHub',
    {
      wt: pr131,
      lane: 'review',
      state: 'commits that aren’t on GitHub',
      work: ['2'],
      agent: 'Claude · 1w ago',
      verb: null,
      terminal: true,
    },
  ],
  [
    'Needs a look · merged, uncommitted files',
    {
      wt: mergedDirty,
      lane: 'review',
      state: 'uncommitted changes',
      work: ['3 files'],
      agent: 'Claude · 2h ago',
      verb: null,
      terminal: true,
    },
  ],
  [
    'Open PR, clean',
    {
      wt: pr214,
      facts: facts('SUCCESS', { review: 'APPROVED' }),
      lane: null,
      state: clean,
      work: [],
      agent: 'Claude · 2h ago',
      verb: 'Open in Claude',
      terminal: true,
    },
  ],
  [
    'Open, with changes',
    {
      wt: both,
      facts: facts('FAILURE', { review: 'CHANGES_REQUESTED' }),
      lane: 'risk',
      state: 'uncommitted changes',
      work: ['7 files', '2'],
      agent: 'Claude · 1h ago',
      verb: 'Open in Claude',
      terminal: true,
    },
  ],
  [
    'Draft PR',
    {
      wt: draft,
      facts: facts('SUCCESS', { draft: true }),
      lane: null,
      state: clean,
      work: [],
      agent: 'Claude · 1d ago',
      verb: 'Open in Claude',
      terminal: true,
    },
  ],
  [
    'Uncommitted only',
    {
      wt: dirtyOnly,
      facts: facts('SUCCESS'),
      lane: 'risk',
      state: 'uncommitted changes',
      work: ['5 files'],
      agent: null,
      verb: 'Open in Claude',
      terminal: true,
    },
  ],
  [
    'Commits not on GitHub only',
    {
      wt: upOnly,
      lane: 'risk',
      state: 'commits that aren’t on GitHub',
      work: ['3'],
      agent: null,
      verb: null,
      terminal: true,
    },
  ],
  [
    'Count unknown',
    {
      wt: unknown,
      lane: 'risk',
      state: 'couldn’t tell whether every commit is on GitHub',
      work: ['?'],
      agent: null,
      verb: null,
      terminal: true,
    },
  ],
  [
    '1,204 files',
    {
      wt: many,
      lane: 'risk',
      state: 'uncommitted changes',
      work: ['1,204 files'],
      agent: null,
      verb: null,
      terminal: true,
    },
  ],
  [
    'Agent lock, live',
    {
      wt: liveLock,
      lane: 'running',
      state: 'something runs in it',
      work: ['1 file'],
      agent: 'Claude · by its lock · 29m',
      verb: null,
      terminal: true,
    },
  ],
  [
    'Stale lock',
    {
      wt: staleLock,
      lane: null,
      state: 'locked by a session that has ended',
      work: [],
      agent: 'Claude · 4h ago',
      verb: 'Unlock',
      terminal: true,
    },
  ],
  [
    'Hand lock',
    { wt: handLocked, lane: 'problem', state: 'locked by hand', work: [], agent: null, verb: null, terminal: true },
  ],
  [
    'Folder gone',
    {
      wt: pr98,
      repo: ag,
      lane: 'problem',
      state: 'its folder is gone',
      work: [],
      agent: null,
      verb: null,
      terminal: false,
    },
  ],
  [
    'git couldn’t read it',
    {
      wt: erred,
      lane: 'problem',
      state: 'git couldn’t read it',
      work: ['?'],
      agent: null,
      verb: null,
      terminal: false,
    },
  ],
  [
    'Detached',
    { wt: experiment, repo: df, lane: null, state: clean, work: [], agent: null, verb: null, terminal: true },
  ],
  [
    'Claude Code (legacy), no item',
    {
      wt: assignees,
      lane: 'risk',
      state: 'uncommitted changes',
      work: ['69 files'],
      agent: null,
      verb: null,
      terminal: true,
    },
  ],
  [
    'Made outside the app',
    { wt: outside, lane: null, state: clean, work: [], agent: null, verb: null, terminal: true },
  ],
  [
    'A long dependabot branch',
    {
      wt: dependabot,
      facts: facts('SUCCESS'),
      lane: null,
      state: clean,
      work: [],
      agent: null,
      verb: 'Open in Claude',
      terminal: true,
    },
  ],
]

// launch stands in for App's launchFor: the row variant's one button, placed where the row
// says.
function launch(_item: LinkedItem, place: LaunchPlace): ReactNode {
  return (
    <div className={place.boxClass}>
      <button type="button">Open in Claude</button>
    </div>
  )
}

// readsNothing is the details of a page that has read nothing, and asks for nothing.
function readsNothing(): WorktreeDetailsState {
  return { of: () => undefined, read: vi.fn() }
}

// actions are a row's handlers, each a mock.
function actions() {
  return {
    onToggle: vi.fn<() => void>(),
    onCleanUp: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onUnlock: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onRemove: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onForceRemove: vi.fn<(repo: WorktreeRepo, wt: Worktree) => void>(),
    onPrune: vi.fn<(repo: WorktreeRepo) => void>(),
  }
}

type LineOptions = Partial<Omit<WorktreeLineProps, 'worktree'>>

// renderLine draws wt's row, closed, in its list, with every handler a mock unless given.
function renderLine(wt: Worktree, options: LineOptions = {}) {
  const handlers = actions()
  const props: WorktreeLineProps = {
    repo: er,
    worktree: wt,
    open: false,
    details: readsNothing(),
    listing: 1,
    disabled: false,
    agentName,
    ...handlers,
    ...options,
  }
  const view = render(
    <ul>
      <WorktreeLine {...props} />
    </ul>,
    { wrapper: at() },
  )

  return { ...view, ...handlers, props, line: within(screen.getAllByRole('listitem')[0] as HTMLElement) }
}

// rowOf is the row of the list, its first item.
function rowOf(): HTMLElement {
  return screen.getAllByRole('listitem')[0] as HTMLElement
}

// verbs are the words of every verb a row can show.
const verbs = ['Clean up', 'Unlock', 'Open in Claude']

describe('WorktreeLine gallery', () => {
  it.each(gallery)('%s', (_label, c) => {
    renderLine(c.wt, { repo: c.repo ?? er, facts: c.facts, session: c.session, launchFor: launch })
    const li = rowOf()
    const line = within(li)

    expect(li.querySelector('[data-lane]')?.getAttribute('data-lane') ?? null).toBe(c.lane)
    expect(line.getByText(c.state)).toHaveClass('sr-only')

    for (const work of c.work) {
      expect(li).toHaveTextContent(work)
    }

    // The agent's cell, the row's fifth column, says what works in it, or isn't there.
    const agent = li.querySelector('.col-start-5')?.textContent.replace(/\s+/g, ' ').trim()
    expect(agent ?? null).toBe(c.agent)
    expect(verbs.filter((verb) => line.queryByRole('button', { name: verb }) !== null)).toEqual(
      c.verb === null ? [] : [c.verb],
    )
    expect(line.queryAllByRole('button', { name: /^Open .+ in Terminal$/ })).toHaveLength(c.terminal ? 1 : 0)
  })

  it('marks the lane on the edge, saying why in its tooltip', () => {
    renderLine(pr131)

    const edge = rowOf().querySelector('[data-lane]')
    expect(edge).toHaveAttribute('title', 'Its pull request is done, but it holds work that isn’t on GitHub')
    expect(edge).toHaveClass('bg-warning')
  })

  it('pulses a running row’s edge', () => {
    renderLine(inspect)

    expect(rowOf().querySelector('[data-lane]')).toHaveClass('bg-info', 'motion-safe:animate-pulse')
  })

  it('shows the state’s words in its tooltip, capitalized', () => {
    renderLine(handLocked)

    expect(screen.getByText('locked by hand').parentElement).toHaveAttribute('title', 'Locked by hand')
    expect(screen.getByText('locked by hand').parentElement).toHaveClass('text-danger')
  })

  it('shows the branch’s prefix muted, so the name stays, with the whole in its tooltip', () => {
    const { line } = renderLine(dependabot)

    const cell = line.getByTitle('dependabot/github_actions/actions/setup-go-6')
    expect(within(cell).getByText('dependabot/github_actions/actions/')).toHaveClass('text-muted')
    expect(within(cell).getByText('setup-go-6')).toHaveClass('font-medium')
  })

  it('shows a branch without a prefix whole', () => {
    const { line } = renderLine(row('main-copy', { branch: 'main' }))

    expect(line.getByTitle('main')).toHaveTextContent(/^main$/)
  })

  it('says a detached worktree’s commit in place of its branch', () => {
    const { line } = renderLine(experiment, { repo: df })

    const cell = line.getByTitle('Detached HEAD at a1b2c3d4e5f60718293a4b5c6d7e8f9012345678')
    expect(cell).toHaveTextContent('detached at a1b2c3d')
    expect(line.getByRole('button', { name: 'Unfold detached dotfiles-experiment' })).toBeInTheDocument()
    expect(line.getByRole('button', { name: 'Open dotfiles-experiment in Terminal' })).toBeInTheDocument()
  })

  it('shows the folder, and Claude Code’s tag on a worktree it made, with no item', () => {
    const { line } = renderLine(assignees)

    expect(line.getByTitle(legacy('pr-assignees'))).toHaveTextContent(/^pr-assignees$/)
    expect(line.getByText('Claude Code')).toHaveAttribute(
      'title',
      `Claude Code made it in the clone’s .claude/worktrees, before ${brand.name} made its own`,
    )
  })

  it('shows the folder alone for one made outside the app', () => {
    const { line } = renderLine(outside)

    expect(line.getByText('example-repo-hotfix')).toBeInTheDocument()
    expect(line.queryByText('Claude Code')).not.toBeInTheDocument()
  })

  it('marks the item by its kind and state, its number opening it on GitHub', async () => {
    const { line, onToggle } = renderLine(pr209)

    expect(line.getByTitle('Pull request, merged 1d ago')).toHaveClass('text-info')
    expect(line.getByText('Drop the v1 config loader')).toHaveAttribute(
      'title',
      'acme-org/example-repo#209: Drop the v1 config loader',
    )

    const number = line.getByRole('button', { name: '#209' })
    expect(number).toHaveAttribute('title', 'Open PR #209 on GitHub')
    await userEvent.click(number)

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/209')
    expect(onToggle).not.toHaveBeenCalled()
  })

  it.each([
    ['an open pull request', pr(7, 'Open'), 'Pull request, open', 'text-success'],
    [
      'a closed pull request',
      pr(7, 'Closed', { state: 'closed', closedAt: ago(3 * hour) }),
      'Pull request, closed without merging 3h ago',
      'text-danger',
    ],
    ['an open issue', issue(7, 'Open issue'), 'Issue, open', 'text-success'],
    [
      'a closed issue',
      issue(7, 'Closed issue', { state: 'closed', closedAt: ago(2 * day) }),
      'Issue, closed 2d ago',
      'text-info',
    ],
  ])('marks %s', (_label, linked, words, tone) => {
    const { line } = renderLine(row('pr-7', { item: linked }))

    expect(line.getByTitle(words)).toHaveClass(tone)
    expect(line.getAllByText(words)[0]).toHaveClass('sr-only')
  })

  it('shows a draft’s tag and its checks’ dot, as the inbox read them', () => {
    const { line } = renderLine(draft, { facts: facts('SUCCESS', { draft: true }) })

    expect(line.getByTitle('Draft pull request')).toHaveClass('text-muted')
    expect(line.getByText('draft')).toHaveAttribute('title', 'A draft, as the inbox read it')
    expect(line.getByTitle('Checks passed, as the inbox read them')).toHaveTextContent('checks passed')
  })

  it('pings the dot of checks still running', () => {
    const { line } = renderLine(pr214, { facts: facts('PENDING') })

    const dot = line.getByTitle('Checks running, as the inbox read them')
    expect(dot.querySelector('.motion-safe\\:animate-ping')).not.toBeNull()
  })

  it('shows no checks’ dot for checks the inbox doesn’t say', () => {
    const { line } = renderLine(pr214, { facts: facts('') })

    expect(line.queryByTitle(/as the inbox read them/)).not.toBeInTheDocument()
  })

  it('words the work in its tooltips, for a screen reader too', () => {
    const { line } = renderLine(both)

    expect(line.getByTitle('7 files have uncommitted changes, counting untracked files')).toHaveTextContent(
      'uncommitted: 7 files',
    )
    expect(line.getByTitle('2 commits aren’t on GitHub')).toHaveTextContent('not on GitHub: 2')
  })

  it('tags a pull request linked by its commits’ copies, open and calm, with no launch on it', () => {
    const { line } = renderLine(copiedWorktree(), { facts: facts('SUCCESS'), launchFor: launch })

    expect(rowOf().querySelector('[data-lane]')).toBeNull()
    expect(line.getByRole('button', { name: '#42' })).toBeInTheDocument()
    expect(line.getByText('copied')).toHaveAttribute('title', 'PR #42 is the pull request its commits were copied onto')
    expect(line.getByText('copied')).toHaveClass('rounded-full')
    expect(line.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
  })

  it('says on the copied tag when the pull request no longer holds the copies', () => {
    const { line } = renderLine(copiedWorktree({ onGitHub: false, copies: worktreeCopies() }))

    expect(line.getByText('copied')).toHaveAttribute(
      'title',
      'Its commits were copied onto PR #42’s branch, but PR #42 doesn’t hold them all now',
    )
  })

  it('tags no pull request linked by the branch it is on, and offers no launch on it', () => {
    const launchFor = vi.fn(launch)
    const { line } = renderLine(row('feature-x', { item: pr(42, 'Describe the widget cache'), linkedBy: 'branch' }), {
      facts: facts('SUCCESS'),
      launchFor,
    })

    expect(line.queryByText('copied')).not.toBeInTheDocument()
    expect(launchFor).not.toHaveBeenCalled()
  })

  it('notes commits on GitHub only as copies, muted, with ⧉ and where they are in its tooltip', () => {
    const { line } = renderLine(copiedWorktree())

    const note = line.getByTitle(
      'Its 10 commits aren’t on GitHub by ID, but each one’s change is on origin/feature-x (PR #42)',
    )
    expect(note).toHaveTextContent('on GitHub as copies: 10 on #42')
    expect(note).toHaveClass('text-muted')
    expect(note).not.toHaveClass('font-medium')
    // ⧉, two squares, in place of ↑.
    expect(note.querySelector('svg rect')).not.toBeNull()
    expect(line.getByText('clean; each commit’s change is on GitHub, copied onto PR #42’s branch')).toHaveClass(
      'sr-only',
    )
  })

  it('notes commits with only an edited copy as not on GitHub, in amber with ↑', () => {
    const { line } = renderLine(partlyCopiedWorktree())

    const note = line.getByTitle('1 commit isn’t on GitHub: 1 has only an edited copy on origin/feature-x')
    expect(note).toHaveTextContent('not on GitHub: 1')
    expect(note).toHaveClass('text-caution', 'font-medium')
    expect(note.querySelector('svg rect')).toBeNull()
    expect(rowOf().querySelector('[data-lane]')).toHaveAttribute('data-lane', 'risk')
  })

  it('says what runs in a worktree in the agent’s tooltip', () => {
    renderLine(inspect, { session: session('working', 12 * minute) })

    expect(screen.getByTitle('Claude Code 2.1.328, pid 47890, working, since 11:48')).toHaveTextContent(
      'Claude 2.1.328 · working · 12m',
    )
  })

  it('says a program the app doesn’t follow by its pid', () => {
    renderLine(row('pr-7', { inUse: [{ pid: 4242, command: 'node', agent: '', by: '' }], stage: 'in-use' }))

    expect(screen.getByTitle('node, pid 4242')).toHaveTextContent('node · pid 4242')
  })

  it('names the routine whose agent works in the background', () => {
    renderLine(routine)

    expect(
      screen.getByTitle('⚡ Fix checks runs Claude Code 2.1.328 here in the background (pid 61022)'),
    ).toHaveTextContent('Fix checks · in the background')
  })

  it('says a live lock holds a worktree no process is listed in', () => {
    renderLine(liveLock)

    expect(
      screen.getByTitle(
        'No process is listed, but a live Claude session holds it by its lock (pid 70113, since 11:30)',
      ),
    ).toHaveTextContent('Claude · by its lock · 29m')
  })

  it('says a lock whose start it can’t read without a time', () => {
    renderLine(
      row('pr-7', { locked: true, agentLock: true, lockReason: 'claude session pr-7 (pid 7)', stage: 'in-use' }),
    )

    expect(
      screen.getByTitle('No process is listed, but a live Claude session holds it by its lock (pid 7)'),
    ).toHaveTextContent(/^Claude · by its lock$/)
  })

  it('names the last session the app launched there in full in its tooltip', () => {
    renderLine(pr214)

    expect(screen.getByTitle(/^The last session the app launched here: Claude, /)).toHaveTextContent('Claude · 2h ago')
  })

  it('shows the last commit’s age, in full in its tooltip', () => {
    const { line } = renderLine(pr214)

    expect(line.getByTitle(/^Last commit: /)).toHaveTextContent('3h ago')
  })

  it('names an agent by its ID by default', () => {
    renderLine(pr209, { agentName: undefined })

    expect(rowOf()).toHaveTextContent('codex · 2d ago')
  })
})

describe('WorktreeLine verb', () => {
  it('cleans up a finished worktree from its green Clean up', async () => {
    const { line, onCleanUp, onToggle } = renderLine(pr209)

    const verb = line.getByRole('button', { name: 'Clean up' })
    expect(verb).toHaveAttribute(
      'title',
      'Removes the worktree, whose every commit is on GitHub; asks which branches go with it',
    )
    expect(verb).toHaveClass('text-success')
    await userEvent.click(verb)

    expect(onCleanUp).toHaveBeenCalledExactlyOnceWith(er, pr209)
    expect(onToggle).not.toHaveBeenCalled()
  })

  it('unlocks a lock an ended session left from Unlock', async () => {
    const { line, onUnlock } = renderLine(staleLock)

    const verb = line.getByRole('button', { name: 'Unlock' })
    expect(verb).toHaveAttribute(
      'title',
      'Releases the lock of the Claude session that ended here, so it can be removed',
    )
    await userEvent.click(verb)

    expect(onUnlock).toHaveBeenCalledExactlyOnceWith(er, staleLock)
  })

  it('holds the verb while a change runs', () => {
    const { line } = renderLine(pr209, { disabled: true })

    expect(line.getByRole('button', { name: 'Clean up' })).toBeDisabled()
  })

  it('places the launch on an open item the inbox lists in the verb’s column', () => {
    const launchFor = vi.fn(launch)
    renderLine(pr214, { facts: facts('SUCCESS'), launchFor })

    expect(launchFor).toHaveBeenCalledWith(pr214.item, launchPlace)
    expect(screen.getByRole('button', { name: 'Open in Claude' }).parentElement).toHaveClass('col-start-7')
  })

  it('shows no launch without a way to launch, nor for an item the inbox doesn’t list', () => {
    const launchFor = vi.fn(launch)
    renderLine(pr214, { facts: facts('SUCCESS') })
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()

    renderLine(upOnly, { launchFor })
    expect(launchFor).not.toHaveBeenCalled()
  })
})

describe('WorktreeLine disclosure', () => {
  it('unfolds from a click on the row, and folds from another', async () => {
    const { onToggle } = renderLine(pr214)

    await userEvent.click(screen.getByText('Retry the upload when the token expires'))
    expect(onToggle).toHaveBeenCalledOnce()
  })

  it('unfolds from its chevron, which names and controls its drawer', async () => {
    const { line, onToggle, rerender, props } = renderLine(pr214)

    const chevron = line.getByRole('button', { name: 'Unfold worktree-pr-214' })
    expect(chevron).toHaveAttribute('aria-expanded', 'false')
    expect(chevron).not.toHaveAttribute('aria-controls')
    await userEvent.click(chevron)
    expect(onToggle).toHaveBeenCalledOnce()

    rerender(
      <ul>
        <WorktreeLine {...props} open />
      </ul>,
    )

    const fold = screen.getByRole('button', { name: 'Fold worktree-pr-214' })
    expect(fold).toHaveAttribute('aria-expanded', 'true')
    expect(fold.getAttribute('aria-controls')).toBe(screen.getByRole('listitem', { name: 'About worktree-pr-214' }).id)
    expect(rowOf()).toHaveAttribute('data-open')
  })

  it('leaves a click that selects the row’s words alone', async () => {
    const selection = vi.spyOn(window, 'getSelection').mockReturnValue({ toString: () => 'pr-214' } as Selection)
    const { onToggle } = renderLine(pr214)

    await userEvent.click(screen.getByText('Retry the upload when the token expires'))

    expect(onToggle).not.toHaveBeenCalled()
    selection.mockRestore()
  })

  it('reads a click with no selection as a click', async () => {
    const selection = vi.spyOn(window, 'getSelection').mockReturnValue(null)
    const { onToggle } = renderLine(pr214)

    await userEvent.click(screen.getByText('Retry the upload when the token expires'))

    expect(onToggle).toHaveBeenCalledOnce()
    selection.mockRestore()
  })

  it('leaves clicks on its controls, its menu, and a dialog to them', async () => {
    const { line, onToggle } = renderLine(pr214, {
      facts: facts('SUCCESS'),
      launchFor: () => (
        <dialog open aria-label="Launch options">
          <p>The launch options</p>
        </dialog>
      ),
    })

    await userEvent.click(line.getByRole('button', { name: 'Open worktree-pr-214 in Terminal' }))
    await userEvent.click(line.getByRole('button', { name: 'More for worktree-pr-214' }))
    await userEvent.click(screen.getByRole('menu'))
    await userEvent.click(screen.getByText('The launch options'))

    expect(onToggle).not.toHaveBeenCalled()
  })

  it('leaves the row as it was on the click that closes an open menu', async () => {
    const { line, onToggle } = renderLine(pr214)

    await userEvent.click(line.getByRole('button', { name: 'More for worktree-pr-214' }))
    await userEvent.click(screen.getByText('Retry the upload when the token expires'))

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(onToggle).not.toHaveBeenCalled()

    await userEvent.click(screen.getByText('Retry the upload when the token expires'))
    expect(onToggle).toHaveBeenCalledOnce()
  })

  it('leaves clicks on what a control renders elsewhere to it', async () => {
    const { onToggle } = renderLine(pr214, {
      facts: facts('SUCCESS'),
      launchFor: () => createPortal(<p>Rendered elsewhere</p>, document.body),
    })

    await userEvent.click(screen.getByText('Rendered elsewhere'))

    expect(onToggle).not.toHaveBeenCalled()
  })

  it('unfolds with → and folds with ←, from its chevron', async () => {
    const { line, onToggle, rerender, props } = renderLine(pr214)

    line.getByRole('button', { name: 'Unfold worktree-pr-214' }).focus()
    await userEvent.keyboard('{ArrowLeft}')
    expect(onToggle).not.toHaveBeenCalled()
    await userEvent.keyboard('{ArrowRight}')
    expect(onToggle).toHaveBeenCalledOnce()

    rerender(
      <ul>
        <WorktreeLine {...props} open />
      </ul>,
    )
    await userEvent.keyboard('{ArrowRight}')
    expect(onToggle).toHaveBeenCalledOnce()
    await userEvent.keyboard('{ArrowLeft}')
    expect(onToggle).toHaveBeenCalledTimes(2)
    await userEvent.keyboard('{Enter}')
    expect(onToggle).toHaveBeenCalledTimes(3)
  })

  it('moves to the rows around it with ↓ and ↑, across clones', async () => {
    const shared = { details: readsNothing(), listing: 1, disabled: false, agentName, ...actions(), open: false }
    render(
      <>
        <ul>
          <WorktreeLine repo={er} worktree={pr214} {...shared} />
          <WorktreeLine repo={er} worktree={pr209} {...shared} />
        </ul>
        <ul>
          <WorktreeLine repo={ag} worktree={issue88} {...shared} />
        </ul>
      </>,
      { wrapper: at() },
    )

    const first = screen.getByRole('button', { name: 'Unfold worktree-pr-214' })
    first.focus()
    await userEvent.keyboard('{ArrowUp}')
    expect(first).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('button', { name: 'Unfold worktree-pr-209' })).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    const last = screen.getByRole('button', { name: 'Unfold worktree-issue-88' })
    expect(last).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(last).toHaveFocus()
    await userEvent.keyboard('{ArrowUp}')
    expect(screen.getByRole('button', { name: 'Unfold worktree-pr-209' })).toHaveFocus()
    await userEvent.keyboard('{Home}')
    expect(screen.getByRole('button', { name: 'Unfold worktree-pr-209' })).toHaveFocus()
  })
})

describe('WorktreeLine details', () => {
  it('reads the details as its drawer opens, and again after each listing', () => {
    const details = readsNothing()
    const { rerender, props } = renderLine(pr214, { details })

    expect(details.read).not.toHaveBeenCalled()

    rerender(
      <ul>
        <WorktreeLine {...props} open />
      </ul>,
    )
    expect(details.read).toHaveBeenCalledExactlyOnceWith(er.path, pr214)

    rerender(
      <ul>
        <WorktreeLine {...props} open listing={2} />
      </ul>,
    )
    expect(details.read).toHaveBeenCalledTimes(2)

    rerender(
      <ul>
        <WorktreeLine {...props} open={false} listing={3} />
      </ul>,
    )
    expect(details.read).toHaveBeenCalledTimes(2)
  })

  it.each([
    ['a folder that’s gone', pr98],
    ['one git couldn’t read', erred],
  ])('never asks for the details of %s', (_label, wt) => {
    const details = readsNothing()
    renderLine(wt, { details, open: true })

    expect(details.read).not.toHaveBeenCalled()
  })

  // Page draws a row with the window's own details, unfolding it as the page does.
  function Page({ wt, listing }: { wt: Worktree; listing: number }) {
    const details = useWorktreeDetails(listing)
    const [open, setOpen] = useState(false)

    return (
      <ul>
        <WorktreeLine
          {...actions()}
          repo={er}
          worktree={wt}
          open={open}
          onToggle={() => {
            setOpen(!open)
          }}
          details={details}
          listing={listing}
          disabled={false}
          agentName={agentName}
        />
      </ul>
    )
  }

  it('lists the first twelve files at once, then the rest once read', async () => {
    const answer = deferred<WorktreeDetails>()
    platform.worktreeDetails.mockReturnValueOnce(answer.promise)
    const wt = row('pr-7', { dirty: true, dirtyCount: 30, files: worktreeFiles(12), canRemove: false })
    render(<Page wt={wt} listing={1} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'Unfold worktree-pr-7' }))

    const drawer = within(screen.getByRole('listitem', { name: 'About worktree-pr-7' }))
    expect(platform.worktreeDetails).toHaveBeenCalledExactlyOnceWith(er.path, wt.path)
    expect(drawer.getByTitle('src/file-012.ts')).toBeInTheDocument()
    expect(drawer.getByText('reading the other 18…')).toBeInTheDocument()

    act(() => {
      answer.resolve(worktreeDetails({ files: worktreeFiles(30), fileCount: 30 }))
    })

    await userEvent.click(await drawer.findByRole('button', { name: 'Show all 30' }))
    expect(drawer.getByTitle('src/file-030.ts')).toBeInTheDocument()
    expect(drawer.queryByText(/reading the other/)).not.toBeInTheDocument()

    // Folded and unfolded again in the same listing, it reads nothing more.
    await userEvent.click(screen.getByRole('button', { name: 'Fold worktree-pr-7' }))
    await userEvent.click(screen.getByRole('button', { name: 'Unfold worktree-pr-7' }))
    expect(platform.worktreeDetails).toHaveBeenCalledOnce()
  })
})

describe('WorktreeLine tools', () => {
  it('opens the worktree in Terminal from ›_', async () => {
    const { line } = renderLine(pr214)

    await userEvent.click(line.getByRole('button', { name: 'Open worktree-pr-214 in Terminal' }))

    expect(platform.openWorktreeTerminal).toHaveBeenCalledExactlyOnceWith(er.path, pr214.path)
  })

  it('says under the row why Terminal or Finder couldn’t open', async () => {
    platform.openWorktreeTerminal.mockRejectedValueOnce(new Error('no terminal is set up'))
    const { line } = renderLine(pr214)

    await userEvent.click(line.getByRole('button', { name: 'Open worktree-pr-214 in Terminal' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('no terminal is set up')
  })

  it('puts away why Terminal couldn’t open at the next listing', async () => {
    platform.openWorktreeTerminal.mockRejectedValueOnce(new Error('no terminal is set up'))
    const { line, rerender, props } = renderLine(pr214)

    await userEvent.click(line.getByRole('button', { name: 'Open worktree-pr-214 in Terminal' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('no terminal is set up')

    rerender(
      <ul>
        <WorktreeLine {...props} />
      </ul>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('no terminal is set up')

    rerender(
      <ul>
        <WorktreeLine {...props} listing={2} />
      </ul>,
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('keeps ›_’s place on a row it can’t open, so ⋯ stays in line with the others', () => {
    const { line } = renderLine(pr98, { repo: ag })

    const tools = line.getByRole('button', { name: 'More for worktree-pr-98' }).closest('[data-row-controls]')
    const gap = tools?.firstElementChild
    expect(gap).toHaveAttribute('data-tool-gap')
    expect(gap).toHaveAttribute('aria-hidden', 'true')
    expect(gap).toHaveClass('size-6')
    expect(line.queryByRole('button', { name: /in Terminal$/ })).not.toBeInTheDocument()
  })

  it('offers everything else in ⋯, and reveals the worktree in Finder from it', async () => {
    const { line } = renderLine(pr214)

    await userEvent.click(line.getByRole('button', { name: 'More for worktree-pr-214' }))

    expect(screen.getAllByRole('menuitem').map((m) => m.textContent)).toEqual([
      'Open in Terminal',
      'Reveal in Finder',
      'Copy path',
      'Copy branch',
      'Open PR #214 on GitHub',
      'Remove…',
    ])
    await userEvent.click(screen.getByRole('menuitem', { name: 'Reveal in Finder' }))

    expect(platform.revealWorktree).toHaveBeenCalledExactlyOnceWith(er.path, pr214.path)
  })

  it('unlocks from ⋯, saying under the item what it does', async () => {
    const { line, onUnlock } = renderLine(staleLock)

    await userEvent.click(line.getByRole('button', { name: 'More for worktree-pr-253' }))

    const unlock = screen.getByRole('menuitem', { name: 'Unlock' })
    expect(unlock).toHaveAccessibleDescription('Releases the lock of the session that ended here')
    expect(screen.getByRole('menuitem', { name: 'Remove…' })).toBeDisabled()
    await userEvent.click(unlock)

    expect(onUnlock).toHaveBeenCalledExactlyOnceWith(er, staleLock)
  })

  it('opens Terminal from ⋯ and from the drawer too', async () => {
    renderLine(pr214, { open: true })

    await userEvent.click(screen.getByRole('button', { name: 'More for worktree-pr-214' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Open in Terminal' }))
    await userEvent.click(
      within(screen.getByRole('listitem', { name: 'About worktree-pr-214' })).getByRole('button', {
        name: 'Reveal in Finder',
      }),
    )

    expect(platform.openWorktreeTerminal).toHaveBeenCalledOnce()
    expect(platform.revealWorktree).toHaveBeenCalledOnce()
  })
})

describe('WorktreeLine work', () => {
  it.each<[RowWork, string, boolean]>([
    [{ state: 'doing', text: 'Removing…', reason: '' }, 'text-text', true],
    [{ state: 'done', text: 'Removed', reason: '' }, 'text-success', true],
  ])('dims a row a change takes, saying %o in place of its verb and tools', (work, tone, dim) => {
    const { line } = renderLine(pr209, { work })

    expect(line.getByRole('status')).toHaveTextContent(work.text)
    expect(line.getByRole('status')).toHaveClass(tone)
    expect(rowOf().classList.contains('opacity-55')).toBe(dim)
    expect(line.queryByRole('button', { name: 'Clean up' })).not.toBeInTheDocument()
    expect(line.queryByRole('button', { name: /More for/ })).not.toBeInTheDocument()
    expect(line.getByRole('button', { name: 'Unfold worktree-pr-209' })).toBeInTheDocument()
  })

  it('marks a row being changed busy', () => {
    renderLine(pr209, { work: { state: 'doing', text: 'Removing…', reason: '' } })

    expect(rowOf()).toHaveAttribute('aria-busy', 'true')
  })

  it('keeps a row a change left in place in its colors, saying why under it', () => {
    const reason = 'Left in place: it changed since the listing, and 1 file has uncommitted changes now.'
    renderLine(pr131, { work: { state: 'left', text: 'Left in place', reason } })

    expect(rowOf()).not.toHaveClass('opacity-55')
    expect(within(rowOf()).getByRole('status')).toHaveClass('text-warning')
    expect(screen.getByRole('alert')).toHaveTextContent(reason)
    expect(screen.getByRole('alert')).toHaveClass('text-warning')
  })

  it('says nothing under a row left in place without a reason', () => {
    renderLine(pr131, { work: { state: 'left', text: 'Left in place', reason: '' } })

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
  })

  it('says under the row why a change to it failed', () => {
    renderLine(pr209, { error: 'the worktree changed since it was checked' })

    expect(screen.getByRole('alert')).toHaveTextContent('the worktree changed since it was checked')
    expect(screen.getByRole('alert')).toHaveClass('text-danger')
  })

  it('holds the drawer’s verb while a change takes the row', () => {
    renderLine(pr209, { open: true, work: { state: 'doing', text: 'Cleaning up…', reason: '' } })

    expect(
      within(screen.getByRole('listitem', { name: 'About worktree-pr-209' })).queryByRole('button', {
        name: 'Clean up',
      }),
    ).not.toBeInTheDocument()
  })
})
