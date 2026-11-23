// The user's GitHub notifications as the window shows them: the toolbar button's badge and
// counts, the newest unread grouped by the item each is about, and the words for GitHub's
// reasons. The backend mirrors them (internal/ghthreads) and gives
// no URL GitHub returned: every link here is built from an item's repository, kind, and
// number, with fixed strings.

import { brand } from '../generated/brand'
import type {
  GitHubBulkAge,
  GitHubBulkDone,
  GitHubThread,
  GitHubThreadState,
  GitHubThreadsStatus,
  ItemKind,
  QueuedClear,
  SettledClear,
  ThreadTrigger,
} from '../platform'
import { counted, formatCount } from './format'
import { itemKey } from './itemWorktrees'
import { formatAgo, formatTimeOfDay, formatUntil } from './time'

// inboxURL is the user's GitHub inbox on the web, and doneURL its Done view, which lists
// what was marked done: every clear that marked one.
export const inboxURL = 'https://github.com/notifications'
export const doneURL = 'https://github.com/notifications?query=is%3Adone'

// refusedText is what the window says while GitHub refuses the token the notifications,
// when the status carries no words of its own.
const refusedText = 'Your GitHub token can’t read notifications.'

// ThreadItem is the pull request or issue a notification is about.
export interface ThreadItem {
  owner: string
  name: string
  kind: ItemKind
  number: number
}

// segment is a repository owner's or name's shape on GitHub: letters, digits, and . - _,
// never "." or "..".
const segment = /^[A-Za-z0-9._-]+$/

// validSegment reports whether s can stand in a GitHub URL's path as an owner or a name.
function validSegment(s: string): boolean {
  return segment.test(s) && s !== '.' && s !== '..'
}

// threadItem is the item a thread is about, or null when it is
// about none (a release, a check suite, ...), or names it in a way no link could hold.
export function threadItem(ref: {
  owner: string
  name: string
  kind: ItemKind | ''
  number: number
}): ThreadItem | null {
  const { owner, name, kind, number } = ref

  if (kind === '' || !Number.isSafeInteger(number) || number <= 0) {
    return null
  }

  if (!validSegment(owner) || !validSegment(name)) {
    return null
  }

  return { owner, name, kind, number }
}

// itemURL is an item's page on GitHub, built from its parts alone.
export function itemURL(item: ThreadItem): string {
  const path = item.kind === 'pr' ? 'pull' : 'issues'

  return `https://github.com/${encodeURIComponent(item.owner)}/${encodeURIComponent(item.name)}/${path}/${String(item.number)}`
}

// threadItemKey is an item's key among the inbox's (itemKey).
function threadItemKey(item: { owner: string; name: string; number: number }): string {
  return itemKey(`${item.owner}/${item.name}`, item.number)
}

// reasonWords are GitHub's reasons for a notification, worded. A reason this build does
// not know shows as GitHub wrote it.
const reasonWords: Readonly<Record<string, string>> = {
  review_requested: 'Review requested',
  assign: 'Assigned',
  mention: 'Mentioned',
  team_mention: 'Team mentioned',
  author: 'You opened it',
  comment: 'You commented',
  subscribed: 'Watching',
  state_change: 'You changed its state',
  ci_activity: 'Workflow run',
  manual: 'Subscribed',
  security_alert: 'Security alert',
  invitation: 'Invitation',
  approval_requested: 'Approval requested',
}

// reasonText words one of GitHub's reasons.
export function reasonText(reason: string): string {
  return Object.hasOwn(reasonWords, reason) ? (reasonWords[reason] ?? reason) : reason
}

// reasonsText words GitHub's reasons, each once, in their order.
export function reasonsText(reasons: readonly string[]): string {
  return [...new Set(reasons.filter((r) => r !== '').map(reasonText))].join(', ')
}

// countsText says what the user's GitHub inbox holds, the unread first, as the toolbar's
// badge counts them: "5 unread · 38 in your GitHub inbox", or that it is empty, or that it
// hasn't been read yet.
export function countsText(status: GitHubThreadsStatus): string {
  if (status.lastPassAt === '') {
    return 'Not read from GitHub yet'
  }

  if (status.count === 0) {
    return 'Nothing in your GitHub inbox'
  }

  const inbox = `${formatCount(status.count)} in your GitHub inbox`

  return status.unread > 0 ? `${formatCount(status.unread)} unread · ${inbox}` : inbox
}

