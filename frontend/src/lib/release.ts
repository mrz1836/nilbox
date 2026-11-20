// Releases as the Release page and its sheet word them: the groups, the rows' order and
// readiness, how far a read of the list has got, a clone's state, the bump the commits
// suggest, the steps a release goes through, how one came out, and the recent ones, from
// the runs.

import type {
  CommitChecks,
  ReleaseCandidate,
  ReleaseCloneState,
  ReleaseFollowState,
  ReleaseGroup,
  ReleaseResult,
  ReleaseResultKind,
  ReleasesProgress,
  ReleaseSort,
  ScriptRun,
  ScriptRunProgress,
} from '../platform'
import { counted, formatCount } from './format'
import { isLive } from './scriptRuns'

// ReleaseRequest asks the Release page to open a repository's sheet, as a merge's toast
// does: the repository, its clone, and the branch the pull request merged into. seq
// numbers the requests, so asking for the same repository again opens it again.
export interface ReleaseRequest {
  repo: string
  dir: string
  branch: string
  seq: number
}

// releaseGroups are the page's groups in order: what each is called, and whether it
// starts open. Only the unreleased changes start open. Muted holds the repositories the
// user muted, themselves or their org, which GitHub isn't asked about.
export const releaseGroups: readonly { id: ReleaseGroup; title: string; open: boolean }[] = [
  { id: 'unreleased', title: 'Unreleased changes', open: true },
  { id: 'upToDate', title: 'Up to date', open: false },
  { id: 'noReleases', title: 'No releases yet', open: false },
  { id: 'cantRelease', title: 'Can’t release here', open: false },
  { id: 'muted', title: 'Muted', open: false },
]

// releasable reports a group whose rows offer Release…: one with something to release.
export function releasable(group: ReleaseGroup): boolean {
  return group === 'unreleased' || group === 'noReleases'
}

// matchCandidates keeps the candidates whose "owner/name" holds the search, trimmed and in
// any case, in their order.
export function matchCandidates(repos: readonly ReleaseCandidate[], search: string): ReleaseCandidate[] {
  const want = search.trim().toLowerCase()

  return repos.filter((r) => r.repo.toLowerCase().includes(want))
}

// unreleasedCount counts the repositories with changes since their newest version, which
// the rail shows beside Release.
export function unreleasedCount(repos: readonly ReleaseCandidate[] | undefined): number {
  return repos?.filter((r) => r.group === 'unreleased').length ?? 0
}

// compare orders two values as Go's cmp.Compare does: -1 when x comes first, 1 when y
// does, and 0 when they're equal.
function compare<T extends string | number>(x: T, y: T): number {
  if (x < y) {
    return -1
  }

  return x > y ? 1 : 0
}

// headTime reads when a candidate's newest commit came, in milliseconds, or -Infinity for
// one without a time, such as a muted repository GitHub wasn't asked about, so it sorts
// after every commit that has one.
function headTime(c: Pick<ReleaseCandidate, 'headAt'>): number {
  const at = Date.parse(c.headAt)

  return Number.isNaN(at) ? -Infinity : at
}

// SortKey is what the sort reads of a candidate, worked out once per row rather than on
// every comparison: whether it is pinned (0) or not (1), when its newest commit came, its
// name lowered, and its commits since the newest version.
interface SortKey {
  row: ReleaseCandidate
  rank: number
  time: number
  name: string
  ahead: number
}

// byName orders candidates by "owner/name", ignoring case, as the release service does
// (cmp.Compare of the lowered names, not the locale's collation).
function byName(a: SortKey, b: SortKey): number {
  return compare(a.name, b.name)
}

// byNewest orders candidates by their newest commit, the newest first and one without a
// time last, then by name.
function byNewest(a: SortKey, b: SortKey): number {
  return compare(b.time, a.time) || byName(a, b)
}

// byCommits orders candidates by their commits since the newest version, the most first,
// then as byNewest does. A count GitHub couldn't work out (+?, an ahead of 0) comes after
// every count it knows, as byNewest puts a row without a time last.
function byCommits(a: SortKey, b: SortKey): number {
  return compare(b.ahead, a.ahead) || byNewest(a, b)
}

