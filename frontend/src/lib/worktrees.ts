// The Worktrees page's logic, pure: where each worktree stands (its lane, which colors its
// edge and orders it), its state and work in a few words, the one action that fits it, why
// a removal can't run, what removing it would cost, and each clone's heading. The backend
// decides what may be removed (Worktree's can* flags); this only words it. A lane depends
// on the Worktree alone, so the toolbar's filter counts what the page shows; the sessions
// the window follows only add words. The smaller parts, what works in a worktree and the
// page's chips among them, are in lib/worktreeParts.

import type { AgentID, AgentSession, LinkedItem, Worktree, WorktreeRepo } from '../platform'
import { counted, formatCount } from './format'
import { homeWord, shortCommit } from './git'
import { capitalize, formatAgo } from './time'
import { sentence } from './update'
import {
  agentLine,
  agoWords,
  byHand,
  clockOf,
  copiedOf,
  itemRef,
  prOfCopies,
  running,
  runsIn,
  unpushedKnown,
  unpushedUnknown,
  type Copied,
  type Tone,
} from './worktreeParts'

// Lane is the one place a row stands: something runs in it, it needs a look (done on
// GitHub, but it holds work that isn't), it holds work, it's open, it's ready to clean up,
// or it has a problem (its folder is gone, git couldn't read it, or it's locked by hand).
export type Lane = 'running' | 'review' | 'risk' | 'open' | 'ready' | 'problem'

// LaneInfo is how a lane shows: its place in the attention order, its words for a screen
// reader, and its edge's color, '' for none.
export interface LaneInfo {
  rank: number
  words: string
  tone: Tone | ''
}

export const lanes: Readonly<Record<Lane, LaneInfo>> = {
  running: { rank: 0, words: 'running', tone: 'info' },
  review: { rank: 1, words: 'needs a look', tone: 'warning' },
  risk: { rank: 2, words: 'holds work', tone: 'warning' },
  open: { rank: 3, words: 'open', tone: '' },
  ready: { rank: 4, words: 'ready to clean up', tone: 'success' },
  problem: { rank: 5, words: 'problem', tone: 'danger' },
}

// laneOf is where a worktree stands.
export function laneOf(wt: Worktree): Lane {
  if (wt.error !== '' || wt.prunable || byHand(wt)) {
    return 'problem'
  }

  if (running(wt)) {
    return 'running'
  }

  if (wt.stage === 'needs-review') {
    return 'review'
  }

  if (wt.stage === 'finished') {
    return 'ready'
  }

  return wt.dirtyCount > 0 || unpushedKnown(wt) > 0 || unpushedUnknown(wt) ? 'risk' : 'open'
}

// edgeTitle is the tooltip of a row's edge, which says why it stands in its lane; '' for
// an open one, which has no edge.
export function edgeTitle(wt: Worktree): string {
  switch (laneOf(wt)) {
    case 'running':
      return 'Something runs in it'
    case 'review':
      return `Its ${wt.item?.kind === 'issue' ? 'issue' : 'pull request'} is done, but it holds work that isn’t on GitHub`
    case 'risk':
      return 'It holds work that isn’t on GitHub yet'
    case 'ready':
      return 'Done on GitHub; cleaning it up loses nothing'
    case 'open':
      return ''
    case 'problem':
      return wt.prunable ? 'Its folder is gone' : byHand(wt) ? 'Locked by hand' : 'git couldn’t read it'
  }
}

// attentionSort orders a clone's rows by attention: running, needs a look, holds work,
// open, ready to clean up, then the rest; each lane newest commit first, and otherwise as
// listed.
export function attentionSort(rows: readonly Worktree[]): Worktree[] {
  return rows
    .map((wt, i) => ({ wt, i, rank: lanes[laneOf(wt)].rank }))
    .sort((a, b) => a.rank - b.rank || b.wt.lastCommitUnix - a.wt.lastCommitUnix || a.i - b.i)
    .map(({ wt }) => wt)
}

// StateGlyph is the state column's one mark: an icon, a still dot, or a pinging one.
export type StateGlyph = 'alert' | 'ghost' | 'ping' | 'lock' | 'dot' | 'up' | 'ring' | 'check'