// summaryText is the panel's line under its title: the counts, the unread first as the
// badge counts them, then when GitHub was last checked, or that a check is under way: "5
// unread · 38 in your GitHub inbox · checked 2m ago".
export function summaryText(now: Date, status: GitHubThreadsStatus, checking: boolean): string {
  const counts = countsText(status)

  if (checking) {
    return `${counts} · checking…`
  }

  const ago = formatAgo(now, status.lastPassAt)

  return ago === '' ? counts : `${counts} · checked ${ago}`
}

// checkedText says what the GitHub inbox held at the last check, and when that was: "38 in
// your GitHub inbox · checked 2m ago".
export function checkedText(now: Date, status: GitHubThreadsStatus): string {
  const inbox =
    status.count === 0 ? 'Nothing in your GitHub inbox' : `${formatCount(status.count)} in your GitHub inbox`
  const ago = formatAgo(now, status.lastPassAt)

  return ago === '' ? inbox : `${inbox} · checked ${ago}`
}

// buttonLabel names the toolbar button with what its badge or dot says, the unread first.
export function buttonLabel(status: GitHubThreadsStatus): string {
  if (status.access === 'refused') {
    return 'GitHub notifications, can’t be read'
  }

  if (status.count === 0) {
    return 'GitHub notifications'
  }

  const unread = status.unread > 0 ? `${formatCount(status.unread)} unread, ` : ''

  return `GitHub notifications, ${unread}${formatCount(status.count)} in your inbox`
}

// refusal is why GitHub refuses the notifications, as the status words it.
export function refusal(status: GitHubThreadsStatus): string {
  return status.lastError === '' ? refusedText : status.lastError
}

// askAgainText says when a listing GitHub refused is sent again: "Asks GitHub again in
// 5h.", or '' when that isn't known.
export function askAgainText(now: Date, status: GitHubThreadsStatus): string {
  const until = formatUntil(now, status.askAgainAt)

  if (until === '') {
    return ''
  }

  return until === 'now' ? 'Asks GitHub again at the next check.' : `Asks GitHub again in ${until}.`
}

// ThreadGroup is one line of a list of notifications: every notification about one item,
// or one about no item. The toolbar's panel lists the unread alone; the Notifications page
// lists the read too.
export interface ThreadGroup {
  key: string
  // item is the item the notifications are about, or null for one about no item.
  item: ThreadItem | null
  title: string
  // subjectType is what it is about, in GitHub's words (PullRequest, Release, ...).
  subjectType: string
  // repo is the repository, "owner/name", or '' when GitHub named none.
  repo: string
  // reasons are GitHub's reasons, each once, the newest notification's first.
  reasons: string[]
  // updatedAt is when the newest of them last changed.
  updatedAt: string
  // count is how many notifications the line stands for, and ids are theirs, which its
  // Done marks done; unread is how many of them are unread, and unreadIds theirs, which
  // opening the line, or its Mark read, marks read.
  count: number
  ids: number[]
  unread: number
  unreadIds: number[]
  // state is its item's state as the newest notification that knows it says, or '' when
  // none does, as for one about no item.
  state: GitHubThreadState
  // threads are the notifications themselves, the newest first.
  threads: GitHubThread[]
}

// threadGroups groups notifications, the newest first, by the item each is about, in the
// order of each item's newest; one about no item stands alone.
export function threadGroups(threads: readonly GitHubThread[]): ThreadGroup[] {
  const groups: ThreadGroup[] = []
  const byKey = new Map<string, ThreadGroup>()

  for (const t of threads) {
    const item = threadItem(t)
    const key = item === null ? `thread-${String(t.id)}` : threadItemKey(item)
    let group = byKey.get(key)

    if (group === undefined) {
      group = {
        key,
        item,
        title: t.title,
        subjectType: t.subjectType,
        repo: t.owner === '' || t.name === '' ? '' : `${t.owner}/${t.name}`,
        reasons: [],
        updatedAt: t.updatedAt,
        count: 0,
        ids: [],
        unread: 0,
        unreadIds: [],
        state: '',
        threads: [],
      }

      byKey.set(key, group)
      groups.push(group)
    }

    group.count += 1
    group.ids.push(t.id)
    group.threads.push(t)

    if (t.unread) {
      group.unread += 1
      group.unreadIds.push(t.id)
    }

    if (t.reason !== '' && !group.reasons.includes(t.reason)) {
      group.reasons.push(t.reason)
    }

    if (group.state === '') {
      group.state = t.state
    }
  }

  return groups
}