// candidateOrder is how a sort orders two candidates, pinning aside.
function candidateOrder(sort: ReleaseSort): (a: SortKey, b: SortKey) => number {
  switch (sort) {
    case 'newest':
      return byNewest
    case 'name':
      return byName
    case 'commits':
      return byCommits
  }
}

// sortCandidates orders the candidates as the page lists them, in a new array: the
// pinned repositories first, then each part in the chosen sort. The sort is stable, so
// candidates it can't tell apart keep their order.
export function sortCandidates(
  rows: readonly ReleaseCandidate[],
  sort: ReleaseSort,
  isPinned: (repo: string) => boolean,
): ReleaseCandidate[] {
  const order = candidateOrder(sort)
  const keys = rows.map((row): SortKey => ({
    row,
    rank: isPinned(row.repo) ? 0 : 1,
    time: headTime(row),
    name: row.repo.toLowerCase(),
    ahead: row.ahead,
  }))

  return keys.sort((a, b) => compare(a.rank, b.rank) || order(a, b)).map((k) => k.row)
}

// waveAt is how many of the listed rows must share a headline before their rows show a ×N
// badge for it.
export const waveAt = 3

// sharedHeadlines maps each headline the rows have to the repositories whose newest
// commit it is, in the rows' order, each repository once however many of its clones are
// listed. A row without a headline is left out.
export function sharedHeadlines(rows: readonly Pick<ReleaseCandidate, 'repo' | 'headline'>[]): Map<string, string[]> {
  const shared = new Map<string, string[]>()

  for (const { repo, headline } of rows) {
    if (headline === '') {
      continue
    }

    const repos = shared.get(headline)

    if (repos === undefined) {
      shared.set(headline, [repo])
    } else if (!repos.includes(repo)) {
      repos.push(repo)
    }
  }

  return shared
}

// listProgressWords says how far a read of the list has got, phase by phase: finding the
// clones, asking GitHub about their repositories, then what changed since each version,
// and checking the unreleased clones, as "Asking GitHub: 45 of 151 repositories…".
export function listProgressWords({ phase, done, total }: ReleasesProgress): string {
  const repos = `${formatCount(done)} of ${counted(total, 'repository', 'repositories')}`

  switch (phase) {
    case 'clones':
      return 'Finding your clones…'
    case 'github':
      return `Asking GitHub: ${repos}…`
    case 'compare':
      return `Asking GitHub what changed: ${repos}…`
    case 'checking':
      return `Checking your clones: ${formatCount(done)} of ${formatCount(total)}…`
  }
}

// bumpResultKeys are the fields a line must have to be magex's json=true result, as the
// release service reads it (mxResultKeys in internal/release).
const bumpResultKeys = ['from', 'to', 'tag', 'pushed', 'branch', 'warnings', 'error'] as const

// isBumpResult reports whether a line of a release's log is magex's json=true result: a
// JSON object with every field the result has. The release's own result shows what it
// says, so its log view leaves the line out; the log file keeps it.
export function isBumpResult(line: string): boolean {
  const text = line.trim()

  if (!text.startsWith('{') || !text.endsWith('}')) {
    return false
  }

  try {
    const parsed: unknown = JSON.parse(text)

    return (
      typeof parsed === 'object' &&
      parsed !== null &&
      !Array.isArray(parsed) &&
      bumpResultKeys.every((k) => k in parsed)
    )
  } catch {
    return false
  }
}

// CloneNoteKind is what a note about a clone is about: a clone git couldn't read, a
// detached commit, a branch that isn't the default, uncommitted files, commits not on
// GitHub, commits it lacks, or a version only it has.
export type CloneNoteKind = 'unreadable' | 'detached' | 'branch' | 'dirty' | 'notPushed' | 'behind' | 'localOnly'

// CloneNote is one thing that matters about a clone before releasing from it: what it's
// about, the few words its row shows (a branch's name alone, which the row reads as "on"
// it), whether it stops a release until it's fixed, and the sentence its tooltip says.
export interface CloneNote {
  kind: CloneNoteKind
  text: string
  blocks: boolean
  title: string
}