// RowState is the state column of a row: its mark, the mark's color, and the words for its
// tooltip and a screen reader.
export interface RowState {
  glyph: StateGlyph
  tone: Tone
  words: string
}

// stateOf is a row's state, the first that holds: git couldn't read it, its folder is
// gone, something runs in it, it's locked by hand or by an ended session, it has
// uncommitted files, commits not on GitHub, or commits it couldn't check; else it's clean,
// its commits on GitHub by their commit IDs or as copies (copiedOf).
export function stateOf(wt: Worktree): RowState {
  if (wt.error !== '') {
    return { glyph: 'alert', tone: 'danger', words: 'git couldn’t read it' }
  }

  if (wt.prunable) {
    return { glyph: 'ghost', tone: 'danger', words: 'its folder is gone' }
  }

  if (running(wt)) {
    return { glyph: 'ping', tone: 'info', words: 'something runs in it' }
  }

  if (byHand(wt)) {
    return { glyph: 'lock', tone: 'danger', words: 'locked by hand' }
  }

  if (wt.staleLock) {
    return { glyph: 'lock', tone: 'muted', words: 'locked by a session that has ended' }
  }

  if (wt.dirtyCount > 0) {
    return { glyph: 'dot', tone: 'warning', words: 'uncommitted changes' }
  }

  if (unpushedKnown(wt) > 0) {
    return { glyph: 'up', tone: 'caution', words: 'commits that aren’t on GitHub' }
  }

  if (unpushedUnknown(wt)) {
    return { glyph: 'ring', tone: 'muted', words: 'couldn’t tell whether every commit is on GitHub' }
  }

  const copied = copiedOf(wt)

  return {
    glyph: 'check',
    tone: wt.stage === 'finished' ? 'success' : 'muted',
    words:
      copied === null
        ? 'clean, and every commit is on GitHub'
        : `clean; each commit’s change ${wordsText(copiedWords(copied))}`,
  }
}

// WorkNote is one note of the work column: uncommitted files, commits not on GitHub (↑),
// commits it couldn't count (↑?), or commits on GitHub only as copies (⧉); its text, the
// words a screen reader hears before it, and its tooltip.
export interface WorkNote {
  kind: 'dirty' | 'up' | 'unknown' | 'copied'
  tone: Tone
  text: string
  sr: string
  title: string
}

// copiedText is the work column's note on commits on GitHub only as copies, after ⧉: how
// many, and the pull request they went into, as in "10 on #42", else the branch their copies
// are on, "10 on feature-x".
function copiedText(c: Copied): string {
  return `${formatCount(c.n)} on ${c.pr > 0 ? `#${String(c.pr)}` : c.branch}`
}

// copiedTitle says why a worktree's commits not on GitHub by their commit IDs are on GitHub
// anyway, as the work column's tooltip: "Its 10 commits aren’t on GitHub by ID, but each
// one’s change is on origin/feature-x (PR #42)", or, once that branch no longer holds them
// all, went into the pull request, as the app recorded (copiedLater).
function copiedTitle(c: Copied): string {
  const lead = `Its ${counted(c.n, 'commit isn’t', 'commits aren’t')} on GitHub by ID, but ${c.n === 1 ? 'its' : 'each one’s'} change`

  if (c.recorded) {
    return `${lead} went into ${prOfCopies(c)}${wordsText(copiedLater(c))}`
  }

  return `${lead} is on ${c.ref}${c.pr > 0 ? ` (${prOfCopies(c)})` : ''}`
}

// upTitle is the tooltip of the commits not on GitHub, n: how many, then how they compare
// with the pushed branches when the backend compared them (copyCounts), and why it couldn't,
// or why their identical copies don't count (Copies.note).
function upTitle(wt: Worktree, n: number): string {
  const copies = wordsText(copyCounts(wt))
  const note = copyNote(wt)
  const title = `${counted(n, 'commit isn’t', 'commits aren’t')} on GitHub${copies === '' ? '' : `: ${copies}`}`

  return note === '' ? title : `${title}. ${note}`
}

