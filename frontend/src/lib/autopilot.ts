// Autopilot as the window words it: the modes, the tab's count and dots, a routine's status
// line, the ⚡ chip of an item a routine touched, the question asked as a routine is turned
// on, and Activity's lines, by day, with its filters. Every function here is pure; the
// components and hooks under components/autopilot read and change Autopilot.

import { brand } from '../generated/brand'
import type {
  AutopilotActivityDay,
  AutopilotActivityPage,
  AutopilotEffect,
  AutopilotEngine,
  AutopilotGitHubActionKind,
  AutopilotGlance,
  AutopilotGroup,
  AutopilotHold,
  AutopilotItemRef,
  AutopilotLeftAlone,
  AutopilotMatch,
  AutopilotMatching,
  AutopilotMode,
  AutopilotReadiness,
  AutopilotReadyCheck,
  AutopilotReason,
  AutopilotRoutine,
  AutopilotRoutineReady,
  AutopilotStepKind,
  AutopilotTarget,
} from '../platform'
import { counted, formatCount } from './format'

// modeLabels name the modes, from the most careful.
export const modeLabels: Readonly<Record<AutopilotMode, string>> = {
  dryRun: 'Dry run',
  ask: 'Ask first',
  auto: 'Autopilot',
}

// modeHelp says what each mode does with a match.
export const modeHelp: Readonly<Record<AutopilotMode, string>> = {
  dryRun: 'Records what it would do, and does nothing else.',
  ask: 'Runs its local steps at once, and waits for your OK before anything else.',
  auto: 'Runs every step at once, within your limits.',
}

// modeOrder is the modes as the controls offer them.
export const modeOrder: readonly AutopilotMode[] = ['dryRun', 'ask', 'auto']

// modeLabel names a mode, or the mode itself for one this build doesn't know.
export function modeLabel(mode: AutopilotMode): string {
  return (modeLabels as Readonly<Record<string, string | undefined>>)[mode] ?? mode
}

// elsewhereText says that another copy of the app runs Autopilot.
export const elsewhereText = `Autopilot is running in another copy of ${brand.name}. This one shows them and starts nothing.`

// pausedText says what Pause Autopilot does while it is on.
export const pausedText = 'Autopilot is paused. Nothing new starts.'

// signedOutText says why nothing starts while no one is signed in to GitHub.
export const signedOutText =
  'No one is signed in to GitHub, so Autopilot starts nothing, its snoozes and pins included, until someone is.'

// AutopilotBadge is what the Autopilot tab counts: the agents working, an amber dot while
// something waits for the user, a red dot while a routine is broken or can't run, and what
// they mean, for its tooltip.
export interface AutopilotBadge {
  count: number
  attention: boolean
  failed: boolean
  title: string
}

// troubled is how many of the user's routines are broken or have a problem the engine
// found, such as a search GitHub fails: a routine of another account is none of the user's.
export function troubled(routines: readonly AutopilotRoutine[], engine: AutopilotEngine | null): number {
  const mine = routines.filter((r) => !r.otherAccount)
  const problems = new Set((engine?.problems ?? []).map((p) => p.routineId))

  return mine.filter((r) => r.broken !== '' || problems.has(r.id)).length
}

// autopilotBadge works out the tab's badge from Autopilot at a glance, the routines, and
// the engine; each may still be loading (null, or none).
export function autopilotBadge(
  glance: AutopilotGlance | null,
  routines: readonly AutopilotRoutine[],
  engine: AutopilotEngine | null,
): AutopilotBadge {
  const working = glance?.working.length ?? 0
  const waiting = glance?.needsYou ?? 0
  const broken = troubled(routines, engine)
  const title = [
    working > 0 ? counted(working, 'agent working', 'agents working') : '',
    waiting > 0 ? `${formatCount(waiting)} ${waiting === 1 ? 'needs' : 'need'} you` : '',
    broken > 0 ? `${counted(broken, 'routine', 'routines')} can’t run` : '',
  ]
    .filter((part) => part !== '')
    .join(' · ')

  return { count: working, attention: waiting > 0, failed: broken > 0, title }
}

// touched reports a match that did something with its item, or waits to: every match but
// a baseline and one that closed as the item or the routine moved on.
export function touched(m: AutopilotMatch): boolean {
  return m.state !== 'baseline' && m.state !== 'closed'
}