// cloneNotes says what matters about a clone before releasing from it, in order: a
// detached commit, or the branch it's on when that isn't the default, then uncommitted
// files, commits not on GitHub, commits it lacks, and versions only it has. A clone git
// couldn't read gets that note alone, and no clone gets none. Only the branch and the
// commits it lacks let a release go ahead: magex switches branches and back, and pulls
// first.
export function cloneNotes(clone: ReleaseCloneState | null, defaultBranch: string): CloneNote[] {
  if (clone === null) {
    return []
  }

  if (clone.unreadable) {
    return [
      {
        kind: 'unreadable',
        text: 'couldn’t read the clone',
        blocks: true,
        title: 'git couldn’t read this clone, so Release can’t check it.',
      },
    ]
  }

  const def = defaultBranch === '' ? 'the default branch' : defaultBranch
  const notes: CloneNote[] = []

  if (clone.detached) {
    notes.push({
      kind: 'detached',
      text: 'on a detached commit',
      blocks: true,
      title: 'Your clone is on a detached commit. Switch to a branch first.',
    })
  } else if (clone.branch !== '' && clone.branch !== defaultBranch) {
    notes.push({
      kind: 'branch',
      text: clone.branch,
      blocks: false,
      title: `You’re on ${clone.branch}. magex switches to ${def}, then back.`,
    })
  }

  if (clone.dirty > 0) {
    notes.push({
      kind: 'dirty',
      text: `${String(clone.dirty)} uncommitted`,
      blocks: true,
      title: `${counted(clone.dirty, 'file has', 'files have')} uncommitted changes. Commit or stash them first.`,
    })
  }

  if (clone.notPushed > 0) {
    notes.push({
      kind: 'notPushed',
      text: `${String(clone.notPushed)} not pushed`,
      blocks: true,
      title: `${counted(clone.notPushed, 'commit isn’t', 'commits aren’t')} on GitHub yet. Push first.`,
    })
  }

  if (clone.behind) {
    notes.push({
      kind: 'behind',
      text: 'behind',
      blocks: false,
      title: `origin/${def} has commits your clone doesn’t. magex pulls them first.`,
    })
  }

  for (const tag of clone.localOnly) {
    notes.push({
      kind: 'localOnly',
      text: `${tag} only on this Mac`,
      blocks: true,
      title: `The tag ${tag} is in this clone but not on GitHub. Push it or delete it first.`,
    })
  }

  return notes
}

// shownNotes are the notes a row shows: only those that stop a release, in order, when
// any does; else the commits the clone lacks, then the branch it's on.
export function shownNotes(notes: readonly CloneNote[]): CloneNote[] {
  const blocking = notes.filter((n) => n.blocks)

  if (blocking.length > 0) {
    return blocking
  }

  return [...notes.filter((n) => n.kind === 'behind'), ...notes.filter((n) => n.kind === 'branch')]
}

// Readiness is how ready a repository is to release, at a glance: its newest commit's
// checks failing, a clone note that stops a release, checks still running, checks passed
// with nothing in the way, or no checks at all.
export type Readiness = 'failing' | 'blocked' | 'running' | 'ready' | 'none'

// readiness reads a repository's readiness from its checks and its clone, the first that
// applies: failing checks, a clone note that blocks, running checks, passed checks, and
// then none. A clone that is failing and blocked reads as failing, the louder of the two.
export function readiness(c: Pick<ReleaseCandidate, 'checks' | 'clone' | 'defaultBranch'>): Readiness {
  const { checks } = c

  if (checks === 'FAILURE' || checks === 'ERROR') {
    return 'failing'
  }

  if (cloneNotes(c.clone, c.defaultBranch).some((n) => n.blocks)) {
    return 'blocked'
  }

  switch (checks) {
    case 'PENDING':
    case 'EXPECTED':
      return 'running'
    case 'SUCCESS':
      return 'ready'
    case '':
      return 'none'
  }
}

