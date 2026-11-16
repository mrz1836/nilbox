import { brand } from '../generated/brand'
import type { AppNotification, AutopilotTarget, BannerStatus, NotificationKind } from '../platform'
import type { GitHubShow, GitHubTypeID } from './githubThreads'

// The kinds of notification, as the backend names them (internal/notify), with what the
// history calls each one and how Settings describes its banners.

export interface KindDescription {
  kind: NotificationKind
  // session marks a kind about the agent sessions, which the history never keeps.
  session?: boolean
  // autopilot marks one of Autopilot's kinds, whose switches Settings lists under a
  // heading of their own, and whose type the Notifications page offers together.
  autopilot?: boolean
  // title heads an entry in the history, as it heads its banner.
  title: string
  // label and description are the kind's switch in Settings.
  label: string
  description: string
}

export const notificationKinds: readonly KindDescription[] = [
  {
    kind: 'review_requested',
    title: 'Review requested',
    label: 'Review requests',
    description: 'A pull request asks for your review, including when it asks again.',
  },
  {
    kind: 'assigned',
    title: 'Assigned to you',
    label: 'Assignments',
    description: 'An issue or pull request someone else opened is assigned to you.',
  },
  {
    kind: 'woke',
    title: 'Back from snooze',
    label: 'Snoozes ending early',
    description: 'New activity brings a snoozed item back before its time, under your wake rules.',
  },
  {
    kind: 'release_done',
    title: 'Released',
    label: 'Releases',
    description: 'A release you started from the Scripts tab reaches GitHub: its tag is on origin.',
  },
  {
    kind: 'release_published',
    title: 'Published',
    label: 'Releases published',
    description: 'The release workflow on GitHub publishes a release you started from the Scripts tab.',
  },
  {
    kind: 'release_failed',
    title: 'Release workflow failed',
    label: 'Release workflows failing',
    description: 'A workflow the tag of a release you started from the Scripts tab runs on GitHub fails.',
  },
  {
    kind: 'run_failed',
    title: 'Run failed',
    label: 'Failed runs',
    description:
      'A run from the Scripts tab fails or runs past its time limit, in the app or in your terminal. A run you stop never notifies.',
  },
  {
    kind: 'service_status',
    title: 'Service status',
    label: 'Service status changes',
    description:
      'A part of GitHub, Claude, or OpenAI you watch goes down, or comes back, as its status page says. Choose what you watch in Service status.',
  },
  {
    kind: 'update_ready',
    title: 'New version',
    label: 'New versions',
    description: `A new version of ${brand.name} is downloaded and ready to restart into, or, with Just tell me, out to download. You hear of each version once.`,
  },
  {
    kind: 'autopilot_alert',
    autopilot: true,
    title: 'Autopilot alert',
    label: 'Routine alerts',
    description:
      'A routine’s Notify me or Tell me step runs. Its banner is headed by the routine’s name, and a routine can turn its own banners on or off.',
  },
  {
    kind: 'autopilot_needs_you',
    autopilot: true,
    title: 'Needs your OK',
    label: 'Needs your OK',
    description: 'A routine that asks first matched an item, and waits for you to say yes.',
  },
  {
    kind: 'autopilot_finished',
    autopilot: true,
    title: 'Agent finished',
    label: 'Agents finishing',
    description: 'A coding agent a routine started in the background ends, with what it did.',
  },
  {
    kind: 'autopilot_checks',
    autopilot: true,
    title: 'Checks after a fix',
    label: 'Checks after a fix',
    description: 'The checks on a routine’s fix finish: they pass, or still fail.',
  },
  {
    kind: 'autopilot_problem',
    autopilot: true,
    title: 'Autopilot problem',
    label: 'Autopilot problems',
    description:
      'A routine can’t run: its search fails, or it won’t load. You hear of each problem once, as it starts.',
  },
  {
    kind: 'session_needs_you',
    session: true,
    title: 'Session waiting for you',
    label: 'Sessions waiting for you',
    description: 'A Claude Code or Codex session you started has waited a few seconds for your approval or answer.',
  },
  {
    kind: 'session_finished',
    session: true,
    title: 'Session finished its turn',
    label: 'Sessions finishing a turn',
    description: 'A Claude Code or Codex session you started is done working and waits for your next message.',
  },
]