// stateWords words an item's state for its line's chip: merged or closed, which the user
// may be done with; an open item, or one whose state isn't known, has none.
export function stateWords(state: GitHubThreadState): string {
  switch (state) {
    case 'MERGED':
      return 'Merged'
    case 'CLOSED':
      return 'Closed'
    default:
      return ''
  }
}

// GitHubShow is what the page's GitHub tab shows: the unread notifications, every one, or
// what clearing did.
export type GitHubShow = 'unread' | 'all' | 'cleared'

// GitHubTypeID is one of the GitHub tab's types, as its Type control offers it and type:
// names it in the toolbar's field.
export type GitHubTypeID = 'all' | 'pr' | 'issue' | 'release' | 'actions' | 'other'

// GitHubType is a type's name, and what the page says when it lists none.
export interface GitHubType {
  label: string
  none: string
}

// githubTypes are the GitHub tab's types: pull requests, issues, releases, workflow runs,
// and every other subject together.
export const githubTypes: Readonly<Record<GitHubTypeID, GitHubType>> = {
  all: { label: 'All', none: 'Nothing in your GitHub inbox.' },
  pr: { label: 'Pull requests', none: 'No notifications about pull requests.' },
  issue: { label: 'Issues', none: 'No notifications about issues.' },
  release: { label: 'Releases', none: 'No notifications about releases.' },
  actions: { label: 'Actions', none: 'No notifications about workflow runs.' },
  other: { label: 'Other', none: 'No other notifications.' },
}

// githubTypeOrder is the order the page offers the types in.
export const githubTypeOrder: readonly GitHubTypeID[] = ['all', 'pr', 'issue', 'release', 'actions', 'other']

// githubTypeOf is the type a notification belongs to, by what it is about.
export function githubTypeOf(thread: { kind: ItemKind | ''; subjectType: string }): Exclude<GitHubTypeID, 'all'> {
  if (thread.kind === 'pr' || thread.subjectType === 'PullRequest') {
    return 'pr'
  }

  if (thread.kind === 'issue' || thread.subjectType === 'Issue') {
    return 'issue'
  }

  switch (thread.subjectType) {
    case 'Release':
      return 'release'
    case 'CheckSuite':
    case 'WorkflowRun':
      return 'actions'
    default:
      return 'other'
  }
}

// filterThreads picks the notifications a list shows, in their order: the unread alone or
// all of them, of one type or every type. Cleared lists the clears instead, and no
// notification.
export function filterThreads(threads: readonly GitHubThread[], show: GitHubShow, type: GitHubTypeID): GitHubThread[] {
  if (show === 'cleared') {
    return []
  }

  return threads.filter((t) => (show === 'all' || t.unread) && (type === 'all' || githubTypeOf(t) === type))
}

// ClearTrigger is a moment the work behind a notification is finished, which clears it
// once its switch is on, as Settings lists it.
export interface ClearTrigger {
  trigger: ThreadTrigger
  label: string
  description: string
}

// clearTriggers are the moments the work behind a notification is finished, each with its
// own switch, in the order the backend lists them. Clearing by hand has none. Settings
// lists them, the Cleared view says which one cleared a notification, and the settings'
// search index matches their labels word for word.
export const clearTriggers: readonly ClearTrigger[] = [
  { trigger: 'merged', label: 'A pull request is merged', description: 'Here or anywhere else.' },
  {
    trigger: 'pr_closed',
    label: 'A pull request is closed without merging',
    description: 'Here or anywhere else.',
  },
  {
    trigger: 'issue_closed',
    label: 'An issue is closed',
    description: 'As completed or as not planned, here or anywhere else.',
  },
  {
    trigger: 'reviewed',
    label: 'You approve or request changes on a pull request',
    description: `Here, from an agent’s session, in your browser, or anywhere else, once ${brand.name} sees it.`,
  },
  {
    trigger: 'dismissed',
    label: `You dismiss an item in ${brand.name}`,
    description: 'From its card or its sheet.',
  },
  {
    trigger: 'unassigned',
    label: 'You unassign yourself or remove yourself as reviewer',
    description: 'With Unassign me or Remove me as reviewer, from its card or its sheet.',
  },
]

// byHandText says a notification was cleared by the user's own hand, which has no switch.
export const byHandText = 'You cleared it'

// triggerText says why a notification cleared: its trigger's label, or that the user
// cleared it. A trigger this build doesn't know shows as the backend wrote it.
export function triggerText(trigger: ThreadTrigger): string {
  if (trigger === 'by_hand') {
    return byHandText
  }

  return clearTriggers.find((t) => t.trigger === trigger)?.label ?? trigger
}