// StatusLine is a routine's line under its name: what it says, its tone, and, for "ran 12
// minutes ago", the time it ran, which the line shows relative to now.
export interface StatusLine {
  text: string
  tone: 'muted' | 'warning' | 'danger'
  since?: string
}

// routineStatus is a routine's status line: whose it is when it is another account's, why
// it is broken, what this Mac lacks for it (notReady), why it can't run, its last error,
// then what it is doing (working, waiting), what it did today, or when it last ran (or, a
// Dry run's, would have), from its own figures; a routine that is off says only its mode.
export function routineStatus(r: AutopilotRoutine, working: number, problem: string, notReady = ''): StatusLine {
  const figures = r.figures
  const mode = modeLabel(r.mode)

  if (r.otherAccount) {
    return { text: `Belongs to @${r.account}`, tone: 'muted' }
  }

  if (r.broken !== '') {
    return { text: r.broken, tone: 'danger' }
  }

  if (notReady !== '' && r.enabled) {
    return { text: notReady, tone: 'danger' }
  }

  if (problem !== '') {
    return { text: problem, tone: 'danger' }
  }

  if (r.lastError !== '') {
    return { text: r.lastError, tone: 'danger' }
  }

  if (!r.enabled) {
    return { text: `${mode} · off`, tone: 'muted' }
  }

  if (working > 0) {
    return { text: `${mode} · ${formatCount(working)} working`, tone: 'muted' }
  }

  if (figures.waiting > 0) {
    return { text: `${mode} · ${formatCount(figures.waiting)} waiting`, tone: 'warning' }
  }

  if (figures.today > 0) {
    return { text: `${mode} · ${formatCount(figures.today)} today`, tone: 'muted' }
  }

  if (figures.wouldRunToday > 0) {
    return { text: `${mode} · ${formatCount(figures.wouldRunToday)} would run today`, tone: 'muted' }
  }

  if (figures.lastAt !== null) {
    return {
      text: `${mode} · ${figures.lastDryRun ? 'would have run' : 'ran'}`,
      tone: 'muted',
      since: figures.lastAt,
    }
  }

  return { text: `${mode} · nothing matched yet`, tone: 'muted' }
}

// canSwitch reports a routine the user can switch on or off here: one of theirs that
// loads.
export function canSwitch(r: AutopilotRoutine): boolean {
  return !r.otherAccount && r.broken === ''
}

// effectVerbs name what a local step did, as Activity's lines do.
const effectVerbs: Readonly<Partial<Record<AutopilotStepKind, string>>> = {
  pin: 'Pinned',
  snooze: 'Snoozed',
  dismiss: 'Dismissed',
  restore: 'Restored',
}

// effectVerb names what a local step did: "Snoozed".
export function effectVerb(action: AutopilotStepKind): string {
  return effectVerbs[action] ?? action.charAt(0).toUpperCase() + action.slice(1)
}

// effectState says where a local step's change stands: taken back by Undo, or by the user,
// or still there.
export function effectState(e: AutopilotEffect): string {
  if (e.undoneAt !== null) {
    return 'Undone'
  }

  if (e.reversedAt !== null) {
    return 'You took it back'
  }

  return e.canUndo ? 'Still there' : 'Changed since'
}

// Chip is what an item's ⚡ chip says, and its tone.
export interface Chip {
  label: string
  tone: 'muted' | 'accent' | 'warning' | 'danger'
}

// notices are the steps that tell the user something.
const notices: readonly AutopilotStepKind[] = ['notify', 'tellMe']

// openChips are what a match that is still open says, by its state: it waits for the user,
// for this Mac, for a limit, or works.
const openChips: Readonly<Record<'waiting' | 'blocked' | 'queued' | 'running' | 'verifying', Chip>> = {
  waiting: { label: 'Needs you', tone: 'warning' },
  blocked: { label: 'Needs you', tone: 'warning' },
  queued: { label: 'Queued', tone: 'muted' },
  running: { label: 'Working', tone: 'accent' },
  verifying: { label: 'Checking', tone: 'accent' },
}

// closedChips are what a closed match says by its state, when its reason says no more.
const closedChips: Readonly<Record<'closed' | 'skipped' | 'dismissed', Chip>> = {
  closed: { label: 'Closed', tone: 'muted' },
  skipped: { label: 'Skipped', tone: 'muted' },
  dismissed: { label: 'You skipped it', tone: 'muted' },
}

