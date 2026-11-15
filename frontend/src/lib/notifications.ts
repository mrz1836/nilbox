import type { AppNotification, BannerStatus, NotificationKind } from '../platform'
import type { GitHubShow, GitHubTypeID } from './githubThreads'

// The kinds of notification, as the backend names them (internal/notify), with what the
// history calls each one and how Settings describes its banners.

export interface KindDescription {
  kind: NotificationKind
  // session marks a kind about the agent sessions, which the history never keeps.
  session?: boolean
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

// sessionBannerHint explains why the sessions' banners start off.
export const sessionBannerHint = 'Claude Code and Codex can already notify through your terminal, so these start off.'

// inboxKinds and sessionKinds are the kinds of notification the history keeps (the
// inbox's, a release's (reaching GitHub, published, or its workflow failing), a run that
// failed, and a service the user watches going down or coming back), and those about the
// agent sessions, which it does not.
export const inboxKinds = notificationKinds.filter((k) => k.session !== true)
export const sessionKinds = notificationKinds.filter((k) => k.session === true)

// kindTitle heads an entry of a kind in the history.
export function kindTitle(kind: NotificationKind): string {
  return notificationKinds.find((k) => k.kind === kind)?.title ?? 'Notification'
}

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
export type AppTypeID = 'all' | 'review' | 'assigned' | 'snooze' | 'release' | 'run' | 'status'

// AppType is a type's name, the kinds of entry it lists (none for every kind), and what
// the page says when it lists none.
export interface AppType {
  label: string
  kinds: readonly NotificationKind[]
  none: string
}

// appTypes are the app tab's types: a release's three kinds together, each other kind
// alone.
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
}

// appTypeOrder is the order the page offers the types in.
export const appTypeOrder: readonly AppTypeID[] = ['all', 'review', 'assigned', 'snooze', 'release', 'run', 'status']

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

// bannerEntry reports whether the log's entry a clicked banner names is the one it stood
// for: the log hands an id out again once it is cleared, so a banner left from before
// stands for the entry with its id only while that entry is about the banner's item, page,
// or run.
export function bannerEntry(entry: AppNotification, banner: { itemId: number; url: string; runId: number }): boolean {
  return (
    (banner.itemId > 0 && entry.itemId === banner.itemId) ||
    (banner.runId > 0 && entry.runId === banner.runId) ||
    (banner.url !== '' && entry.url === banner.url)
  )
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