// readinessOrder is the order readinesses come in, what needs a look first.
export const readinessOrder: readonly Readiness[] = ['failing', 'blocked', 'running', 'ready', 'none']

// readinessWords say each readiness in the words a group's summary counts it with.
export const readinessWords: Readonly<Record<Readiness, string>> = {
  failing: 'failing',
  blocked: 'blocked',
  running: 'running',
  ready: 'ready',
  none: 'without checks',
}

// readinessCounts counts the rows of each readiness, in readinessOrder, leaving out a
// readiness no row has.
export function readinessCounts(
  rows: readonly Pick<ReleaseCandidate, 'checks' | 'clone' | 'defaultBranch'>[],
): { readiness: Readiness; count: number }[] {
  const all = rows.map(readiness)

  return readinessOrder
    .map((r) => ({ readiness: r, count: all.filter((a) => a === r).length }))
    .filter((c) => c.count > 0)
}

// commitChecksText says how the checks on a commit went, as words beside their icon.
export function commitChecksText(checks: CommitChecks): string {
  switch (checks) {
    case 'SUCCESS':
      return 'checks passed'
    case 'PENDING':
    case 'EXPECTED':
      return 'checks running'
    case 'FAILURE':
    case 'ERROR':
      return 'checks failing'
    case '':
      return 'no checks'
  }
}

// breakingHeadline matches a conventional commit marked breaking, its type in the first
// group: feat!: and fix(api)!: alike, or a headline that starts BREAKING CHANGE:.
const breakingHeadline = /^(?:([a-z]+)(?:\([^)]*\))?!:|BREAKING[ -]CHANGE:)/i

// featHeadline matches a conventional commit that adds a feature: feat: or feat(scope):.
const featHeadline = /^feat(?:\([^)]*\))?:/i

// breakingMarks are the matches of the headlines marked breaking, in order.
function breakingMarks(headlines: readonly string[]): RegExpExecArray[] {
  return headlines.map((h) => breakingHeadline.exec(h.trim())).filter((m) => m !== null)
}

// featCount counts the headlines that start with feat:.
function featCount(headlines: readonly string[]): number {
  return headlines.filter((h) => featHeadline.test(h.trim())).length
}

// bumpKind reads, from the headlines of the commits a release would ship, the bump their
// conventional-commit prefixes usually mean, as bumpHint words it: major when any is
// marked breaking, else minor when any starts with feat:, else ''.
export function bumpKind(headlines: readonly string[]): 'major' | 'minor' | '' {
  if (breakingMarks(headlines).length > 0) {
    return 'major'
  }

  return featCount(headlines) > 0 ? 'minor' : ''
}

// bumpHint suggests, from the headlines of the commits a release would ship, the bump
// their conventional-commit prefixes usually mean: a major one when any is marked
// breaking, else a minor one when any starts with feat:. It only words the suggestion and
// never changes the choice, which starts at Patch every time (D11); '' when there is
// nothing to suggest.
export function bumpHint(headlines: readonly string[]): string {
  const breaking = breakingMarks(headlines)

  if (breaking.length > 0) {
    const kind = breaking[0]?.[1]?.toLowerCase()
    const marker = kind === undefined ? 'BREAKING CHANGE:' : `${kind}!:`

    return breaking.length === 1
      ? `1 commit is marked breaking (${marker}), which usually means a major release.`
      : `${counted(breaking.length, 'commit', 'commits')} are marked breaking (such as ${marker}), which usually means a major release.`
  }

  const feats = featCount(headlines)

  if (feats === 0) {
    return ''
  }

  return feats === 1
    ? '1 commit starts with feat:, which usually means a minor release.'
    : `${counted(feats, 'commit', 'commits')} start with feat:, which usually means a minor release.`
}

// ReleaseIntent is what a release set out to do, as its run recorded it at the start.
export interface ReleaseIntent {
  version: string
  branch: string
  original: string
}

// stringField reads a string field of a value the runner kept as plain JSON, or ''.
function stringField(value: unknown, key: string): string {
  if (typeof value !== 'object' || value === null || !(key in value)) {
    return ''
  }

  const field = (value as Record<string, unknown>)[key]

  return typeof field === 'string' ? field : ''
}