// workNotes are the work column's notes, none for a clean worktree with every commit on
// GitHub by its commit ID: the state's ✓ says that already. Commits on GitHub only as
// copies get a muted note, which says where.
export function workNotes(wt: Worktree): WorkNote[] {
  const out: WorkNote[] = []
  const commits = unpushedKnown(wt)
  const copied = copiedOf(wt)

  if (wt.dirtyCount > 0) {
    out.push({
      kind: 'dirty',
      tone: 'warning',
      text: counted(wt.dirtyCount, 'file', 'files'),
      sr: 'uncommitted: ',
      title: `${counted(wt.dirtyCount, 'file has', 'files have')} uncommitted changes, counting untracked files`,
    })
  }

  if (commits > 0) {
    out.push({
      kind: 'up',
      tone: 'caution',
      text: formatCount(commits),
      sr: 'not on GitHub: ',
      title: upTitle(wt, commits),
    })
  } else if (copied !== null) {
    out.push({
      kind: 'copied',
      tone: 'muted',
      text: copiedText(copied),
      sr: 'on GitHub as copies: ',
      title: copiedTitle(copied),
    })
  } else if (unpushedUnknown(wt)) {
    out.push({
      kind: 'unknown',
      tone: 'muted',
      text: '?',
      sr: 'commits not on GitHub: ',
      title: 'Couldn’t count the commits that aren’t on GitHub',
    })
  }

  return out
}

// Verb is a row's one action that fits its state, always bordered: Clean up, Unlock, or a
// launch on its item (the launch control names its agent), with its tooltip.
export type Verb =
  | { act: 'cleanUp'; label: 'Clean up'; tone: 'success'; title: string }
  | { act: 'unlock'; label: 'Unlock'; tone: ''; title: string }
  | { act: 'launch'; title: string }

// verbOf is the verb of a worktree, or null for one with nothing to do: Clean up for a
// finished one, Unlock for a lock its ended session left, and a launch on an open item the
// inbox lists (listed) when nothing runs in it and it isn't locked by hand. The launch
// resumes the worktree only when it is the item's own, linked by its record, name, or
// branch made for the item (linkedBy 'name'): for one linked by the pushed branch it is on,
// or by its commits' copies, a launch on the item would make or resume the item's own
// worktree instead, so none is offered.
export function verbOf(wt: Worktree, listed: boolean): Verb | null {
  if (wt.canCleanUp) {
    const what = wt.staleLock ? 'Releases the ended session’s lock, then removes' : 'Removes'
    const every = copiedOf(wt) === null ? 'every commit' : 'every commit’s change'

    return {
      act: 'cleanUp',
      label: 'Clean up',
      tone: 'success',
      title: `${what} the worktree, whose ${every} is on GitHub; asks which branches go with it`,
    }
  }

  if (wt.canUnlock) {
    return {
      act: 'unlock',
      label: 'Unlock',
      tone: '',
      title: 'Releases the lock of the Claude session that ended here, so it can be removed',
    }
  }

  if (wt.item?.state === 'open' && wt.linkedBy === 'name' && !running(wt) && !byHand(wt) && listed) {
    return {
      act: 'launch',
      title: `Launches on ${itemRef(wt.item)} in this worktree, which resumes it (your launch defaults)`,
    }
  }

  return null
}

// removeWhy says why Remove… (or, with force, Force remove…) can't run on a worktree, or
// '' when it can. The backend decides (canRemove, canForceRemove); this words it, so it is
// '' only when the backend allows it.
export function removeWhy(wt: Worktree, force: boolean, agentName: (id: AgentID) => string): string {
  if (wt.prunable) {
    return 'Its folder is gone: Prune clears the entry'
  }

  if (wt.error !== '') {
    return 'git couldn’t read it'
  }

  if (running(wt)) {
    return capitalize(runsIn(wt, agentName))
  }

  if (byHand(wt)) {
    return 'It’s locked by hand'
  }

  if (wt.locked) {
    return 'A Claude session that has ended still locks it: Unlock releases it'
  }

  if (!force) {
    if (wt.dirtyCount > 0) {
      return 'It has uncommitted files: Force remove… discards them'
    }

    // The backend refuses it for a reason the listing doesn't show.
    return wt.canRemove ? '' : 'It can’t be removed right now: Check again'
  }

  if (wt.dirtyCount === 0) {
    return 'Nothing to discard: Remove… takes it'
  }

  // The backend refuses it for a reason the listing doesn't show, as while it can't list
  // the running processes.
  return wt.canForceRemove ? '' : 'It can’t be force-removed right now: Check again'
}