// everyKind holds every kind of notification the backend has: a record over the generated
// union, so a kind added in Go fails to compile until it is listed here, and the tests hold
// every list in the window (notificationKinds, appTypes, bannerOpens, EntryRow's icons) to
// it.
const everyKind: Readonly<Record<NotificationKind, true>> = {
  review_requested: true,
  assigned: true,
  woke: true,
  session_needs_you: true,
  session_finished: true,
  release_done: true,
  release_published: true,
  release_failed: true,
  run_failed: true,
  service_status: true,
  update_ready: true,
  autopilot_alert: true,
  autopilot_needs_you: true,
  autopilot_finished: true,
  autopilot_checks: true,
  autopilot_problem: true,
}

// allNotificationKinds are every kind of notification, in the backend's order.
export const allNotificationKinds = Object.keys(everyKind) as readonly NotificationKind[]

// sessionBannerHint explains why the sessions' banners start off.
export const sessionBannerHint = 'Claude Code and Codex can already notify through your terminal, so these start off.'

// inboxKinds, autopilotKinds, and sessionKinds are the kinds of notification the history
// keeps (the inbox's, a release's (reaching GitHub, published, or its workflow failing), a
// run that failed, a service the user watches going down or coming back, and a new version
// of the app), Autopilot's, which it keeps too, and those about the agent sessions, which
// it does not.
export const inboxKinds = notificationKinds.filter((k) => k.session !== true && k.autopilot !== true)
export const autopilotKinds = notificationKinds.filter((k) => k.autopilot === true)
export const sessionKinds = notificationKinds.filter((k) => k.session === true)

// kindTitle heads an entry of a kind in the history.
export function kindTitle(kind: NotificationKind): string {
  return notificationKinds.find((k) => k.kind === kind)?.title ?? 'Notification'
}

// entryHeading heads an entry in the history: its own heading, as an Autopilot alert is
// headed by its routine's name, or else its kind's title.
export function entryHeading(entry: AppNotification): string {
  return entry.heading === '' ? kindTitle(entry.kind) : entry.heading
}

// isAutopilotKind reports one of Autopilot's kinds.
export function isAutopilotKind(kind: NotificationKind): boolean {
  return autopilotKinds.some((k) => k.kind === kind)
}

// opensAutopilot reports an entry of Autopilot's about no item, as a problem is, which
// opens the Autopilot tab at what it names (its routine, or its match in Activity). One
// about an item opens that item, as an inbox arrival does, and is marked read when the
// item's sheet opens.
export function opensAutopilot(entry: AppNotification): boolean {
  return isAutopilotKind(entry.kind) && entry.itemKind === ''
}

// targetNamed reports an Autopilot target that names something to open.
export function targetNamed(target: AutopilotTarget): boolean {
  return target.routineId > 0 || target.matchId > 0 || target.runId > 0 || target.activity
}

// sameTarget reports two Autopilot targets that name the same thing, made at the same
// time: the history hands an id out again once it is cleared.
function sameTarget(a: AutopilotTarget, b: AutopilotTarget): boolean {
  return (
    targetNamed(a) &&
    a.routineId === b.routineId &&
    a.matchId === b.matchId &&
    a.runId === b.runId &&
    a.createdAt === b.createdAt
  )
}

// autopilotNotYetText says why an Autopilot notification, or a row of the menu bar's
// Autopilot group, opens nothing yet: its tab isn't in this version.
export const autopilotNotYetText = `This opens in the Autopilot tab, which this version of ${brand.name} doesn’t have yet.`

// keptRunningText says, once, why the app kept running when its window was closed with
// the menu bar icon off: Autopilot had a routine on or an agent working.
export const keptRunningText = `${brand.name} kept running, so Autopilot can go on: a routine is on, or an agent is working. Click ${brand.name} in the Dock to come back here, or quit with ⌘Q.`