// intentOf reads what a release's run set out to do. A run that isn't a release, or kept
// none, reads as empty.
export function intentOf(run: Pick<ScriptRun, 'result'>): ReleaseIntent {
  const intent: unknown = run.result.intent

  return {
    version: stringField(intent, 'version'),
    branch: stringField(intent, 'branch'),
    original: stringField(intent, 'original'),
  }
}

// outcomeKind reads how a release's run came out, or '' while it has no result, as a run
// still going, or one that ended before magex did.
export function outcomeKind(run: Pick<ScriptRun, 'result'>): ReleaseResultKind | '' {
  const kind = stringField(run.result.outcome, 'kind')

  return kind === 'released' || kind === 'notPushed' || kind === 'failed' || kind === 'unverified' ? kind : ''
}

// outcomeWords say how a release came out, in the words Recent lists it with.
export const outcomeWords: Readonly<Record<ReleaseResultKind, string>> = {
  released: 'on GitHub',
  notPushed: 'not pushed',
  failed: 'failed',
  unverified: 'not checked on GitHub',
}

// followState reads where following a released run's workflow on GitHub is, or '' for a
// release not followed: one that isn't on GitHub, or from before the app followed them.
export function followState(run: Pick<ScriptRun, 'result'>): ReleaseFollowState | '' {
  const outcome: unknown = run.result.outcome
  const follow =
    typeof outcome === 'object' && outcome !== null && 'follow' in outcome
      ? (outcome as Record<string, unknown>).follow
      : null
  const state = stringField(follow, 'state')

  return followStates.includes(state as ReleaseFollowState) ? (state as ReleaseFollowState) : ''
}

// followStates are where following a release's workflow can be.
const followStates: readonly ReleaseFollowState[] = [
  'following',
  'published',
  'failed',
  'noWorkflow',
  'unpublished',
  'timedOut',
]

// followWords say where a release on GitHub is as its workflow is followed, in the words
// Recent lists it with. One no workflow ran for, or that stopped at its limits, is still
// only on GitHub: nothing is assumed.
export const followWords: Readonly<Record<ReleaseFollowState, string>> = {
  following: 'on GitHub, following its workflow',
  published: 'published',
  failed: 'release workflow failed',
  noWorkflow: 'on GitHub',
  unpublished: 'on GitHub, not published',
  timedOut: 'on GitHub',
}

// recentReleases are the newest releases, newest first, as the runs list them.
export function recentReleases(runs: readonly ScriptRun[] | null, limit = 10): ScriptRun[] {
  return (runs ?? []).filter((run) => run.kind === 'release').slice(0, limit)
}

// recentWords say where a release in Recent is: going, how it came out, as far as its
// workflow on GitHub has got once it is on GitHub, or, without a result, how its run
// ended.
export function recentWords(run: ScriptRun): string {
  if (isLive(run.status)) {
    return 'running'
  }

  const kind = outcomeKind(run)
  const follow = followState(run)

  if (kind === 'released' && follow !== '') {
    return followWords[follow]
  }

  if (kind !== '') {
    return outcomeWords[kind]
  }

  return run.status === 'succeeded' ? 'done' : run.status
}

// FollowLine is the line a release's result says how its workflow on GitHub goes with:
// its tone, its words, the page its View link opens, or '' for none, and what that link
// is called to assistive technology.
export interface FollowLine {
  tone: 'ok' | 'info' | 'warn' | 'bad'
  text: string
  url: string
  label: string
}