// Words is a sentence as the page shows it, with no markup: plain text, and the parts set
// in code, such as a branch's name.
export type Words = readonly (string | { code: string })[]

// wordsText is a sentence's text alone, its code parts as plain text.
export function wordsText(words: Words): string {
  return words.map((part) => (typeof part === 'string' ? part : part.code)).join('')
}

// joined puts sentences one after another, a space between, leaving out the empty ones.
function joined(...sentences: Words[]): Words {
  return sentences.filter((s) => s.length > 0).flatMap((s, i) => (i === 0 ? s : [' ', ...s]))
}

// copiedWords say where a worktree's commits went as copies (copiedOf), after "every
// commit’s change": "is on GitHub, copied onto PR #42’s branch", the pull request that
// branch heads, or onto the branch itself when no pull request is known to; once what the
// app recorded vouches for them, "went into PR #42".
function copiedWords(c: Copied): Words {
  if (c.recorded) {
    return [`went into ${prOfCopies(c)}`]
  }

  return c.pr > 0
    ? [`is on GitHub, copied onto ${prOfCopies(c)}’s branch`]
    : ['is on GitHub, copied onto ', { code: c.ref }]
}

// copiedLater says, after the pull request a worktree's copies went into as the app
// recorded them, what became of the branch they were on since: ", from origin/feature-x, a
// branch that’s gone now", or "; origin/feature-x no longer holds all of them", as a merged
// pull request's branch may not, deleted or not.
function copiedLater(c: Copied): Words {
  return c.gone
    ? [', from ', { code: c.ref }, ', a branch that’s gone now']
    : ['; ', { code: c.ref }, ' no longer holds all of them']
}

// copyCounts say how a worktree's commits not on GitHub compare with the pushed branch the
// backend compared them with, when some have a copy there: "7 have an identical copy on
// origin/feature-x, 2 only an edited one, 1 none". An edited copy has another change, so
// its commit's own isn't on GitHub. None when every commit is on GitHub, when none has a
// copy there, or when the comparison counted other commits than these.
function copyCounts(wt: Worktree): Words {
  const c = wt.copies
  const n = unpushedKnown(wt)

  if (n === 0 || c.count !== n || c.ref === '' || c.identical + c.edited === 0) {
    return []
  }

  const have = (k: number) => `${formatCount(k)} ${k === 1 ? 'has' : 'have'}`
  const on: Words = [' on ', { code: c.ref }]
  const parts: Words[] = []

  if (c.identical > 0) {
    parts.push([`${have(c.identical)} an identical copy`, ...on])
  }

  if (c.edited > 0) {
    parts.push(
      c.identical > 0
        ? [`${formatCount(c.edited)} only an edited one`]
        : [`${have(c.edited)} only an edited copy`, ...on],
    )
  }

  if (c.none > 0) {
    parts.push([`${formatCount(c.none)} none`])
  }

  return parts.flatMap((p, i) => (i === 0 ? p : [', ', ...p]))
}

// copyNote is the backend's sentence on why it couldn't compare a worktree's commits not on
// GitHub with the pushed branches, or why their identical copies don't count, or '' when it
// has none, or every commit is on GitHub.
function copyNote(wt: Worktree): string {
  const n = unpushedKnown(wt)

  return n > 0 && wt.copies.count === n ? wt.copies.note : ''
}

// copySentences say, after the commits not on GitHub, how they compare with the pushed
// branches (copyCounts), and the backend's note on it.
function copySentences(wt: Worktree): Words {
  const counts = copyCounts(wt)
  const note = copyNote(wt)

  return joined(counts.length === 0 ? [] : [...counts, '.'], note === '' ? [] : [note])
}

