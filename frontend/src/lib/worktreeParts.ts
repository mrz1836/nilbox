// The Worktrees page's smaller parts, pure: the words and marks of a row's cells (its
// branch, its pull request or issue, a changed file, what works in it and who made it),
// what the window joins to a row from what it already holds (the inbox's items, the
// sessions it follows), the page's chips, and the command that brings a cleaned-up
// worktree back. The row's lane and state, its verb, and what removing it would cost are in
// lib/worktrees.

import { brand } from '../generated/brand'
import type {
  AgentID,
  AgentSession,
  InboxItem,
  LinkedItem,
  SessionState,
  SessionsSnapshot,
  Worktree,
  WorktreeCleanup,
  WorktreeFile,
  WorktreeLinkKind,
  WorktreeRepo,
} from '../platform'
import { counted } from './format'
import { homeWord, shellQuoted } from './git'
import { itemKey } from './itemWorktrees'
import { isLive } from './sessions'
import { capitalize, formatAgo } from './time'

// Tone is the color a part of the page takes, one of the theme's tokens: text-info,
// text-warning, and so on.
export type Tone = 'info' | 'warning' | 'caution' | 'success' | 'danger' | 'muted'

// clockOf is a time of day as the page writes it, in 24 hours: "01:27".
export function clockOf(iso: string): string {
  const d = new Date(iso)

  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// running reports a worktree something runs in: a process, or a live agent session that
// holds it by its lock.
export function running(wt: Worktree): boolean {
  return wt.inUse.length > 0 || wt.stage === 'in-use'
}

// byHand reports a worktree someone locked by hand, which the app never unlocks.
export function byHand(wt: Worktree): boolean {
  return wt.locked && !wt.agentLock
}

// unpushedKnown is how many commits on a worktree aren't on GitHub, or 0 when every one is
// (by its commit ID, at its pull request's head, or as an identical copy: onGitHub), or
// when they couldn't be counted.
export function unpushedKnown(wt: Worktree): number {
  return wt.unpushed > 0 && !wt.onGitHub ? wt.unpushed : 0
}

// unpushedUnknown reports a linked worktree whose commits couldn't be checked against
// GitHub, which is never counted as on GitHub.
export function unpushedUnknown(wt: Worktree): boolean {
  return !wt.main && !wt.prunable && wt.unpushed < 0 && !wt.onGitHub
}

// atItemHead reports a worktree at its pull request's head commit, which GitHub keeps, so
// every commit it has is on GitHub by its commit ID, however many no remote-tracking branch
// has.
export function atItemHead(wt: Worktree): boolean {
  return wt.item !== null && wt.item.headOid !== '' && wt.item.headOid === wt.head
}

// Copied is why a worktree's commits that aren't on GitHub by their commit IDs are on
// GitHub anyway: each of them, n, has an identical copy on ref, a pushed branch, as in
// origin/feature-x, whose own name is branch (feature-x), as cherry-picking or rebasing
// them onto it leaves. pr is the number of the pull request they went into, the one that
// branch heads, or 0 when none is known, and item that pull request as the worktree is
// linked to it by its copies, or null when it is linked otherwise, or to nothing. recorded
// reports that ref no longer holds them all, and what the app recorded while it did vouches
// for them instead, as it does once pr merged, or once ref is gone (gone): pr's head, which
// GitHub keeps, has every copy.
export interface Copied {
  n: number
  ref: string
  branch: string
  pr: number
  item: LinkedItem | null
  recorded: boolean
  gone: boolean
}

// copiedOf says why a worktree's commits that aren't on GitHub by their commit IDs are on
// GitHub as copies, or is null when they aren't. The backend rules them on GitHub
// (onGitHub), and this only words its comparison when that backs it: every one of them, as
// many as it counted (unpushed), with an identical copy on one pushed branch, and nothing
// that keeps those from counting (note). A worktree at its pull request's head has them on
// GitHub by commit ID, which says it more plainly.
export function copiedOf(wt: Worktree): Copied | null {
  const c = wt.copies

  if (
    !wt.onGitHub ||
    wt.unpushed <= 0 ||
    c.count !== wt.unpushed ||
    c.identical !== c.count ||
    c.note !== '' ||
    c.ref === '' ||
    atItemHead(wt)
  ) {
    return null
  }

  const { item } = wt

  return {
    n: c.count,
    ref: c.ref,
    branch: c.ref.slice(c.ref.indexOf('/') + 1),
    pr: c.pr,
    item: wt.linkedBy === 'copy' && item?.number === c.pr ? item : null,
    recorded: c.recorded,
    gone: c.gone,
  }
}

// prOfCopies names the pull request a worktree's commits went into as copies: "PR #42", or
// "its pull request" when its number isn't known.
export function prOfCopies(c: Copied): string {
  return c.pr > 0 ? `PR #${String(c.pr)}` : 'its pull request'
}

// folderOf is the last part of a worktree's path: its folder's name.
export function folderOf(wt: Worktree): string {
  return wt.displayPath.slice(wt.displayPath.lastIndexOf('/') + 1)
}

// splitBranch splits a branch into the prefix that may give way when the row runs out of
// room and the name that stays: ["worktree-", "pr-214"], ["feat/", "retry"], or
// ["", "main"].
export function splitBranch(branch: string): [prefix: string, name: string] {
  if (branch.startsWith('worktree-')) {
    return ['worktree-', branch.slice('worktree-'.length)]
  }

  const slash = branch.lastIndexOf('/')

  return slash < 0 ? ['', branch] : [branch.slice(0, slash + 1), branch.slice(slash + 1)]
}

// itemRef names a pull request or issue in a few words: "PR #214" or "Issue #88".
export function itemRef(item: LinkedItem): string {
  return `${item.kind === 'pr' ? 'PR' : 'Issue'} #${String(item.number)}`
}

// copiedTag is the tooltip of the copied tag beside the pull request a worktree is linked
// to by its commits' copies (linkedBy 'copy'), or '' for one linked otherwise, which shows
// none: that it is the pull request they were copied onto, or, once the listing found it
// doesn't hold them all (onGitHub false), that too.
export function copiedTag(wt: Worktree): string {
  if (wt.linkedBy !== 'copy' || wt.item === null) {
    return ''
  }

  const pr = itemRef(wt.item)

  return wt.onGitHub
    ? `${pr} is the pull request its commits were copied onto`
    : `Its commits were copied onto ${pr}’s branch, but ${pr} doesn’t hold them all now`
}

// linkWords say how a worktree is linked to its pull request or issue, before naming it in
// a sentence (as in "Made for pull request #7"): made for it, by its record, name, or own
// branch; on its branch, by the pushed branch it is on; or by the copies of its commits on
// its branch.
export const linkWords: Readonly<Record<WorktreeLinkKind, string>> = {
  '': 'Made for',
  name: 'Made for',
  branch: 'On the branch of',
  copy: 'Its commits were copied onto',
}

// KindIcon is the mark of a pull request or issue as it stands.
export type KindIcon = 'pr' | 'prMerged' | 'prClosed' | 'issue' | 'issueClosed'

// Kind is a row's pull request or issue as the item column marks it: its icon, the icon's
// color, and the words for its tooltip and a screen reader.
export interface Kind {
  icon: KindIcon
  tone: Tone
  words: string
}

// agoWords is how long before now iso was, after a space, as in " 2d ago", or '' when
// it isn't known, so a sentence reads whole either way.
export function agoWords(now: Date, iso: string): string {
  const ago = formatAgo(now, iso)

  return ago === '' ? '' : ` ${ago}`
}

// kindOf marks a pull request or issue: open, a draft (as the inbox read it), merged, or
// closed, with how long ago. A closed issue is done, so it takes the merged color, not red.
export function kindOf(item: LinkedItem, draft: boolean, now: Date): Kind {
  const ago = agoWords(now, item.closedAt)

  if (item.kind === 'pr') {
    switch (item.state) {
      case 'merged':
        return { icon: 'prMerged', tone: 'info', words: `Pull request, merged${ago}` }
      case 'closed':
        return { icon: 'prClosed', tone: 'danger', words: `Pull request, closed without merging${ago}` }
      default:
        return draft
          ? { icon: 'pr', tone: 'muted', words: 'Draft pull request' }
          : { icon: 'pr', tone: 'success', words: 'Pull request, open' }
    }
  }

  return item.state === 'open'
    ? { icon: 'issue', tone: 'success', words: 'Issue, open' }
    : { icon: 'issueClosed', tone: 'info', words: `Issue, closed${ago}` }
}

// Checks is a pull request's checks as the inbox read them, for the dot beside its title.
export interface Checks {
  tone: Tone
  words: string
  // pending reports checks still running, whose dot pings.
  pending: boolean
}

// checksOf reads GitHub's overall state of a pull request's checks, or null for none, or a
// state the dot doesn't show.
export function checksOf(state: string): Checks | null {
  switch (state) {
    case 'SUCCESS':
      return { tone: 'success', words: 'Checks passed', pending: false }
    case 'FAILURE':
    case 'ERROR':
      return { tone: 'danger', words: 'Checks failing', pending: false }
    case 'PENDING':
      return { tone: 'warning', words: 'Checks running', pending: true }
    default:
      return null
  }
}

// FileStatus is a changed file as the drawer lists it: its one letter (M, A, D, or R as git
// gives it, U for untracked, C for a conflict, or git's own for anything else), the words
// for it, whether any of it is staged, its tooltip, and its path split into the folder,
// muted, and the name.
export interface FileStatus {
  letter: string
  words: string
  staged: boolean
  title: string
  dir: string
  name: string
}

// fileWords name the letters git status gives a file.
const fileWords: Readonly<Record<string, string>> = { M: 'modified', A: 'added', D: 'deleted', R: 'renamed' }

// fileStatus reads a file's two status letters, the index's then the worktree's.
export function fileStatus(file: WorktreeFile): FileStatus {
  const x = file.code.charAt(0)
  const y = file.code.charAt(1)
  let letter: string
  let words: string

  if (file.code === '??') {
    letter = 'U'
    words = 'untracked: new, not added'
  } else if (x === 'U' || y === 'U' || file.code === 'AA' || file.code === 'DD') {
    letter = 'C'
    words = 'unmerged: a conflict'
  } else {
    letter = y === ' ' ? x : y
    words = fileWords[letter] ?? 'changed'
  }

  const staged = x !== ' ' && x !== '?'
  const part = staged ? (y === ' ' ? ', staged' : ', partly staged') : ''
  const slash = file.path.lastIndexOf('/')

  return {
    letter,
    words,
    staged,
    title: `${capitalize(words)}${part} (git status “${file.code}”)`,
    dir: file.path.slice(0, slash + 1),
    name: file.path.slice(slash + 1),
  }
}

// ItemFacts is what the inbox read of a worktree's pull request or issue, while it lists
// it: the item, its checks' overall state, its review decision, and whether it's a draft.
export interface ItemFacts {
  item: InboxItem
  checks: string
  review: string
  draft: boolean
}

// itemFacts maps each item the inbox lists, by itemKey, to what it read of it: the other
// way from lib/itemWorktrees, which maps the inbox's items to their worktrees.
export function itemFacts(items: readonly InboxItem[]): ReadonlyMap<string, ItemFacts> {
  return new Map(
    items.map((item) => [
      itemKey(item.repo, item.number),
      {
        item,
        checks: item.details?.checks.state ?? '',
        review: item.details?.reviews.decision ?? '',
        draft: item.isDraft,
      },
    ]),
  )
}

// factsOf is what the inbox read of a worktree's item, or undefined when it has none or the
// inbox doesn't list it.
export function factsOf(facts: ReadonlyMap<string, ItemFacts>, linked: LinkedItem | null): ItemFacts | undefined {
  return linked === null ? undefined : facts.get(itemKey(`${linked.owner}/${linked.name}`, linked.number))
}

// liveSessions maps each folder a session the app follows works in, by its path with ~ (a
// worktree's displayPath), to the newest such session that hasn't ended. It is empty while
// the app doesn't follow sessions.
export function liveSessions(snapshot: SessionsSnapshot | null): ReadonlyMap<string, AgentSession> {
  const out = new Map<string, AgentSession>()

  if (snapshot === null || !snapshot.following) {
    return out
  }

  for (const s of snapshot.sessions) {
    if (s.dir !== '' && isLive(s.state) && !out.has(s.dir)) {
      out.set(s.dir, s)
    }
  }

  return out
}

// cloneOf reads the clone a worktree the app looks after belongs to from its path: its
// managed root (<parent>/.<slug>/worktrees/<clone>/<name>) or the clone's .claude/worktrees.
// It is '' for a path that is neither.
export function cloneOf(displayPath: string): string {
  const parts = displayPath.split('/')
  const n = parts.length

  if (parts[n - 3] === '.claude' && parts[n - 2] === 'worktrees') {
    return parts.slice(0, n - 3).join('/')
  }

  if (parts[n - 3] === 'worktrees' && parts[n - 4] === `.${brand.slug}`) {
    return [...parts.slice(0, n - 4), parts[n - 2]].join('/')
  }

  return ''
}

// restoreCommand is the git command that brings a cleaned-up worktree back, at the commit
// it had checked out: git -C <clone> worktree add [-b <branch>] <path> <commit>. With the
// one branch deleted with it, it comes back on that branch; with none, or several (the
// record joins their names with ", ", which no branch name holds), detached. The paths keep
// their ~/ outside the quotes, so the shell expands it. It is '' when the clone can't be
// read from the worktree's path, which offers no command then.
export function restoreCommand(cleanup: WorktreeCleanup): string {
  const clone = cloneOf(cleanup.displayPath)

  if (clone === '') {
    return ''
  }

  const one = cleanup.deletedBranch !== '' && !cleanup.deletedBranch.includes(', ')
  const branch = one ? `-b ${shellQuoted(cleanup.deletedBranch)} ` : ''

  return `git -C ${homeWord(clone)} worktree add ${branch}${homeWord(cleanup.displayPath)} ${cleanup.head}`
}

// LockInfo is an agent lock's reason, read: the session's name, its process, and when it
// started (RFC 3339), or '' when the reason doesn't say.
export interface LockInfo {
  session: string
  pid: number
  start: string
}

// lockPattern mirrors the backend's reading of an agent lock's reason (lock.go), as in
// "claude session pr-7 (pid 4242 start Fri Nov  6 17:14:26 2026)".
const lockPattern = /^claude (?:agent|session) (.{1,255}) \(pid (\d{1,10})(?: start (.{1,255}))?\)$/

// asctime is the start's form, C's asctime with no zone, which is the machine's own.
const asctime = /^\w{3} (\w{3}) +(\d{1,2}) (\d\d):(\d\d):(\d\d) (\d{4})$/

const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// lockInfo reads a worktree's agent lock, or null for one the agent didn't write.
export function lockInfo(wt: Worktree): LockInfo | null {
  const m = lockPattern.exec(wt.lockReason)

  if (m === null) {
    return null
  }

  // The session and the pid always match; the start is optional.
  const [session, pid, start = ''] = m.slice(1) as [string, string, string | undefined]
  const t = asctime.exec(start)
  const month = t === null ? -1 : months.indexOf(String(t[1]))
  const at =
    t === null || month < 0
      ? null
      : new Date(Number(t[6]), month, Number(t[2]), Number(t[3]), Number(t[4]), Number(t[5]))

  return { session, pid: Number(pid), start: at === null ? '' : at.toISOString() }
}

// stateWords say how a session the app follows stands, on a row: one it can't read the
// state of still runs.
export const stateWords: Readonly<Record<SessionState, string>> = {
  starting: 'starting',
  working: 'working',
  needs_approval: 'needs approval',
  needs_input: 'needs input',
  idle: 'idle',
  ended: 'ended',
  failed: 'failed',
  unknown: 'running',
}

// stateTones color those words: blue at work, amber while it waits for the user, green
// once its turn is done, red when it failed, muted otherwise.
export const stateTones: Readonly<Record<SessionState, Tone>> = {
  starting: 'muted',
  working: 'info',
  needs_approval: 'warning',
  needs_input: 'warning',
  idle: 'success',
  ended: 'muted',
  failed: 'danger',
  unknown: 'muted',
}

// AgentLine is what the agent column says works in a worktree now: an Autopilot routine's
// agent (routine), another process, a session the app follows or not (live), a live agent
// lock with no process listed (lock); or else the last session the app launched there
// (past). name names the program; version is Claude Code's, read from its process; state
// and since are the followed session's, '' when none is.
export type AgentLine =
  | { kind: 'routine'; name: string; version: string; pid: number; by: string; state: SessionState | ''; since: string }
  | {
      kind: 'live'
      name: string
      version: string
      pid: number
      followed: boolean
      state: SessionState | ''
      since: string
    }
  | { kind: 'lock'; name: string; pid: number; since: string }
  | { kind: 'past'; name: string; at: string }

// version matches a process named after Claude Code's version, as in "2.1.328".
const version = /^\d+\.\d+\.\d+$/

// agentLine reads what works in a worktree, with session, the session the app follows
// there (liveSessions), if any, and agentName, how the window names an agent; or null when
// nothing does and the app never launched there.
export function agentLine(
  wt: Worktree,
  session: AgentSession | undefined,
  agentName: (id: AgentID) => string,
): AgentLine | null {
  const s = wt.inUse.find((p) => p.agent !== '' || p.by !== '') ?? wt.inUse[0]

  if (s !== undefined) {
    const v = version.test(s.command) ? s.command : ''
    const name = s.agent !== '' ? agentName(s.agent) : v !== '' ? agentName('claude') : s.command
    const state = session?.state ?? ''
    const since = session?.launchedAt ?? ''

    if (s.by !== '') {
      return { kind: 'routine', name, version: v, pid: s.pid, by: s.by.replace(/^⚡\s*/u, ''), state, since }
    }

    const followed = s.agent !== ''

    return {
      kind: 'live',
      name,
      version: v,
      pid: s.pid,
      followed,
      state: followed ? state : '',
      since: followed ? since : '',
    }
  }

  if (wt.agentLock && !wt.staleLock) {
    const lock = lockInfo(wt)

    return { kind: 'lock', name: agentName('claude'), pid: lock?.pid ?? 0, since: lock?.start ?? '' }
  }

  if (wt.agent !== '' && wt.lastLaunchedAt !== '') {
    return { kind: 'past', name: agentName(wt.agent), at: wt.lastLaunchedAt }
  }

  return null
}

// runsIn says what works in a worktree, as a clause: "Claude runs in it".
export function runsIn(wt: Worktree, agentName: (id: AgentID) => string): string {
  const a = agentLine(wt, undefined, agentName)

  if (a === null) {
    return 'a session runs in it'
  }

  switch (a.kind) {
    case 'routine':
      return `⚡ ${a.by} runs its agent in it`
    case 'lock':
      return `the ${a.name} session that locked it still runs`
    default:
      return `${a.name} runs in it`
  }
}

// madeBy says who made a worktree: Claude Code in the clone's .claude/worktrees, an
// Autopilot routine whose agent works in it, the app, or someone outside it.
export function madeBy(wt: Worktree): string {
  if (wt.legacy) {
    return 'Claude Code (legacy), in the clone’s .claude/worktrees'
  }

  const run = wt.inUse.find((s) => s.by !== '')

  if (run !== undefined) {
    return run.by
  }

  return wt.managed ? brand.name : 'outside the app'
}

// FacetKey is a state the page's chips filter by, as its is: value.
export type FacetKey = 'in-use' | 'dirty' | 'unpushed' | 'review' | 'finished'

// facets are the chips, in order: each its is: value, its label and color, which worktrees
// it counts, and what they have in common, for its tooltip, said of one and of several.
const facets: readonly {
  key: FacetKey
  label: string
  tone: Tone
  test: (wt: Worktree) => boolean
  what: readonly [one: string, many: string]
}[] = [
  {
    key: 'in-use',
    label: 'running',
    tone: 'info',
    test: running,
    what: ['something runs in', 'something runs in'],
  },
  {
    key: 'dirty',
    label: 'uncommitted',
    tone: 'warning',
    test: (wt) => wt.dirtyCount > 0,
    what: ['has uncommitted files', 'have uncommitted files'],
  },
  {
    key: 'unpushed',
    label: 'not on GitHub',
    tone: 'caution',
    test: (wt) => unpushedKnown(wt) > 0,
    what: ['has commits that aren’t on GitHub', 'have commits that aren’t on GitHub'],
  },
  {
    key: 'review',
    label: 'needs a look',
    tone: 'warning',
    test: (wt) => wt.stage === 'needs-review',
    what: ['is done on GitHub but holds work that isn’t', 'are done on GitHub but hold work that isn’t'],
  },
  {
    key: 'finished',
    label: 'ready to clean up',
    tone: 'success',
    test: (wt) => wt.stage === 'finished',
    what: ['Clean up removes without losing anything', 'Clean up removes without losing anything'],
  },
]

// Chip is one of the page's chips: its state, label, and color, how many worktrees are in
// it, whether the toolbar's filter is it alone (on), and its tooltip.
export interface Chip {
  key: FacetKey
  label: string
  tone: Tone
  n: number
  on: boolean
  title: string
}

// facetOn reports a query that is the chip's filter alone.
export function facetOn(query: string, key: FacetKey): boolean {
  return query.trim() === `is:${key}`
}

// facetQuery is the query a click on a chip writes: its filter, or none when it's on.
export function facetQuery(query: string, key: FacetKey): string {
  return facetOn(query, key) ? '' : `is:${key}`
}

// facetsOf lists the chips with any worktree in them, across every clone's linked
// worktrees, with query, the toolbar's filter.
export function facetsOf(repos: readonly WorktreeRepo[], query: string): Chip[] {
  const rows = repos.flatMap((repo) => repo.worktrees.filter((wt) => !wt.main))

  return facets.flatMap(({ key, label, tone, test, what }) => {
    const n = rows.filter(test).length
    const on = facetOn(query, key)

    return n === 0
      ? []
      : [
          {
            key,
            label,
            tone,
            n,
            on,
            title: on
              ? 'Clear the filter'
              : `Show the ${counted(n, 'worktree', 'worktrees')} that ${n === 1 ? what[0] : what[1]} (is:${key})`,
          },
        ]
  })
}