// clearsIn says when a queued clear goes: "in 12s", "in 5m", or "any moment" once its
// hold is over.
export function clearsIn(now: Date, sendAfter: string): string {
  const at = Date.parse(sendAfter)

  if (Number.isNaN(at)) {
    return ''
  }

  const seconds = Math.ceil((at - now.getTime()) / 1000)

  if (seconds <= 0) {
    return 'any moment'
  }

  return seconds < 60 ? `in ${String(seconds)}s` : `in ${formatUntil(now, sendAfter)}`
}

// skipWords say why a clear was skipped. Skips the backend decided again (changed, stale)
// aren't shown, so they have none.
const skipWords: Readonly<Record<string, string>> = {
  off: 'clearing was turned off',
  rule_off: 'its rule is off',
  still_open: 'it was reopened',
  new_activity: 'there was new activity',
  already_read: 'it was already read',
  not_item: 'it isn’t a pull request or an issue',
  not_listed: 'it left your GitHub inbox first',
  kept: 'you kept it before',
  failed: 'it failed before',
}

// shownSettled reports a settled clear the Cleared view lists: every one but a skip the
// backend decided again, which says nothing that happened.
export function shownSettled(clear: SettledClear): boolean {
  return !(clear.state === 'skipped' && (clear.reason === 'changed' || clear.reason === 'stale'))
}

// settledText says how a clear settled: "Marked done", "Marked read", "Kept", "Couldn’t
// clear: …", or "Skipped: …".
export function settledText(clear: SettledClear): string {
  switch (clear.state) {
    case 'sent':
      return clear.action === 'read' ? 'Marked read' : 'Marked done'
    case 'kept':
      return 'Kept'
    case 'failed':
      return clear.error === '' ? 'Couldn’t clear' : `Couldn’t clear: ${clear.error}`
    case 'skipped': {
      const why = Object.hasOwn(skipWords, clear.reason) ? skipWords[clear.reason] : undefined

      return why === undefined ? 'Skipped' : `Skipped: ${why}`
    }
    default:
      return clear.state
  }
}

// clearItem is the item a queued or settled clear is about, or null when it names none a
// link could hold.
function clearItem(clear: QueuedClear | SettledClear): ThreadItem | null {
  return threadItem(clear)
}

// clearWhere says what a clear is about: "example-repo #7", or its repository alone.
export function clearWhere(clear: QueuedClear | SettledClear): string {
  return clear.number > 0 ? `${clear.name} #${String(clear.number)}` : clear.name
}

// clearURL is where a clear's line opens on GitHub: its item's page, or else the GitHub
// inbox.
export function clearURL(clear: QueuedClear | SettledClear): string {
  const item = clearItem(clear)

  return item === null ? inboxURL : itemURL(item)
}

// subjectWords are the subjects GitHub names, worded, for a notification about no item.
const subjectWords: Readonly<Record<string, string>> = {
  Release: 'Release',
  CheckSuite: 'Workflow run',
  Discussion: 'Discussion',
  Commit: 'Commit',
  RepositoryVulnerabilityAlert: 'Security alert',
  RepositoryDependabotAlertsThread: 'Dependabot alerts',
}

// subjectPages are the page of its repository a notification about no item opens on, by
// what it is about: a workflow run's Actions, a release's Releases, and so on; any other
// opens the repository.
const subjectPages: Readonly<Record<string, string>> = {
  CheckSuite: 'actions',
  WorkflowRun: 'actions',
  Release: 'releases',
  Discussion: 'discussions',
  Commit: 'commits',
  RepositoryVulnerabilityAlert: 'security',
  RepositoryDependabotAlertsThread: 'security/dependabot',
}

// repoURL is a repository's page on GitHub, from "owner/name", or '' when it names none a
// link could hold.
function repoURL(repo: string): string {
  const [owner = '', name = '', ...rest] = repo.split('/')

  if (rest.length > 0 || !validSegment(owner) || !validSegment(name)) {
    return ''
  }

  return `https://github.com/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`
}

// groupURL is where a line opens on GitHub, as GitHub's own inbox opens it: its item's
// page, or, for one about no item, the page of its repository it belongs on (subjectPages),
// or else the GitHub inbox. Every link is built from fixed parts and the repository's
// checked name: none is an address GitHub sent.
export function groupURL(group: ThreadGroup): string {
  if (group.item !== null) {
    return itemURL(group.item)
  }

  const repo = repoURL(group.repo)

  if (repo === '') {
    return inboxURL
  }

  const page = Object.hasOwn(subjectPages, group.subjectType) ? subjectPages[group.subjectType] : undefined

  return page === undefined ? repo : `${repo}/${page}`
}

