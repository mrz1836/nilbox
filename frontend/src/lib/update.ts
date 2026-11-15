// The app's own updates as the window words them: what the update banner shows, the
// Updates row in Settings → About, the question restarting asks first, and the release
// pages. Everything here is pure; lib/useUpdate.ts holds the status and runs the restart.

import { brand } from '../generated/brand'
import type { RunGoing, ScriptRun, UpdateLocation, UpdateStatus } from '../platform'
import type { IssueDraft } from './issueDrafts'
import type { ReviewDraft } from './reviewDrafts'
import { heldReleaseText, heldRunText, releaseTagRisk, tagRisk } from './runsGoing'

// RestartStep is where the window's restart to update stands: nothing under way; finding
// the newer version first (a Try again with nothing found yet); getting it ready to ask,
// with what it shows unchanged; downloading it first, as Just tell me's Update does;
// asking first, as the question says; restarting, once Restart is called; or failed, with
// the backend's words for why.
export type RestartStep =
  | { step: 'idle' }
  | { step: 'checking' }
  | { step: 'preparing' }
  | { step: 'downloading' }
  | { step: 'asking'; question: RestartQuestion }
  | { step: 'restarting' }
  | { step: 'failed'; message: string }

// RestartQuestion is what restarting would lose, which the window asks about first: the
// runs going in the app it would stop (null when runs go but couldn't be listed), and how
// many sheets hold text the user hasn't sent.
export interface RestartQuestion {
  runs: readonly RunGoing[] | null
  sheets: number
}

// Banner is what the update banner shows, if anything:
//   - ready: the update is downloaded, waiting for Restart to update;
//   - available: a newer version is out, which Update downloads and restarts into;
//   - working: a restart under way, or the download or check it starts with, and how far
//     a download has got (progress, 0 to 100), or null for work with no measure;
//   - failed: the restart the user asked for just failed, with why;
//   - lastLaunch: the restart before this launch didn't install its update;
//   - location: an update waits, but the app can't replace itself where it runs.
export type Banner =
  | { kind: 'none' }
  | { kind: 'ready'; version: string }
  | { kind: 'available'; version: string }
  | { kind: 'working'; text: string; progress: number | null; detail: string }
  | { kind: 'failed'; message: string }
  | { kind: 'lastLaunch'; version: string; current: string }
  | { kind: 'location'; version: string; ready: boolean }

// versionName is a version as the window names it, or a stand-in when none is known.
function versionName(version: string): string {
  return version === '' ? 'the new version' : version
}

// downloadingWords says a version is downloading: "Downloading 0.3.0…".
export function downloadingWords(version: string): string {
  return `Downloading ${versionName(version)}…`
}

// downloadingText says how far a download has got: "Downloading 0.3.0… 42%".
export function downloadingText(version: string, progress: number): string {
  return `${downloadingWords(version)} ${String(progress)}%`
}

// restartingText says the app is restarting into a version.
export function restartingText(version: string): string {
  return `Restarting into ${versionName(version)}…`
}

// backingUp is what a restart says while it backs up the data: Status tells a restart
// under way (installing), which backs the data up first, from one waiting its turn.
export const backingUp = 'Backing up your data first.'

// working is what a restart under way shows, from the live status, so the version named
// is the one being installed, which may be newer than the one first shown: a download's
// progress, determinate, or the restart itself, with no measure.
function working(status: UpdateStatus | null, step: RestartStep['step']): Banner {
  const version = status?.latest ?? ''

  if (step === 'checking') {
    return { kind: 'working', text: 'Checking for updates…', progress: null, detail: '' }
  }

  if (status?.state === 'downloading' || (step === 'downloading' && status?.state === 'available')) {
    return { kind: 'working', text: downloadingWords(version), progress: status.progress, detail: '' }
  }

  return {
    kind: 'working',
    text: restartingText(version),
    progress: null,
    detail: status?.state === 'installing' ? backingUp : '',
  }
}