// reasonChips are what a closed match says by why it closed, every reason the ledger has:
// a record over the generated union, so a reason added in Go fails to compile until it is
// worded here. A match that ran says so through its steps instead (chipOf).
export const reasonChips: Readonly<Record<AutopilotReason, Chip>> = {
  ran: { label: 'Ran', tone: 'accent' },
  would_run: { label: 'Would run', tone: 'muted' },
  tries: { label: 'Gave up', tone: 'danger' },
  outside_contributor: { label: 'Skipped: an outside contributor’s', tone: 'muted' },
  you_skipped: { label: 'You skipped it', tone: 'muted' },
  you_stopped: { label: 'Stopped by you', tone: 'muted' },
  merged: { label: 'Closed: it merged', tone: 'muted' },
  item_closed: { label: 'Closed: the item closed', tone: 'muted' },
  gone: { label: 'Closed: the item is gone', tone: 'muted' },
  left_watch: { label: 'Closed: it left the watch', tone: 'muted' },
  moved_on: { label: 'Closed: it moved on', tone: 'muted' },
  no_longer_matches: { label: 'Closed: no longer matches', tone: 'muted' },
  routine_changed: { label: 'Closed: the routine changed', tone: 'muted' },
  routine_off: { label: 'Closed: the routine was off', tone: 'muted' },
  not_offered: { label: 'Closed: its step isn’t offered', tone: 'muted' },
  failed: { label: 'A step failed', tone: 'danger' },
  from_now: { label: 'Already matched when you turned it on', tone: 'muted' },
  hands_off: { label: 'Left alone', tone: 'muted' },
}

// chipOf is what a match says, on an item's ⚡ chip (the newest a routine has on it,
// LatestMatches) and in the item's history: a Dry run's "Would run"; where an open match
// is; a baseline's "Already matched when you turned it on"; what a local step did
// ("Snoozed", "Pinned"), or that it was undone or taken back; a notice; or why a closed one
// closed. Each later kind of step brings its own words.
export function chipOf(m: AutopilotMatch): Chip {
  // A Dry run's baseline, as turning one on from now on records, would have run nothing,
  // and a Dry run's match that closed or was skipped says why, as a real one does.
  if (m.dryRun && m.state !== 'baseline' && m.state !== 'closed' && m.state !== 'skipped' && m.state !== 'dismissed') {
    return { label: 'Would run', tone: 'muted' }
  }

  switch (m.state) {
    case 'waiting':
    case 'blocked':
    case 'queued':
    case 'running':
    case 'verifying':
      return openChips[m.state]
    case 'baseline':
      return reasonChips.from_now
    case 'closed':
    case 'skipped':
    case 'dismissed':
      // A closed match never ran; one that says so anyway reads by its state alone.
      return m.reason === '' || m.reason === 'ran' || m.reason === 'would_run'
        ? closedChips[m.state]
        : reasonChips[m.reason]
    case 'done':
      break
  }

  const effect = m.effects.at(-1)

  if (effect !== undefined) {
    if (effect.undoneAt !== null) {
      return { label: 'Undone', tone: 'muted' }
    }

    if (effect.reversedAt !== null) {
      return { label: 'Taken back', tone: 'muted' }
    }

    return { label: effectVerb(effect.action), tone: 'accent' }
  }

  if (m.reason === 'failed') {
    return reasonChips.failed
  }

  const sent = githubChip(m)
  if (sent !== undefined) {
    return sent
  }

  if (m.steps.some((s) => notices.includes(s.kind))) {
    return { label: 'Told you', tone: 'accent' }
  }

  // A Read only agent in the background leaves notes: its final message.
  if (m.runs.some((r) => r.runner === 'background' && r.preset === 'readOnly' && r.outcome === 'done')) {
    return { label: 'Notes ready', tone: 'accent' }
  }

  // A match that is done ran, unless it says it closed for another reason.
  return m.reason === '' || m.reason === 'would_run' ? reasonChips.ran : reasonChips[m.reason]
}

// actionLabels name the GitHub step's actions, every one the catalog has: a record over the
// generated union, so an action added in Go fails to compile until it is worded here.
export const actionLabels: Readonly<Record<AutopilotGitHubActionKind, string>> = {
  rebase: 'Ask Dependabot to rebase',
  updateBranch: 'Update branch',
  reply: 'Comment one of my replies',
  labels: 'Add or remove labels',
}