// copiedWhen says where pull request pr, which a worktree's copies went into, stands, after
// it: ", merged 2d ago", ", closed", or ", which is open"; nothing when the worktree isn't
// linked to it, so its state isn't known.
function copiedWhen(pr: LinkedItem | null, now: Date): string {
  if (pr === null) {
    return ''
  }

  return pr.state === 'open' ? ', which is open' : `, ${pr.state}${agoWords(now, pr.closedAt)}`
}

// lostOriginals says a detached worktree's original commits, which no branch holds, go with
// it, when their changes are on GitHub as copies (copied).
const lostOriginals: Words = ['Only the original commits, which no branch holds, go with it.']

// originals says what becomes of the commits of a worktree whose changes are on GitHub as
// copies when it goes: a detached one's, which no branch holds, go with it; any other's stay
// on its branch, which removing it keeps unless that branch may go with it, which the
// confirmation asks.
function originals(wt: Worktree): Words {
  if (wt.detached) {
    return lostOriginals
  }

  if (wt.branches.some((b) => b.name === wt.branch && b.deletable)) {
    return []
  }

  return ['Removing it keeps its branch, ', { code: wt.branch }, ', with the original commits.']
}

// uncountedWords say the commits not on GitHub couldn't be counted, and cleanButUncounted
// says so of a worktree with nothing uncommitted, after a lead such as "Once it ends,".
const uncountedWords = 'Couldn’t count the commits that aren’t on GitHub, so removing it might lose some.'
const cleanButUncounted =
  'its folder is clean, but the commits that aren’t on GitHub couldn’t be counted, so removing it might lose some.'

// SafetyIcon is the mark of what removing a worktree would cost.
export type SafetyIcon = 'alert' | 'ghost' | 'info' | 'lock' | 'up' | 'ring' | 'shield'

// Safety is what removing a worktree would cost, in one sentence and a tone: text, then,
// when there's more to say, also, muted. command is a command to copy, '' for none.
export interface Safety {
  tone: 'danger' | 'info' | 'warning' | 'success'
  icon: SafetyIcon
  text: Words
  also: Words
  command: string
}