// bannerOf works out what the update banner shows, from the update's status, the
// window's restart, and the failure the user put away this session (lastLaunchDismissed,
// a version, or ''):
//   - the restart's failure, then the restart under way, win over everything;
//   - nothing shows for a build that doesn't update, or before the status loads;
//   - a restart under way that the window didn't start shows as working too;
//   - the restart before this launch failing shows until put away;
//   - an update ready, or available, shows unless hidden with Later, as the location
//     notice when the app can't update where it runs;
//   - with Download it and tell me, an update found and not yet downloaded is about to
//     download quietly, so nothing shows until it is ready, unless its download failed.
// Background checks and downloads never show: About tells them.
export function bannerOf(status: UpdateStatus | null, flow: RestartStep, lastLaunchDismissed: string): Banner {
  if (flow.step === 'failed') {
    return { kind: 'failed', message: sentence(flow.message) }
  }

  if (flow.step === 'checking' || flow.step === 'downloading' || flow.step === 'restarting') {
    return working(status, flow.step)
  }

  if (status === null || !status.enabled) {
    return { kind: 'none' }
  }

  if (status.state === 'installing') {
    return working(status, 'restarting')
  }

  const failed = status.installFailed

  if (failed.version !== '' && failed.version !== lastLaunchDismissed) {
    return { kind: 'lastLaunch', version: failed.version, current: status.current }
  }

  const ready = status.state === 'ready'
  const available = status.state === 'available' && (status.mode !== 'download' || status.lastError !== '')

  if (status.latest === '' || status.bannerHidden || (!ready && !available)) {
    return { kind: 'none' }
  }

  if (!status.location.ok) {
    return { kind: 'location', version: status.latest, ready }
  }

  return ready ? { kind: 'ready', version: status.latest } : { kind: 'available', version: status.latest }
}

// sentence ends words with a full stop, unless they end with one already.
export function sentence(words: string): string {
  const trimmed = words.trim()

  return trimmed === '' || /[.!?…]$/.test(trimmed) ? trimmed : `${trimmed}.`
}

// AboutLine is the Updates row's status in Settings → About: the words, and, for a check
// that reached the releases with nothing new, when it ended, which the row shows as a
// time ago; note is a failure to add under it, or ''.
export interface AboutLine {
  text: string
  checkedAt: string
  note: string
}

// aboutLine words where the update stands for Settings → About.
export function aboutLine(status: UpdateStatus): AboutLine {
  const line = (text: string, note = ''): AboutLine => ({ text, checkedAt: '', note })
  const note = sentence(status.lastError)

  if (!status.enabled) {
    return line(status.off)
  }

  switch (status.state) {
    case 'installing':
      return line(restartingText(status.latest))
    case 'downloading':
      return line(downloadingText(status.latest, status.progress))
    case 'ready':
      return line(`${status.latest} is ready`, note)
    case 'available':
      return line(`${status.latest} is available`, note)
    default:
      break
  }

  if (status.state === 'checking' || status.checking) {
    return line('Checking…')
  }

  if (status.lastError !== '') {
    return line(`Last check failed: ${note}`)
  }

  return status.lastChecked === ''
    ? line('Not checked yet')
    : { text: 'Up to date', checkedAt: status.lastChecked, note: '' }
}

// LocationWords say why the app can't update where it runs, and what to do.
export interface LocationWords {
  title: string
  hint: string
}

// locationWords words a location the app can't update in: an Applications folder its
// account can't change, or anywhere else, from where it moves into the Applications folder
// it would go in (the target's folder).
export function locationWords(location: UpdateLocation): LocationWords {
  if (location.kind === 'applications' || location.kind === 'userApplications') {
    return {
      title: `${brand.name} can’t update itself here`,
      hint: 'Your account can’t change the folder it’s in, which an update needs.',
    }
  }

  const slash = location.target.lastIndexOf('/')
  const folder = slash > 0 ? location.target.slice(0, slash) : 'your Applications folder'

  return {
    title: `Move ${brand.name} to Applications to update it`,
    hint: `${brand.name} can only update itself from your Applications folder. Move it to ${folder}, then open it from there.`,
  }
}

// locationNote is the banner's short hint for an update the app can't install where it
// runs.
export function locationNote(location: UpdateLocation): string {
  return location.kind === 'applications' || location.kind === 'userApplications'
    ? 'Your account can’t change the folder it’s in, so it can’t install it.'
    : `Move ${brand.name} to your Applications folder to install it.`
}