// groupWhere says where a line's notifications are: "example-repo #7", or "acme-org/sdk ·
// Release" for one about no item.
export function groupWhere(group: ThreadGroup): string {
  if (group.item !== null) {
    return `${group.item.name} #${String(group.item.number)}`
  }

  const subject = Object.hasOwn(subjectWords, group.subjectType)
    ? (subjectWords[group.subjectType] ?? group.subjectType)
    : group.subjectType

  return [group.repo, subject].filter((part) => part !== '').join(' · ')
}

// Marking notifications done in bulk (Mark all done): the backend runs the job, one
// notification at a time, and the window chooses what it marks, from the list it already
// holds, and words how it goes.

// BulkAgeOption is one of the ages a bulk mark offers, as the dialog's Older than names it,
// and how many days back it reaches.
export interface BulkAgeOption {
  value: GitHubBulkAge
  label: string
  days: number
}

// bulkAges are the ages a bulk mark offers, the youngest first, as the backend measures
// them.
export const bulkAges: readonly BulkAgeOption[] = [
  { value: 'any', label: 'Any age', days: 0 },
  { value: 'day', label: '1 day', days: 1 },
  { value: 'week', label: '1 week', days: 7 },
  { value: 'month', label: '1 month', days: 30 },
  { value: 'quarter', label: '3 months', days: 90 },
]

// dayMs is a day, in milliseconds.
const dayMs = 24 * 60 * 60 * 1000

// BulkRepo is one repository a bulk mark offers: "owner/name", how many notifications it
// holds at the age chosen, and how many of them are unread.
export interface BulkRepo {
  repo: string
  count: number
  unread: number
}

// bulkRepos counts threads by repository, those updated before now less age, the most
// first, then by name. A repository with none at that age isn't there, nor a thread about
// no repository, which only a bulk mark of everything reaches.
export function bulkRepos(threads: readonly GitHubThread[], age: GitHubBulkAge, now: Date): BulkRepo[] {
  const days = bulkAges.find((a) => a.value === age)?.days ?? 0
  const cutoff = now.getTime() - days * dayMs
  const byRepo = new Map<string, BulkRepo>()

  for (const t of threads) {
    const updated = Date.parse(t.updatedAt)

    if (t.owner === '' || t.name === '' || Number.isNaN(updated) || updated >= cutoff) {
      continue
    }

    const repo = `${t.owner}/${t.name}`
    const row = byRepo.get(repo) ?? { repo, count: 0, unread: 0 }

    row.count++
    row.unread += t.unread ? 1 : 0
    byRepo.set(repo, row)
  }

  return [...byRepo.values()].sort((a, b) => b.count - a.count || a.repo.localeCompare(b.repo))
}

// bulkMinutes is how long marking n done takes, in minutes, at most perMinute a minute and
// perHour an hour: past an hour's worth, each one waits for the hour to pass.
export function bulkMinutes(n: number, perMinute: number, perHour: number): number {
  if (n <= 0 || perMinute <= 0) {
    return 0
  }

  const hours = perHour > 0 ? Math.floor((n - 1) / perHour) : 0

  return hours * 60 + (n - hours * perHour) / perMinute
}

// aboutText says roughly how long minutes take: "less than a minute", "about 25 minutes",
// "about 1 hour 15 minutes", to the five minutes past the first hour.
export function aboutText(minutes: number): string {
  if (minutes < 1) {
    return 'less than a minute'
  }

  if (minutes < 59.5) {
    return `about ${counted(Math.round(minutes), 'minute', 'minutes')}`
  }

  const fives = Math.round(minutes / 5)
  const hours = Math.floor(fives / 12)
  const rest = (fives % 12) * 5
  const spoken = counted(hours, 'hour', 'hours')

  return rest === 0 ? `about ${spoken}` : `about ${spoken} ${counted(rest, 'minute', 'minutes')}`
}

// bulkKeepsText says what a bulk mark at age leaves in the user's inbox.
export function bulkKeepsText(age: GitHubBulkAge): string {
  const option = bulkAges.find((a) => a.value === age)

  return option === undefined || option.days === 0
    ? 'Anything updated after now stays in your inbox.'
    : `Anything updated in the last ${option.label.replace(/^1 /, '')} stays in your inbox.`
}

