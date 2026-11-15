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
// asking first, as the question says; restarting, once Restart is called; failed, with the
// backend's words for why, final when trying again would fail the same way (an update that
// failed to install for good); or waiting, refused for a moment while the last update may
// still be finishing, or a restart whose quit was held sees the app out, which is no
// failure.
export type RestartStep =
  | { step: 'idle' }
  | { step: 'checking' }
  | { step: 'preparing' }
  | { step: 'downloading' }
  | { step: 'asking'; question: RestartQuestion }
  | { step: 'restarting' }
  | { step: 'failed'; message: string; final?: true }
  | { step: 'waiting'; message: string }

// RestartQuestion is what restarting would lose, which the window asks about first: the
// runs going in the app it would stop (null when Go says runs go but couldn't list them,
// and empty when none were named), and how many sheets hold text the user hasn't sent.
export interface RestartQuestion {
  runs: readonly RunGoing[] | null
  sheets: number
}

// namesRuns reports whether a question named the runs restarting would stop, by name or as
// a whole, which its words then say it stops.
export function namesRuns(question: RestartQuestion): boolean {
  return question.runs === null || question.runs.length > 0
}

// covers reports whether a question the user confirmed named every run Go names as going
// now (going), so a restart that stops them stops only runs the user was shown: each by
// its id, or, when Go can't list them (going is empty), the runs as a whole. A run that
// started while it asked, or the runs going when it named none, asks again.
export function covers(question: RestartQuestion, going: readonly RunGoing[]): boolean {
  const { runs } = question

  if (going.length === 0) {
    return runs === null
  }

  return runs !== null && going.every((run) => runs.some((shown) => shown.id === run.id))
}

// settlingWords is what the window says while the last update may still be finishing:
// its own words, since that can take a while.
export const settlingWords = 'The last update is still finishing. This can take a minute or two.'

// finishingText and finishingDetail are what the window says while a restart whose quit
// was held sees the app out (finishing): the app quits on its own, and the update's helper
// installs the update and opens it.
export const finishingText = 'Finishing the update…'
export const finishingDetail = `${brand.name} will reopen in a moment.`

// finishingWords say both, as one line.
export const finishingWords = `${finishingText} ${finishingDetail}`

// Banner is what the update banner shows, if anything:
//   - ready: the update is downloaded, waiting for Restart to update;
//   - available: a newer version is out, which Update downloads and restarts into;
//   - working: a restart under way, or the download or check it starts with, and how far
//     a download has got (progress, 0 to 100), or null for work with no measure;
//   - failed: the restart the user asked for just failed, with why;
//   - waiting: the last update is still finishing, so the restart waits a moment, quietly;
//   - lastLaunch: the restart before this launch didn't install its update, with the newer
//     version on offer in its place, ready or available, which its button installs, or
//     null when the offer is the version that failed (or none);
//   - location: an update waits, but the app can't replace itself where it runs.
export type Banner =
  | { kind: 'none' }
  | { kind: 'ready'; version: string }
  | { kind: 'available'; version: string }
  | { kind: 'working'; text: string; progress: number | null; detail: string }
  | { kind: 'failed'; message: string }
  | { kind: 'waiting'; message: string }
  | { kind: 'lastLaunch'; version: string; current: string; newer: NewerVersion | null }
  | { kind: 'location'; version: string; ready: boolean }

// NewerVersion is a version on offer other than the one that failed to install: ready to
// restart into, or available to download first.
export interface NewerVersion {
  version: string
  ready: boolean
}

// newerThanFailed is the version on offer in place of the one that failed to install
// (failed), or null when the offer is that version, or nothing is on offer.
function newerThanFailed(status: UpdateStatus, failed: string): NewerVersion | null {
  const pending = status.state === 'ready' || status.state === 'available'

  return pending && status.latest !== '' && status.latest !== failed
    ? { version: status.latest, ready: status.state === 'ready' }
    : null
}

// versionName is a version as the window names it, or a stand-in when none is known.
function versionName(version: string): string {
  return version === '' ? 'the new version' : version
}