// sentChips are what an item's chip says of the GitHub action a routine sent it last.
const sentChips: Readonly<Record<AutopilotGitHubActionKind, string>> = {
  rebase: 'Asked rebase',
  updateBranch: 'Updated branch',
  reply: 'Commented',
  labels: 'Labeled',
}

// githubChip is what a done match says of what it sent GitHub: the last action GitHub took
// ("Asked rebase", "Commented"), or that it marked the item's notifications done; undefined
// when it sent nothing.
function githubChip(m: AutopilotMatch): Chip | undefined {
  const last = m.runs.filter((r) => r.runner === 'github' && r.outcome === 'done').at(-1)
  const kind = last?.action as AutopilotGitHubActionKind | undefined

  if (kind !== undefined && kind in sentChips) {
    return { label: sentChips[kind], tone: 'accent' }
  }

  if (m.steps.some((s) => s.kind === 'markDone' && s.status === 'done')) {
    return { label: 'Marked done', tone: 'accent' }
  }

  return undefined
}

// githubAhead is the GitHub step a match is at and the actions of it still to send, or
// undefined when its next step isn't one.
export function githubAhead(
  m: Pick<AutopilotMatch, 'steps' | 'step'>,
): { actions: AutopilotGitHubActionKind[]; sent: number } | undefined {
  const st = m.steps.at(m.step)

  if (st?.kind !== 'github') {
    return undefined
  }

  return { actions: st.actions.slice(st.sent), sent: st.sent }
}

// githubAheadText says what a match waiting on a GitHub step would send next: "Ask
// Dependabot to rebase", "Comment one of my replies, then Add or remove labels"; '' when
// its next step isn't one.
export function githubAheadText(m: Pick<AutopilotMatch, 'steps' | 'step'>): string {
  const ahead = githubAhead(m)

  return ahead === undefined ? '' : ahead.actions.map((a) => actionLabels[a]).join(', then ')
}

// githubGoLabel is what the button that lets a waiting GitHub action go says, by what it
// waits on: the warnings its sheet showed, a first pass's backlog, an outside contributor's
// item, an action the quit cut off, or the routine's own OK; '' for a match whose next
// step isn't a GitHub step, or that waits on nothing the user decides.
export function githubGoLabel(m: Pick<AutopilotMatch, 'steps' | 'step' | 'state' | 'blockedBy'>): string {
  if (m.state !== 'waiting' || githubAhead(m) === undefined) {
    return ''
  }

  switch (m.blockedBy) {
    case 'warning':
      return 'Send anyway'
    case 'backlog':
      return 'Run on them'
    case 'interrupted':
      return 'Send it again'
    case 'outside_contributor':
    case 'ask':
    case 'catch_up':
      return 'Send it'
    default:
      return ''
  }
}

// routineName names a routine by its ID among those listed, or "a routine" for one that
// isn't, as a deleted one's mark can.
export function routineName(names: ReadonlyMap<number, string>, id: number): string {
  return names.get(id) ?? 'a routine'
}

// matchingWords asks the question as a routine is turned on or leaves Dry run: "3 pull
// requests match right now."; with none it can tell, "2 items may match right now.", and
// with nothing at all it could count, that not everything was read.
export function matchingWords(m: AutopilotMatching): string {
  if (m.count === 0 && m.unknown > 0) {
    return `${counted(m.unknown, 'item', 'items')} may match right now.`
  }

  if (m.count === 0 && m.incomplete) {
    return 'Not everything it watches could be read right now.'
  }

  const parts = [
    m.pullRequests > 0 ? counted(m.pullRequests, 'pull request', 'pull requests') : '',
    m.issues > 0 ? counted(m.issues, 'issue', 'issues') : '',
  ].filter((part) => part !== '')
  const what = parts.length === 0 ? counted(m.count, 'item', 'items') : parts.join(' and ')

  return `${what} ${m.count === 1 ? 'matches' : 'match'} right now.`
}

// ActivityResult is one of Activity's result filters: every line, what the local steps
// did, what a Dry run would have done, the notices, or what is worth knowing (what was
// left alone, what gave up, what failed).
export type ActivityResult = 'all' | 'actions' | 'dryRuns' | 'notices' | 'attention'

// resultLabels name the result filters.
export const resultLabels: Readonly<Record<ActivityResult, string>> = {
  all: 'Every result',
  actions: 'Snoozes, pins, and dismissals',
  dryRuns: 'Dry runs',
  notices: 'Notices',
  attention: 'Worth knowing',
}

