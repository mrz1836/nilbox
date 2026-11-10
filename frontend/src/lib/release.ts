// Releases as the Release page and its sheet word them: the groups, a clone's state, the
// steps a release goes through, how one came out, and the recent ones, from the runs.

import type {
  CommitChecks,
  ReleaseCandidate,
  ReleaseCloneState,
  ReleaseGroup,
  ReleaseResult,
  ReleaseResultKind,
  ScriptRun,
  ScriptRunProgress,
} from '../platform'
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

// recentReleases are the newest releases, newest first, as the runs list them.
export function recentReleases(runs: readonly ScriptRun[] | null, limit = 10): ScriptRun[] {
  return (runs ?? []).filter((run) => run.kind === 'release').slice(0, limit)
}

// recentWords say where a release in Recent is: going, how it came out, or, without a
// result, how its run ended.
export function recentWords(run: ScriptRun): string {
  if (isLive(run.status)) {
    return 'running'
  }

  const kind = outcomeKind(run)

  if (kind !== '') {
    return outcomeWords[kind]
  }

  return run.status === 'succeeded' ? 'done' : run.status
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
      return `${repo} ${tag} is on GitHub`
    case 'notPushed':
      return `${tag} is tagged on this Mac but isn’t on GitHub`
    case 'failed':
      return result.failure?.heading ?? `The release of ${tag} failed`
    case 'unverified':
      return `Couldn’t check whether ${tag} is on GitHub`
  }
}