// BulkLineKind is how a bulk mark's line reads: going, waiting, waiting on a failure,
// ended, ended with some it couldn't mark, or stopped.
export type BulkLineKind = 'running' | 'paused' | 'failing' | 'done' | 'partial' | 'stopped'

// BulkLine is a bulk mark's status line: its words, how far it got (0 to 1) while it runs,
// or null when that can't be told, or it ended, and how it reads.
export interface BulkLine {
  text: string
  share: number | null
  kind: BulkLineKind
}

// inText says when iso comes, as a wait: "in 40 seconds", "in 2 minutes", "in a moment".
function inText(now: Date, iso: string): string {
  const at = Date.parse(iso)
  const seconds = Number.isNaN(at) ? 0 : Math.ceil((at - now.getTime()) / 1000)

  if (seconds <= 0) {
    return 'in a moment'
  }

  return seconds < 60
    ? `in ${counted(seconds, 'second', 'seconds')}`
    : `in ${counted(Math.ceil(seconds / 60), 'minute', 'minutes')}`
}

// pausedText says why a running bulk mark waits, or '' when it goes on.
function pausedText(now: Date, b: GitHubBulkDone): string {
  switch (b.pause) {
    case 'held':
      return `Paused: GitHub asked for a short break. Resuming ${inText(now, b.nextAt)}.`
    case 'reserve': {
      const at = formatTimeOfDay(b.nextAt)

      return at === ''
        ? 'Paused until your GitHub API budget refills.'
        : `Paused until your GitHub API budget refills at ${at}.`
    }
    case 'otherAccount':
      return 'Paused: your GitHub token is for another account now.'
    case 'readingOff':
      return `Paused while ${brand.name} isn’t reading your GitHub notifications.`
    case 'failing':
      return `Couldn’t reach GitHub. Trying again ${inText(now, b.nextAt)}.`
    case 'none':
      return ''
  }
}

// runningText says how far a running bulk mark got: "Marking done on GitHub: 412 of 1,180
// · about 19 minutes left", with "or more" when GitHub holds more than the app mirrors.
function runningText(b: GitHubBulkDone): string {
  const total = b.done + b.failed + b.left

  if (total === 0) {
    return 'Marking done on GitHub…'
  }

  if (b.left === 0) {
    return `Marking done on GitHub: ${formatCount(b.done)} so far`
  }

  const of = `${formatCount(total)}${b.more ? ' or more' : ''}`
  const left = aboutText(bulkMinutes(b.left, b.perMinute, b.perHour))

  return `Marking done on GitHub: ${formatCount(b.done)} of ${of} · ${left} left`
}

// endedText says how a bulk mark that ended went.
function endedText(b: GitHubBulkDone): BulkLine {
  if (b.stopped) {
    return {
      text: `Stopped after marking ${formatCount(b.done)} done. The rest are still in your inbox.`,
      share: null,
      kind: 'stopped',
    }
  }

  const marked = `Marked ${formatCount(b.done)} done on GitHub`

  if (b.failed === 0) {
    return { text: marked, share: null, kind: 'done' }
  }

  return {
    text: `${marked} · ${formatCount(b.failed)} couldn’t be marked; they’re still in your inbox`,
    share: null,
    kind: 'partial',
  }
}

// bulkDoneText is the line that says how the bulk mark stands, as of now: how far it got,
// why it waits, or how it ended; null when there is none to show.
export function bulkDoneText(now: Date, b: GitHubBulkDone): BulkLine | null {
  if (b.startedAt === '') {
    return null
  }

  if (!b.running) {
    return endedText(b)
  }

  const total = b.done + b.failed + b.left
  const share = total === 0 ? null : (b.done + b.failed) / total
  const paused = pausedText(now, b)

  if (paused !== '') {
    return { text: paused, share, kind: b.pause === 'failing' ? 'failing' : 'paused' }
  }

  return { text: runningText(b), share, kind: 'running' }
}

// bulkToastText is what the toast says once a bulk mark finished: "Marked 1,180
// notifications done on GitHub", with "3 couldn’t be marked" as its detail when some
// couldn't be.
export function bulkToastText(b: GitHubBulkDone): { text: string; detail: string } {
  return {
    text: `Marked ${counted(b.done, 'notification', 'notifications')} done on GitHub`,
    detail: b.failed === 0 ? '' : `${formatCount(b.failed)} couldn’t be marked`,
  }
}