// resultOrder is the result filters as Activity offers them.
export const resultOrder: readonly ActivityResult[] = ['all', 'actions', 'dryRuns', 'notices', 'attention']

// ActivityLine is one line of Activity: a routine's grouped local steps on a day ("Snoozed
// 6 · Undo"), one match ("Would run"), or an item a routine leaves alone ("Left alone ·
// Let it act again"); at is its time, which orders and dates it.
export type ActivityLine =
  | { kind: 'group'; key: string; at: string; group: AutopilotGroup }
  | { kind: 'match'; key: string; at: string; match: AutopilotMatch }
  | { kind: 'leftAlone'; key: string; at: string; left: AutopilotLeftAlone }

// lineResult is the result filter a line belongs to.
function lineResult(line: ActivityLine): Exclude<ActivityResult, 'all'> {
  switch (line.kind) {
    case 'group':
      return 'actions'
    case 'leftAlone':
      return 'attention'
    case 'match':
      if (line.match.dryRun) {
        return 'dryRuns'
      }

      if (line.match.state === 'skipped' || line.match.reason === 'failed') {
        return 'attention'
      }

      return line.match.steps.some((s) => notices.includes(s.kind)) ? 'notices' : 'actions'
  }
}

// lineRoutine is the routine a line is about.
export function lineRoutine(line: ActivityLine): number {
  switch (line.kind) {
    case 'group':
      return line.group.routineId
    case 'match':
      return line.match.routineId
    case 'leftAlone':
      return line.left.routineId
  }
}

// inActivity reports a match Activity lists, on a line of its own or a grouped one: one
// that ran, gave up, or the user skipped. An open match waiting on a limit waits in
// Queued, and one being started in Working, which later builds show; a baseline and a
// match that closed before it acted are left out.
export function inActivity(m: AutopilotMatch): boolean {
  return m.state === 'done' || m.state === 'skipped' || m.state === 'dismissed'
}

// notListedText says why Activity shows no line for a match a notification or a chip asked
// for: what it is instead.
export function notListedText(m: AutopilotMatch): string {
  return `Activity has no line for that one. ${chipOf(m).label}.`
}

// listed reports a match that takes a line of its own: one Activity lists that a grouped
// line doesn't stand for.
function listed(m: AutopilotMatch, grouped: ReadonlySet<number>): boolean {
  return !grouped.has(m.id) && inActivity(m)
}

// ActivityFilter is what Activity shows: one routine's lines (0 for every routine's), and
// one result.
export interface ActivityFilter {
  routine: number
  result: ActivityResult
}

// filters reports a filter that leaves any line out: one routine, or one result.
export function filters(f: ActivityFilter): boolean {
  return f.routine !== 0 || f.result !== 'all'
}

// activityLines are the lines of the pages loaded, newest first, narrowed by the filter:
// each day's grouped lines and the matches they don't stand for, and the items the
// routines leave alone, at the moment they were left alone.
export function activityLines(
  days: readonly AutopilotActivityDay[],
  leftAlone: readonly AutopilotLeftAlone[],
  filter: ActivityFilter,
): ActivityLine[] {
  const lines: ActivityLine[] = []
  // A step's line is on the day it ran, which may be a later one than its match's.
  const grouped = new Set(days.flatMap((day) => day.groups.flatMap((g) => g.matches)))

  for (const day of days) {
    for (const group of day.groups) {
      lines.push({ kind: 'group', key: `group:${group.key}`, at: group.latest, group })
    }

    for (const match of day.matches) {
      if (listed(match, grouped)) {
        lines.push({ kind: 'match', key: `match:${String(match.id)}`, at: match.closedAt ?? match.createdAt, match })
      }
    }
  }

  for (const left of leftAlone) {
    lines.push({
      kind: 'leftAlone',
      key: `left:${String(left.routineId)}:${String(left.item.id)}`,
      at: left.since,
      left,
    })
  }

  return lines
    .filter(
      (line) =>
        (filter.routine === 0 || lineRoutine(line) === filter.routine) &&
        (filter.result === 'all' || lineResult(line) === filter.result),
    )
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
}

// lineOf finds the line that shows the match id, a grouped one included, or undefined.
export function lineOf(lines: readonly ActivityLine[], id: number): ActivityLine | undefined {
  return lines.find(
    (line) =>
      (line.kind === 'match' && line.match.id === id) || (line.kind === 'group' && line.group.matches.includes(id)),
  )
}

