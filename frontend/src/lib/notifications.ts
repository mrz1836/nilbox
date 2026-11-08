import type { AppNotification, BannerStatus, NotificationKind } from '../platform'

// The kinds of notification, as the backend names them (internal/notify), with what the
// history calls each one and how Settings describes its banners.

export interface KindDescription {
  kind: NotificationKind
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
]

// kindTitle heads an entry of a kind in the history.
export function kindTitle(kind: NotificationKind): string {
  return notificationKinds.find((k) => k.kind === kind)?.title ?? 'Notification'
}

// entryRef names an entry's item: "example-repo #12".
export function entryRef(entry: AppNotification): string {
  const name = entry.repo.slice(entry.repo.lastIndexOf('/') + 1)

  return `${name} #${String(entry.number)}`
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