// safety says what removing a worktree of repo would cost, now, as the drawer and the
// confirmations show it, with session, the session the app follows there, and agentName.
// An uncommitted file goes with the folder. A commit that isn't on GitHub stays on its
// branch, which a removal keeps (Branch.deletable is false while it holds one); only a
// detached worktree's commits, which no branch holds, go too. Commits on GitHub only as
// identical copies (copiedOf) lose nothing but their own commit IDs; an edited copy doesn't
// count, and is only said of the commits that aren't on GitHub (copySentences).
export function safety(
  wt: Worktree,
  repo: WorktreeRepo | null,
  now: Date,
  session: AgentSession | undefined,
  agentName: (id: AgentID) => string,
): Safety {
  const files = wt.dirtyCount
  const commits = unpushedKnown(wt)
  const copied = copiedOf(wt)
  // everyCommit says every commit is on GitHub, by its change when it is there as a copy.
  const everyCommit = copied === null ? 'every commit is on GitHub' : 'every commit’s change is on GitHub'
  const copies = copySentences(wt)
  // originalsGo says a detached worktree's original commits go with it, when their changes
  // are on GitHub as copies.
  const originalsGo: Words = copied !== null && wt.detached ? lostOriginals : []
  const ftext = counted(files, 'uncommitted file', 'uncommitted files')
  const ctext = counted(commits, 'commit', 'commits')
  // kept says the branch keeps the commits, starting with lead ("Its" or "its").
  const kept = (lead: string): Words =>
    commits > 0 && !wt.detached
      ? [
          `${lead} ${ctext} not on GitHub ${commits === 1 ? 'stays' : 'stay'} in the clone on `,
          { code: wt.branch },
          ', which removing it keeps.',
        ]
      : []
  const lost: Words =
    commits > 0 && wt.detached ? [`${capitalize(ctext)} that no branch holds would be lost with it.`] : []
  // uncounted says the commits not on GitHub couldn't be counted, so none is said to be.
  const unknown = unpushedUnknown(wt)
  const uncounted: Words = unknown ? [uncountedWords] : []
  const doneWithout: Words =
    wt.stage === 'needs-review' && wt.item !== null
      ? [`${itemRef(wt.item)} ${wt.item.state}${agoWords(now, wt.item.closedAt)}, without this work.`]
      : []
  const result = (tone: Safety['tone'], icon: SafetyIcon, text: Words, also: Words = []): Safety => ({
    tone,
    icon,
    text,
    also,
    command: '',
  })

  if (wt.error !== '') {
    return result(
      'danger',
      'alert',
      ['Couldn’t read this worktree, so nothing here can say what removing it would cost.'],
      [sentence(capitalize(wt.error))],
    )
  }

  if (wt.prunable) {
    const why = wt.prunableReason === '' ? '' : ` (${wt.prunableReason})`

    return result(
      'danger',
      'ghost',
      ['Its folder is gone, so there’s nothing left in it to lose.'],
      [
        `git still lists it${why}. Prune ${repo?.name ?? 'the clone'} clears the entry; it touches no worktree that exists, and no branch.`,
      ],
    )
  }

  if (running(wt)) {
    const a = agentLine(wt, session, agentName)
    const pid = a !== null && a.kind !== 'past' && a.pid > 0 ? `pid ${String(a.pid)}` : ''
    const since = a !== null && a.kind !== 'past' ? a.since : ''
    const started = since === '' ? '' : `started ${clockOf(since)}, ${formatAgo(now, since)}`
    const bits = [pid, started].filter((b) => b !== '').join(', ')
    const then: Words =
      files > 0
        ? joined(
            [`Once it ends, removing it would discard ${ftext}.`],
            kept('Its'),
            lost,
            copies,
            uncounted,
            originalsGo,
          )
        : commits > 0
          ? joined(['Once it ends,'], kept('its'), lost, copies)
          : unknown
            ? [`Once it ends, ${cleanButUncounted}`]
            : joined([`Once it ends, nothing in it would be lost: ${everyCommit}.`], originalsGo)

    return result(
      'info',
      'info',
      [`Can’t be removed while ${runsIn(wt, agentName)}${bits === '' ? '' : ` (${bits})`}.`],
      then,
    )
  }

  if (byHand(wt)) {
    const then: Words =
      files > 0
        ? joined([`Then removing it would discard ${ftext}.`], kept('Its'), lost, copies, uncounted, originalsGo)
        : commits > 0
          ? joined(['Then'], kept('its'), lost, copies)
          : unknown
            ? [`After that, ${cleanButUncounted}`]
            : joined([`After that, nothing in it would be lost: ${everyCommit}.`], originalsGo)

    return {
      ...result('danger', 'lock', ['Locked by hand, so it can’t be removed here. Unlock it first:'], then),
      command: `git -C ${homeWord(repo?.displayPath ?? '')} worktree unlock ${homeWord(wt.displayPath)}`,
    }
  }

  if (files > 0) {
    return result(
      'warning',
      'alert',
      [`Removing it would discard ${ftext}, counting untracked ones.`],
      joined(kept('Its'), lost, copies, uncounted, originalsGo, doneWithout),
    )
  }

  if (lost.length > 0) {
    return result('warning', 'alert', lost, joined(copies, ['Its folder is clean.']))
  }

  if (commits > 0) {
    const one = commits === 1

    return result(
      'warning',
      'up',
      [`Its folder is clean, but ${ctext} on `, { code: wt.branch }, ` ${one ? 'isn’t' : 'aren’t'} on GitHub.`],
      joined(copies, doneWithout, [
        `Removing the worktree keeps that branch, so ${one ? 'it stays' : 'they stay'} in the clone; the app won’t delete it while ${one ? 'it’s' : 'they’re'} only here.`,
      ]),
    )
  }

  if (unknown) {
    return result('warning', 'ring', [uncountedWords], ['Its folder is clean.'])
  }

  const stale: Words = wt.staleLock
    ? [
        `A Claude session that has ended still locks it; ${wt.canCleanUp ? 'Clean up' : 'Unlock'} releases the lock first.`,
      ]
    : []

  if (copied !== null) {
    // Once what the app recorded vouches for them, it says where their pull request stands,
    // and that their branch no longer holds them all, unless it's gone.
    const where: Words = copied.recorded
      ? [copiedWhen(copied.item, now), ...(copied.gone ? [] : copiedLater(copied))]
      : []

    return result(
      'success',
      'shield',
      ['Safe to remove: no uncommitted files, and every commit’s change ', ...copiedWords(copied), ...where, '.'],
      joined(originals(wt), stale),
    )
  }

  const { item } = wt
  const head =
    item !== null && item.headOid !== '' && item.headOid === wt.head ? ` (it’s at ${itemRef(item)}’s head)` : ''

  return result(
    'success',
    'shield',
    [`Safe to remove: no uncommitted files, and every commit is on GitHub${head}.`],
    stale,
  )
}