// mergeGroups puts together a grouped line that two pages split: its counts, what Undo
// would put back, and its matches and items, under one line.
function mergeGroups(a: AutopilotGroup, b: AutopilotGroup): AutopilotGroup {
  const count = a.count + b.count

  return {
    ...a,
    words: `${effectVerb(a.action)} ${String(count)}`,
    count,
    undone: a.undone + b.undone,
    reversed: a.reversed + b.reversed,
    undo: [...a.undo, ...b.undo],
    matches: [...a.matches, ...b.matches.filter((id) => !a.matches.includes(id))],
    items: [...a.items, ...b.items.filter((it) => !a.items.some((had) => had.id === it.id))],
    latest: a.latest > b.latest ? a.latest : b.latest,
  }
}

// mergeDays adds a later page's days to the days loaded, joining a day the two split and,
// within it, a line the two split (ActivityGroup's key names it across pages).
export function mergeDays(
  loaded: readonly AutopilotActivityDay[],
  page: readonly AutopilotActivityDay[],
): AutopilotActivityDay[] {
  const out = loaded.map((day) => ({ ...day, matches: [...day.matches], groups: [...day.groups] }))

  for (const day of page) {
    const same = out.find((d) => d.date === day.date)

    if (same === undefined) {
      out.push(day)

      continue
    }

    same.matches.push(...day.matches.filter((m) => !same.matches.some((had) => had.id === m.id)))

    for (const group of day.groups) {
      const at = same.groups.findIndex((g) => g.key === group.key)

      if (at < 0) {
        same.groups.push(group)
      } else {
        const had = same.groups[at]

        if (had !== undefined) {
          same.groups[at] = mergeGroups(had, group)
        }
      }
    }
  }

  return out.sort((a, b) => (a.date < b.date ? 1 : a.date > b.date ? -1 : 0))
}

// matchCount counts the matches the pages hold.
export function matchCount(page: Pick<AutopilotActivityPage, 'days'>): number {
  return page.days.reduce((n, day) => n + day.matches.length, 0)
}

// itemText names an item as a line does: "example-repo #12".
export function itemText(item: AutopilotItemRef): string {
  const name = item.repo.slice(item.repo.lastIndexOf('/') + 1)

  return `${name} #${String(item.number)}`
}

// sameSecond reports two RFC 3339 times that fall in the same second: a target's time is
// written to the second, and a match's to the nanosecond.
export function sameSecond(a: string, b: string): boolean {
  const x = Date.parse(a)
  const y = Date.parse(b)

  return !Number.isNaN(x) && !Number.isNaN(y) && Math.floor(x / 1000) === Math.floor(y / 1000)
}

// maxIDs is how many items one read by IDs takes (LatestMatches, ItemsByID), as the
// backend caps them; the window reads more in as many calls.
export const maxIDs = 500

// inChunks splits ids into runs of at most maxIDs, in order.
export function inChunks(ids: readonly number[]): number[][] {
  const out: number[][] = []

  for (let at = 0; at < ids.length; at += maxIDs) {
    out.push(ids.slice(at, at + maxIDs))
  }

  return out
}

// checkText says how one readiness check came out: its label and what was found, as in
// "Claude · isn’t installed".
export function checkText(c: Pick<AutopilotReadyCheck, 'label' | 'found'>): string {
  return c.found === '' ? c.label : `${c.label} · ${c.found}`
}

// readyOf is a routine's readiness on this Mac, or undefined while it isn't known, as for a
// routine that is off, which nothing checks.
export function readyOf(readiness: AutopilotReadiness | null, routineId: number): AutopilotRoutineReady | undefined {
  return readiness?.routines.find((r) => r.routineId === routineId)
}

// notReadyText says what this Mac lacks for a routine, by its first problem: "Not ready on
// this Mac: Claude · isn’t installed", or '' for a routine that is ready or not known.
export function notReadyText(r: AutopilotRoutineReady | undefined): string {
  if (r === undefined || r.ready) {
    return ''
  }

  const [first] = r.problems

  return first === undefined ? 'Not ready on this Mac' : `Not ready on this Mac: ${checkText(first)}`
}

// notReadyNames are the routines this Mac can't run, in the list's order.
export function notReadyNames(readiness: AutopilotReadiness | null): string[] {
  return (readiness?.routines ?? []).filter((r) => !r.ready).map((r) => r.name)
}