// downloadingWords says a version is downloading: "Downloading 0.3.0…".
export function downloadingWords(version: string): string {
  return `Downloading ${versionName(version)}…`
}

// restartingText says the app is restarting into a version.
export function restartingText(version: string): string {
  return `Restarting into ${versionName(version)}…`
}

// backingUp is what a restart under way (installing) says it does, true from its start to
// the quit: it backs the data up, then stages the update beside the app.
export const backingUp = 'Backing up your data and preparing the update…'

// finishing is what a restart whose quit was held shows while the app quits on its own.
const finishing: Banner = { kind: 'working', text: finishingText, progress: null, detail: finishingDetail }

// working is what a restart under way shows, from the live status, so the version named
// is the one being installed, which may be newer than the one first shown: a download's
// progress, determinate, or the restart itself, with no measure.
function working(status: UpdateStatus | null, step: RestartStep['step']): Banner {
  const version = status?.latest ?? ''

  if (status?.state === 'finishing') {
    return finishing
  }

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
//   - a restart whose quit was held, as the app quits on its own to finish it, wins over
//     everything, Later or not;
//   - the restart's failure, its wait while the last update finishes, then the restart
//     under way, win over the rest;
//   - nothing shows for a build that doesn't update, or before the status loads;
//   - a restart under way that the window didn't start shows as working too;
//   - the restart before this launch failing shows until put away, over the same version
//     ready again (Try again restarts into it), which is never offered as plainly ready,
//     and names a newer version found since, which its button installs;
//   - an update ready, or available, shows unless hidden with Later, as the location
//     notice when the app can't update where it runs, where nothing downloads;
//   - with Download it and tell me, an update found and not yet downloaded is about to
//     download quietly, so nothing shows until it is ready, even when a download failed:
//     the next look tries again, and About and the menu bar offer Update meanwhile.
// Background checks and downloads, and their failures, never show: About tells them.
export function bannerOf(status: UpdateStatus | null, flow: RestartStep, lastLaunchDismissed: string): Banner {
  if (status?.state === 'finishing') {
    return finishing
  }

  if (flow.step === 'failed' || flow.step === 'waiting') {
    return { kind: flow.step, message: sentence(flow.message) }
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
    return {
      kind: 'lastLaunch',
      version: failed.version,
      current: status.current,
      newer: newerThanFailed(status, failed.version),
    }
  }

  // The version that just failed to install stays downloaded, ready to try again, but is
  // never offered as plainly ready: put away, its failure shows nothing.
  if (failed.version !== '' && failed.version === status.latest) {
    return { kind: 'none' }
  }

  const ready = status.state === 'ready'
  // Where the app can't replace itself, nothing downloads on its own, so a version found
  // stays available, and is told as soon as it is found.
  const quiet = status.mode === 'download' && status.location.ok
  const available = status.state === 'available' && !quiet

  if (status.latest === '' || status.bannerHidden || (!ready && !available)) {
    return { kind: 'none' }
  }

  if (!status.location.ok) {
    return { kind: 'location', version: status.latest, ready }
  }

  return ready ? { kind: 'ready', version: status.latest } : { kind: 'available', version: status.latest }
}

// OfferAction is what the window offers to do about the update: restart into the version
// ready, update to the version found (download it, then restart), or try again after a
// restart, or an install, that failed. Each runs the one restart (restartToUpdate).
export type OfferAction = 'restart' | 'update' | 'retry'

// Offer is what the window offers, the same in the banner and in Settings → About, so the
// two never disagree: the action, with its words, or none; the release page to download
// and install by hand from (Download manually), or '' when that isn't offered; and whether
// the app must first move to Applications.
export interface Offer {
  action: OfferAction | null
  label: string
  page: string
  move: boolean
}

// offerLabels are each action's words.
const offerLabels: Readonly<Record<OfferAction, string>> = {
  restart: 'Restart to update',
  update: 'Update',
  retry: 'Try again',
}

// offering is an offer of action, with change made.
function offering(action: OfferAction | null, change: Partial<Offer> = {}): Offer {
  return { action, label: action === null ? '' : offerLabels[action], page: '', move: false, ...change }
}

// pageOf is the release page of the version a restart is about: the newer version found,
// whose page the status names, or, with none found, the version that failed to install;
// '' when neither is known, since the status then names the running version's page, which
// holds nothing to install.
function pageOf(status: UpdateStatus): string {
  const failed = status.installFailed.version

  if (status.latest !== '') {
    return status.releaseUrl
  }

  return failed === '' ? '' : releasePageOf(status.releaseUrl, failed)
}

// offerOf works out what the window offers, from the update's status and the restart
// (what the user put away changes only whether the banner shows, never what is offered):
//   - after a restart that just failed, Try again, or nothing when trying again would fail
//     the same way, and Download manually for the version it was about, once known;
//   - nothing for a build that doesn't update, or before the status loads;
//   - where the app can't replace itself, that it must move first, and no restart;
//   - after an install that failed at the last launch, for the version that failed (or
//     none found since), Try again and Download manually of that version, or Download
//     manually alone when it can't be tried again: never a plain Restart to update for it;
//   - otherwise Restart to update for a version ready, Update for one found, and nothing
//     while one downloads, installs, finishes installing, or none is newer.
export function offerOf(status: UpdateStatus | null, flow: RestartStep): Offer {
  if (flow.step === 'failed') {
    return offering(flow.final === true ? null : 'retry', { page: status === null ? '' : pageOf(status) })
  }

  if (status === null || !status.enabled) {
    return offering(null)
  }

  const pending = status.state === 'ready' || status.state === 'available'
  const failed = status.installFailed
  const failedOnOffer = failed.version !== '' && (failed.version === status.latest || status.latest === '')

  if (!status.location.ok && (pending || failedOnOffer)) {
    return offering(null, { move: true })
  }

  if (failedOnOffer) {
    return offering(failed.retryable ? 'retry' : null, { page: releasePageOf(status.releaseUrl, failed.version) })
  }

  if (!pending) {
    return offering(null)
  }

  return offering(status.state === 'ready' ? 'restart' : 'update')
}

// sentence ends words with a full stop, unless they end with one already.
export function sentence(words: string): string {
  const trimmed = words.trim()

  return trimmed === '' || /[.!?…]$/.test(trimmed) ? trimmed : `${trimmed}.`
}

// AboutLine is the Updates row's status in Settings → About: the words; how far a download
// has got (0 to 100), or null; and, for a check that reached the releases with nothing new,
// when it ended, which the row shows as a time ago; note is a failure to add under it, or
// ''. The row reads its words and note out as they change, never the climbing percentage
// or the time.
export interface AboutLine {
  text: string
  progress: number | null
  checkedAt: string
  note: string
}

// aboutLine words where the update stands for Settings → About.
export function aboutLine(status: UpdateStatus): AboutLine {
  const line = (text: string, note = ''): AboutLine => ({ text, progress: null, checkedAt: '', note })
  const note = sentence(status.lastError)

  if (!status.enabled) {
    return line(status.off)
  }

  switch (status.state) {
    case 'installing':
      return line(restartingText(status.latest))
    case 'finishing':
      return line(finishingText)
    case 'downloading':
      return { ...line(downloadingWords(status.latest)), progress: status.progress }
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

  return status.lastChecked === '' ? line('Not checked yet') : { ...line('Up to date'), checkedAt: status.lastChecked }
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
export function questionWords(question: RestartQuestion): QuestionWords {
  const { runs, sheets } = question
  const stops = namesRuns(question)
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
    notes.push(one ? `${sentence(heldRun.stopRefused)} ${heldRunText(true, 'Restarting')}` : heldRunText(false))
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
// that URL as it is when it isn't one: the page the toast's What's new opens for the
// version an update installed, whichever version the status's releaseUrl names by then.
export function releasePageOf(releaseUrl: string, version: string): string {
  return releaseTag.test(releaseUrl) ? releaseUrl.replace(releaseTag, `/releases/tag/v${version}`) : releaseUrl
}