// entryRef names an entry's item: "example-repo #12", or, for an entry about no item, as
// a release is, its repository alone: "example-repo", or '' for none, as a run in a
// folder that isn't a repository has.
export function entryRef(entry: AppNotification): string {
  const name = entry.repo.slice(entry.repo.lastIndexOf('/') + 1)

  return entry.itemKind === '' ? name : `${name} #${String(entry.number)}`
}

// entryWhere is an entry's line under its title: its item, then what it says besides, as
// "sdk · Tagged a1b2c3d on master.", either alone when the other is ''.
export function entryWhere(entry: AppNotification): string {
  return [entryRef(entry), entry.detail].filter((part) => part !== '').join(' · ')
}

// opensUpdate reports an entry about a new version of the app, which opens the update's
// banner, Later or not.
export function opensUpdate(entry: AppNotification): boolean {
  return entry.kind === 'update_ready'
}

// opensRun reports an entry that opens a run, as a failed run's does: Scripts → Runs with
// the run in its panel.
export function opensRun(entry: AppNotification): boolean {
  return entry.kind === 'run_failed' && entry.runId > 0
}

// entryTitle is an entry's full reference, for its tooltip: "acme-org/example-repo #12",
// "acme-org/sdk" for an entry about no item, or none when the repository is unknown.
export function entryTitle(entry: AppNotification): string | undefined {
  if (entry.repo === '') {
    return undefined
  }

  return entry.itemKind === '' ? entry.repo : `${entry.repo} #${String(entry.number)}`
}

// quickViewMax is how many unread entries the bell's quick view lists; the rest wait on
// the Notifications page.
export const quickViewMax = 20

// NotificationShow is what a list of the Notifications page shows: the unread alone, or
// every entry, the read too.
export type NotificationShow = 'unread' | 'all'

// NotificationLists are the Notifications page's Show and Type on each tab, which the
// window holds, so the toolbar's field counts what the list shows.
export interface NotificationLists {
  app: { show: NotificationShow; type: AppTypeID }
  github: { show: GitHubShow; type: GitHubTypeID }
}

// startLists are the page's lists as the window opens: every entry, of every type, since
// the page is the history.
export const startLists: NotificationLists = {
  app: { show: 'all', type: 'all' },
  github: { show: 'all', type: 'all' },
}

// AppTypeID is one of the app tab's types, as its Type control offers it and type: names
// it in the toolbar's field.
export type AppTypeID = 'all' | 'review' | 'assigned' | 'snooze' | 'release' | 'run' | 'status' | 'update' | 'autopilot'

// AppType is a type's name, the kinds of entry it lists (none for every kind), and what
// the page says when it lists none.
export interface AppType {
  label: string
  kinds: readonly NotificationKind[]
  none: string
}

// appTypes are the app tab's types: a release's three kinds together, Autopilot's five
// together, each other kind alone.
export const appTypes: Readonly<Record<AppTypeID, AppType>> = {
  all: { label: 'All', kinds: [], none: 'No notifications yet.' },
  review: { label: 'Reviews', kinds: ['review_requested'], none: 'No review requests.' },
  assigned: { label: 'Assigned', kinds: ['assigned'], none: 'Nothing assigned to you.' },
  snooze: { label: 'Snoozes', kinds: ['woke'], none: 'No snoozes ended early.' },
  release: {
    label: 'Releases',
    kinds: ['release_done', 'release_published', 'release_failed'],
    none: 'No releases.',
  },
  run: { label: 'Runs', kinds: ['run_failed'], none: 'No failed runs.' },
  status: { label: 'Status', kinds: ['service_status'], none: 'No service status changes.' },
  update: { label: 'Updates', kinds: ['update_ready'], none: `No new versions of ${brand.name} yet.` },
  autopilot: {
    label: 'Autopilot',
    kinds: autopilotKinds.map((k) => k.kind),
    none: 'Nothing from Autopilot yet.',
  },
}

// appTypeOrder is the order the page offers the types in.
export const appTypeOrder: readonly AppTypeID[] = [
  'all',
  'review',
  'assigned',
  'snooze',
  'release',
  'run',
  'status',
  'update',
  'autopilot',
]

