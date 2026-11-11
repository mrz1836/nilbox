// Releases as the Release page and its sheet word them: the groups, how far a read of the
// list has got, a clone's state, the steps a release goes through, how one came out, and
// the recent ones, from the runs.

import type {
  CommitChecks,
  ReleaseCandidate,
  ReleaseCloneState,
  ReleaseFollowState,
  ReleaseGroup,
  ReleaseResult,
  ReleaseResultKind,
  ReleasesProgress,
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
// starts open. Only the unreleased changes start open.
export const releaseGroups: readonly { id: ReleaseGroup; title: string; open: boolean }[] = [
  { id: 'unreleased', title: 'Unreleased changes', open: true },
  { id: 'upToDate', title: 'Up to date', open: false },
  { id: 'noReleases', title: 'No releases yet', open: false },
  { id: 'cantRelease', title: 'Can’t release here', open: false },
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

// cloneNotes says what matters about a clone before releasing from it, in a few words
// each: the branch it's on when that isn't the default, a detached commit, uncommitted
// files, commits not on GitHub, commits it lacks, and versions only it has.
export function cloneNotes(clone: ReleaseCloneState | null, defaultBranch: string): string[] {
  if (clone === null) {
    return []
  }

  if (clone.unreadable) {
    return ['couldn’t read the clone']
  }

  const notes: string[] = []

  if (clone.detached) {
    notes.push('on a detached commit')
  } else if (clone.branch !== '' && clone.branch !== defaultBranch) {
    notes.push(`on ${clone.branch}`)
  }

  if (clone.dirty > 0) {
    notes.push(`${String(clone.dirty)} uncommitted`)
  }

  if (clone.notPushed > 0) {
    notes.push(`${String(clone.notPushed)} not pushed`)
  }

  if (clone.behind) {
    notes.push('behind origin')
  }

  for (const tag of clone.localOnly) {
    notes.push(`${tag} only on this Mac`)
  }

  return notes
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