// followLine is the workflow line of a release on GitHub: following it, published, one of
// its workflows failed, no workflow ran, the workflows ended with no release published, or
// stopped at the follow's limits; null for a release not followed.
export function followLine(result: ReleaseResult, repo: string): FollowLine | null {
  const { follow } = result

  if (follow === null) {
    return null
  }

  const tag = result.tag === '' ? result.version : result.tag
  const what = `${repo} ${tag}`

  switch (follow.state) {
    case 'following':
      return { tone: 'info', text: `Following the release workflow for ${what}…`, url: '', label: '' }
    case 'published':
      return { tone: 'ok', text: `The release workflow published ${what}`, url: follow.url, label: 'View the release' }
    case 'failed':
      return {
        tone: 'bad',
        text: `The ${follow.workflow === '' ? 'release' : follow.workflow} workflow failed for ${what}`,
        url: follow.url,
        label: 'View the failed workflow',
      }
    case 'noWorkflow':
      return {
        tone: 'warn',
        text: `No workflow ran for the ${tag} tag, so there’s nothing to follow`,
        url: follow.url,
        label: 'View the tag’s workflow runs',
      }
    case 'unpublished':
      return {
        tone: 'warn',
        text: `The tag’s workflows finished, but GitHub hasn’t published a release for ${tag}`,
        url: follow.url,
        label: 'View the releases',
      }
    case 'timedOut':
      return {
        tone: 'warn',
        text: `Stopped following ${what} after ${counted(follow.minutes, 'minute', 'minutes')}`,
        url: follow.url,
        label: 'View the release workflow',
      }
  }
}

// StepState is how far a release's step is: done, under way, or still to come.
export type StepState = 'done' | 'working' | 'pending'

// ReleaseStep is one line of a release's progress.
export interface ReleaseStep {
  id: string
  state: StepState
  text: string
}

// stepOrder is the order a release's steps come in: magex's four, then the app's own
// check of GitHub once magex ends.
const stepOrder = ['onBranch', 'updated', 'tagCreated', 'tagPushed', 'verified'] as const

type StepID = (typeof stepOrder)[number]

// stepTexts word each step done and under way, for a release of version from branch,
// begun on original.
function stepTexts(id: StepID, intent: ReleaseIntent): { done: string; working: string } {
  const branch = intent.branch === '' ? 'the release branch' : intent.branch
  const version = intent.version === '' ? 'the tag' : intent.version

  switch (id) {
    case 'onBranch':
      return intent.original === intent.branch && intent.branch !== ''
        ? { done: `On ${branch}`, working: `Checking out ${branch}` }
        : { done: `Switched to ${branch}`, working: `Switching to ${branch}` }
    case 'updated':
      return { done: `Pulled ${branch} from origin`, working: `Pulling ${branch} from origin` }
    case 'tagCreated':
      return { done: `Created tag ${version}`, working: `Creating tag ${version}` }
    case 'tagPushed':
      return { done: `Pushed ${version} to origin`, working: 'Pushing to origin' }
    case 'verified':
      return { done: 'GitHub has it', working: 'Checking that GitHub has it' }
  }
}

// releaseSteps lists a release's steps as its progress reads them: each one magex reached
// is done, and the one it works on, or else the next, is under way while the run goes. A
// line magex printed that wasn't recognized moves nothing, so the progress may lag but
// is never ahead of the run.
export function releaseSteps(progress: ScriptRunProgress, intent: ReleaseIntent, live: boolean): ReleaseStep[] {
  const reached = stepOrder.indexOf(progress.step as StepID) + 1
  const working = stepOrder.indexOf(progress.working as StepID)
  const current = live ? Math.max(reached, working) : -1

  return stepOrder.map((id, i) => {
    const texts = stepTexts(id, intent)

    if (i < reached) {
      return { id, state: 'done', text: texts.done }
    }

    return i === current ? { id, state: 'working', text: texts.working } : { id, state: 'pending', text: texts.working }
  })
}

// resultHeading heads a release's result: what happened, in words, with the repository
// and the version.
export function resultHeading(result: ReleaseResult, repo: string): string {
  const tag = result.tag === '' ? result.version : result.tag

  switch (result.kind) {
    case 'released':
      return result.follow?.state === 'published' ? `${repo} ${tag} is published` : `${repo} ${tag} is on GitHub`
    case 'notPushed':
      return `${tag} is tagged on this Mac but isn’t on GitHub`
    case 'failed':
      return result.failure?.heading ?? `The release of ${tag} failed`
    case 'unverified':
      return `Couldn’t check whether ${tag} is on GitHub`
  }
}