// MainNote is the clone heading's note on its main checkout: the branch it's on, whether
// it holds uncommitted work (dirty), and the note's color: muted while clean, amber while
// dirty, and red when git couldn't read it.
export interface MainNote {
  text: string
  dirty: boolean
  tone: 'muted' | 'warning' | 'danger'
  title: string
}

// LaneCount is how many of a clone's worktrees stand in a lane, as its heading counts
// them; a worktree that needs a look counts as at risk.
export interface LaneCount {
  lane: 'running' | 'risk' | 'ready' | 'problem'
  n: number
  words: string
  tone: Tone
}

// CloneSummary is a clone's heading: the owner, from a linked item ('' with none), its
// name, its main checkout as a note (null without one), its lanes counted, and how many
// linked worktrees it has.
export interface CloneSummary {
  owner: string
  name: string
  note: MainNote | null
  lanes: readonly LaneCount[]
  count: number
}

// counts are the lanes a clone's heading counts, in order, with their words and colors.
const counts: readonly (readonly [LaneCount['lane'], string, Tone])[] = [
  ['running', 'running', 'info'],
  ['risk', 'at risk', 'warning'],
  ['ready', 'ready', 'success'],
  ['problem', 'problem', 'danger'],
]

// cloneSummary sums up a clone for its heading.
export function cloneSummary(repo: WorktreeRepo): CloneSummary {
  const main = repo.worktrees.find((wt) => wt.main)
  const linked = repo.worktrees.filter((wt) => !wt.main)
  const owner = linked.find((wt) => wt.item !== null)?.item?.owner ?? ''
  const n = new Map<Lane, number>()

  for (const wt of linked) {
    const lane = laneOf(wt)
    const key = lane === 'review' ? 'risk' : lane

    n.set(key, (n.get(key) ?? 0) + 1)
  }

  const laneCounts = counts.flatMap(([lane, words, tone]) => {
    const count = n.get(lane) ?? 0

    return count === 0 ? [] : [{ lane, n: count, words: lane === 'problem' && count > 1 ? 'problems' : words, tone }]
  })

  let note: MainNote | null = null

  if (main !== undefined) {
    const on = main.detached ? `detached at ${shortCommit(main.head)}` : `on ${main.branch}`

    if (main.error !== '') {
      note = {
        text: `${on} · couldn’t read`,
        dirty: false,
        tone: 'danger',
        title: sentence(`Couldn’t read the main checkout: ${main.error}`),
      }
    } else if (main.dirtyCount > 0) {
      note = {
        text: `${on} · ${formatCount(main.dirtyCount)} uncommitted`,
        dirty: true,
        tone: 'warning',
        title: `The main checkout is ${on}, with ${counted(main.dirtyCount, 'uncommitted file', 'uncommitted files')}`,
      }
    } else {
      note = {
        text: `${on} · clean`,
        dirty: false,
        tone: 'muted',
        title: `The main checkout is ${on}, with nothing uncommitted`,
      }
    }
  }

  return { owner, name: repo.name, note, lanes: laneCounts, count: linked.length }
}