// appTypeOf is the type an entry belongs to, or '' for a kind this build doesn't know.
export function appTypeOf(entry: AppNotification): Exclude<AppTypeID, 'all'> | '' {
  for (const id of appTypeOrder) {
    if (id !== 'all' && appTypes[id].kinds.includes(entry.kind)) {
      return id
    }
  }

  return ''
}

// filterEntries picks the entries a list shows, in their order: the unread alone or all of
// them, of one type or every type.
export function filterEntries(
  items: readonly AppNotification[],
  show: NotificationShow,
  type: AppTypeID,
): AppNotification[] {
  const { kinds } = appTypes[type]

  return items.filter((n) => (show === 'all' || !n.read) && (kinds.length === 0 || kinds.includes(n.kind)))
}

// unreadOf is the ids of the unread entries about an item, which opening the item marks
// read.
export function unreadOf(items: readonly AppNotification[], itemId: number): number[] {
  return itemId <= 0 ? [] : items.filter((n) => !n.read && n.itemId === itemId).map((n) => n.id)
}

// BannerRef is what a clicked banner names besides its entry: its item, page, run, or what
// it opens in the Autopilot tab, or none of them, as a new version's banner does.
export interface BannerRef {
  itemId: number
  url: string
  runId: number
  autopilot: AutopilotTarget
}

// updateBanner reports a clicked banner that names nothing but its entry, as only a new
// version's banner does.
export function updateBanner(banner: BannerRef): boolean {
  return banner.itemId <= 0 && banner.runId <= 0 && banner.url === '' && !targetNamed(banner.autopilot)
}

// bannerEntry reports whether the log's entry a clicked banner names is the one it stood
// for: the log hands an id out again once it is cleared, so a banner left from before
// stands for the entry with its id only while that entry is about the banner's item, page,
// run, or Autopilot target, or, for a banner about none, while it is about a new version.
export function bannerEntry(entry: AppNotification, banner: BannerRef): boolean {
  return (
    (banner.itemId > 0 && entry.itemId === banner.itemId) ||
    (banner.runId > 0 && entry.runId === banner.runId) ||
    (banner.url !== '' && entry.url === banner.url) ||
    sameTarget(banner.autopilot, entry.autopilot) ||
    (updateBanner(banner) && opensUpdate(entry))
  )
}

// BannerOpen is what a clicked banner opens: its run (Scripts → Runs), the update's banner,
// the Autopilot tab, or its item or page.
export type BannerOpen = 'run' | 'update' | 'autopilot' | 'item'

// bannerOpens works out what a clicked banner opens, as its entry in the log would: by
// that entry's kind (entry, the log's entry with the banner's notification id), while it
// is the one the banner stood for (bannerEntry); otherwise, as for an entry the log hasn't
// loaded, or a row of the menu bar's Autopilot group, by what the banner names: a run,
// something in the Autopilot tab when it names no item or page, nothing but its entry
// (named is whether it names one), as only a new version's banner does, or an item or
// page, an Autopilot banner's included.
export function bannerOpens(entry: AppNotification | undefined, banner: BannerRef, named: boolean): BannerOpen {
  if (entry !== undefined && bannerEntry(entry, banner)) {
    if (opensRun(entry)) {
      return 'run'
    }

    if (opensAutopilot(entry)) {
      return 'autopilot'
    }

    return opensUpdate(entry) ? 'update' : 'item'
  }

  if (banner.runId > 0) {
    return 'run'
  }

  if (targetNamed(banner.autopilot) && banner.itemId <= 0 && banner.url === '') {
    return 'autopilot'
  }

  return named && updateBanner(banner) ? 'update' : 'item'
}

// bannerStatusText explains the state of the banners in Settings, or '' when they show.
// The status is '' until it has been read.
export function bannerStatusText(status: BannerStatus | ''): string {
  switch (status) {
    case 'on':
      return ''
    case 'off':
      return 'Notifications are turned off for this app in System Settings › Notifications. The history keeps every one anyway.'
    case 'unavailable':
      return 'Banners need the packaged app, and this development build is not one. The history keeps every notification anyway.'
    default:
      return ''
  }
}
