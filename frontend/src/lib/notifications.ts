import type { AppNotification, BannerStatus, NotificationKind } from '../platform'

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
// inbox's, and a release's: reaching GitHub, published, or its workflow failing), and those
// about the agent sessions, which it does not.
export const inboxKinds = notificationKinds.filter((k) => k.session !== true)
export const sessionKinds = notificationKinds.filter((k) => k.session === true)

// kindTitle heads an entry of a kind in the history.
export function kindTitle(kind: NotificationKind): string {
  return notificationKinds.find((k) => k.kind === kind)?.title ?? 'Notification'
}

// entryRef names an entry's item: "example-repo #12", or, for an entry about no item, as
// a release is, its repository alone: "example-repo".
export function entryRef(entry: AppNotification): string {
  const name = entry.repo.slice(entry.repo.lastIndexOf('/') + 1)

  return entry.itemKind === '' ? name : `${name} #${String(entry.number)}`
}

// entryTitle is an entry's full reference, for its tooltip: "acme-org/example-repo #12",
// "acme-org/sdk" for an entry about no item, or none when the repository is unknown.
export function entryTitle(entry: AppNotification): string | undefined {
  if (entry.repo === '') {
    return undefined
  }

  return entry.itemKind === '' ? entry.repo : `${entry.repo} #${String(entry.number)}`
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