// sheetsText names the sheets that hold text the user hasn't sent.
function sheetsText(sheets: number): string {
  return sheets === 1 ? 'the text you haven’t sent in a sheet' : `the text you haven’t sent in ${String(sheets)} sheets`
}

// runText names one run going, and where it runs.
export function runText(run: RunGoing): string {
  const where = run.repo === '' ? run.dir : run.repo

  return where === '' ? run.title : `${run.title} in ${where}`
}

// runsText names the runs going: one by its name, several by their count, which the
// question lists, and runs that couldn't be listed as a whole.
function runsText(runs: readonly RunGoing[] | null): string {
  if (runs === null || runs.length === 0) {
    return `the runs going in ${brand.name}`
  }

  const [only] = runs

  return runs.length === 1 && only !== undefined ? runText(only) : `${String(runs.length)} runs`
}

// QuestionWords are the ask-first dialog's words: what restarting would lose, the runs it
// lists when there are several, and what stopping them can leave.
export interface QuestionWords {
  text: string
  list: readonly RunGoing[]
  notes: readonly string[]
}

// questionWords says what restarting to update would lose, in the quit dialog's words for
// the runs: "Restarting stops Release v1.2.0 in acme-org/sdk and loses the text you
// haven’t sent in 2 sheets."
export function questionWords({ runs, sheets }: RestartQuestion): QuestionWords {
  const stops = runs === null || runs.length > 0
  const going = runs ?? []
  const parts = [stops && `stops ${runsText(runs)}`, sheets > 0 && `loses ${sheetsText(sheets)}`].filter(
    (part) => part !== false,
  )
  const text = parts.length === 0 ? '' : `Restarting ${parts.join(' and ')}.`
  const one = going.length === 1
  const releases = going.filter((run) => run.kind === 'release')
  const held = going.filter((run) => run.stopRefused !== '')
  const notes: string[] = []

  if (held.some((run) => run.kind === 'release')) {
    notes.push(heldReleaseText(one))
  } else if (releases.length > 0) {
    notes.push(one ? tagRisk : releaseTagRisk)
  }

  const [heldRun] = held.filter((run) => run.kind !== 'release')

  if (heldRun !== undefined) {
    notes.push(one ? `${sentence(heldRun.stopRefused)} ${heldRunText(true)}` : heldRunText(false))
  }

  return { text, list: going.length > 1 ? going : [], notes }
}

// runsGoingIn lists the runs going in the app, as restarting would stop them, from the
// runs listed: the quit dialog's runs, a run held back from stopping for now included.
export function runsGoingIn(listed: readonly ScriptRun[]): RunGoing[] {
  return listed
    .filter((run) => run.canStop || run.progress.stopRefused !== '')
    .map((run) => ({
      id: run.id,
      kind: run.kind,
      title: run.title,
      repo: run.repo,
      dir: run.dir,
      stopRefused: run.progress.stopRefused,
    }))
}

// unsentSheets counts the sheets holding text the user hasn't sent, which a restart would
// lose: a review with text the user wrote or picked, or line comments, and an issue
// comment with any text.
export function unsentSheets(reviews: readonly ReviewDraft[], issues: readonly IssueDraft[]): number {
  const review = reviews.filter((d) => (d.edited && d.body.trim() !== '') || d.comments.length > 0).length

  return review + issues.filter((d) => d.body.trim() !== '').length
}

// releaseTag matches the end of a release page's URL, which Go builds as
// <repository>/releases/tag/v<version>.
const releaseTag = /\/releases\/tag\/v[^/]*$/

// releasePageOf is the release page of version, from the URL of any release's page, or
// that URL as it is when it isn't one.
export function releasePageOf(releaseUrl: string, version: string): string {
  return releaseTag.test(releaseUrl) ? releaseUrl.replace(releaseTag, `/releases/tag/v${version}`) : releaseUrl
}

// latestReleasePage is the page of the newest release, where the app downloads by hand,
// from the URL of any release's page, or that URL as it is when it isn't one.
export function latestReleasePage(releaseUrl: string): string {
  return releaseTag.test(releaseUrl) ? releaseUrl.replace(releaseTag, '/releases/latest') : releaseUrl
}