// notReadyBanner says how many of the user's routines this Mac can't run.
export function notReadyBanner(count: number): string {
  return `This Mac can’t run ${formatCount(count)} of your routines.`
}

// NeedsYou is what waits in Activity's Needs you, by where it stands: the matches waiting
// for the user's OK, a first pass's backlogs (each routine's together, which Run on them
// answers at once), those that can't start until something on this Mac is fixed, and those
// that start by themselves once what they wait on frees, each newest first.
export interface NeedsYou {
  waiting: AutopilotMatch[]
  backlogs: Backlog[]
  blocked: AutopilotMatch[]
  queued: AutopilotMatch[]
}

// Backlog is one routine's matches waiting as a backlog over its first pass's cap.
export interface Backlog {
  routineId: number
  routine: string
  matches: AutopilotMatch[]
}

// needsYou sorts the open matches that haven't started into Needs you's parts, one
// routine's alone when routine isn't 0.
export function needsYou(matches: readonly AutopilotMatch[], routine = 0): NeedsYou {
  const mine = matches.filter((m) => routine === 0 || m.routineId === routine)
  const backlogs = new Map<number, Backlog>()

  for (const m of mine) {
    if (m.state !== 'waiting' || m.blockedBy !== 'backlog') {
      continue
    }

    const b = backlogs.get(m.routineId)

    if (b === undefined) {
      backlogs.set(m.routineId, { routineId: m.routineId, routine: m.routine, matches: [m] })
    } else {
      b.matches.push(m)
    }
  }

  return {
    waiting: mine.filter((m) => m.state === 'waiting' && m.blockedBy !== 'backlog'),
    backlogs: [...backlogs.values()],
    blocked: mine.filter((m) => m.state === 'blocked'),
    queued: mine.filter((m) => m.state === 'queued'),
  }
}

// backlogText says what a backlog holds and why it waits: "42 items matched at once, more
// than Rebase my bumps takes on by itself: Ask Dependabot to rebase, for each".
export function backlogText(b: Backlog): string {
  const [first] = b.matches
  const ahead = first === undefined ? '' : githubAheadText(first)
  const text = `${counted(b.matches.length, 'item', 'items')} matched at once, more than ${b.routine} takes on by itself`

  return ahead === '' ? `${text}.` : `${text}: ${ahead}, for each.`
}

// shortHead is a commit as the window names it: its first seven characters.
export function shortHead(sha: string): string {
  return sha.slice(0, 7)
}

// dirtyText says what holds a start in a worktree back: "pr-88 has 3 uncommitted files".
export function dirtyText(hold: Pick<AutopilotHold, 'worktree' | 'dir' | 'dirty'>): string {
  const where = hold.worktree === '' ? hold.dir : hold.worktree

  return `${where} has ${counted(hold.dirty, 'uncommitted file', 'uncommitted files')}`
}

// runLabel is what a waiting match's Run says: for a hold of a pull request's files, the
// commit it confirms them at ("Run on a1b2c3d"); for a worktree with uncommitted changes,
// that it runs there anyway ("Run in pr-88 anyway"); else Run.
export function runLabel(m: Pick<AutopilotMatch, 'blockedBy' | 'hold'>): string {
  const hold = m.hold

  if (hold === null) {
    return 'Run'
  }

  if (m.blockedBy === 'dirty_worktree') {
    return `Run in ${hold.worktree === '' ? hold.dir : hold.worktree} anyway`
  }

  return m.blockedBy === 'hold' && hold.head !== '' ? `Run on ${shortHead(hold.head)}` : 'Run'
}

// skipLabel is what Skip says: a pull request's occurrence is its commit, so skipping it
// waits for the next push; any other waits for the next time the routine fires.
export function skipLabel(m: Pick<AutopilotMatch, 'item' | 'fireKey'>): string {
  return m.item.kind === 'pr' && m.fireKey.includes(':commit:') ? 'Skip this commit' : 'Skip this time'
}

// AutopilotRequest is what the window asks the Autopilot tab to show, numbered so the same
// target asked again shows again: Activity, at a match when one is named, or a routine.
export interface AutopilotRequest {
  seq: number
  target: AutopilotTarget
}

// targetOf is the target that shows a match in Activity, as a notification's would.
export function targetOf(m: AutopilotMatch): AutopilotTarget {
  return { routineId: m.routineId, matchId: m.id, runId: 0, createdAt: m.createdAt, activity: false }
}
