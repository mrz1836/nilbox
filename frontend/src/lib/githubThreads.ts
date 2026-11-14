// The user's GitHub notifications as the window shows them: the toolbar button's badge and
// counts, the newest unread grouped by the item each is about, and the words for GitHub's
// reasons. The backend mirrors them (internal/ghthreads) and gives
// no URL GitHub returned: every link here is built from an item's repository, kind, and
// number, with fixed strings.

import type { GitHubThread, GitHubThreadsStatus, ItemKind } from '../platform'
import { formatCount } from './format'
import { itemKey } from './itemWorktrees'
import { formatAgo, formatUntil } from './time'

// inboxURL is the user's GitHub inbox on the web.
export const inboxURL = 'https://github.com/notifications'

// refusedText is what the window says while GitHub refuses the token the notifications,
// when the status carries no words of its own.
export const refusedText = 'Your GitHub token can’t read notifications.'

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
export function threadItemKey(item: { owner: string; name: string; number: number }): string {
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

// badgeText is the toolbar button's count: the number, up to 99, then 99+.
export function badgeText(count: number): string {
  return count > 99 ? '99+' : formatCount(count)
}

// countsText says what the user's GitHub inbox holds: "38 in your GitHub inbox · 5 unread",
// or that it is empty, or that it hasn't been read yet.
export function countsText(status: GitHubThreadsStatus): string {
  if (status.lastPassAt === '') {
    return 'Not read from GitHub yet'
  }

  if (status.count === 0) {
    return 'Nothing in your GitHub inbox'
  }

  const inbox = `${formatCount(status.count)} in your GitHub inbox`

  return status.unread > 0 ? `${inbox} · ${formatCount(status.unread)} unread` : inbox
}

// checkedText says what the GitHub inbox held at the last check, and when that was: "38 in
// your GitHub inbox · checked 2m ago".
export function checkedText(now: Date, status: GitHubThreadsStatus): string {
  const inbox =
    status.count === 0 ? 'Nothing in your GitHub inbox' : `${formatCount(status.count)} in your GitHub inbox`
  const ago = formatAgo(now, status.lastPassAt)

  return ago === '' ? inbox : `${inbox} · checked ${ago}`
}

// buttonLabel names the toolbar button with what its badge or dot says.
export function buttonLabel(status: GitHubThreadsStatus): string {
  if (status.access === 'refused') {
    return 'GitHub notifications, can’t be read'
  }

  if (status.count === 0) {
    return 'GitHub notifications'
  }

  const unread = status.unread > 0 ? `, ${formatCount(status.unread)} unread` : ''

  return `GitHub notifications, ${formatCount(status.count)} in your inbox${unread}`
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

// ThreadGroup is one line of the toolbar's list of unread notifications: every unread
// notification about one item, or one about no item.
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
  // count is how many unread notifications the line stands for.
  count: number
}

// threadGroups groups the newest unread notifications, the newest first, by the item each
// is about, in the order of each item's newest; one about no item stands alone.
export function threadGroups(threads: readonly GitHubThread[]): ThreadGroup[] {
  const groups: ThreadGroup[] = []
  const byKey = new Map<string, ThreadGroup>()

  for (const t of threads) {
    const item = threadItem(t)
    const key = item === null ? `thread-${String(t.id)}` : threadItemKey(item)
    const found = byKey.get(key)

    if (found !== undefined) {
      found.count += 1

      if (t.reason !== '' && !found.reasons.includes(t.reason)) {
        found.reasons.push(t.reason)
      }

      continue
    }

    const group: ThreadGroup = {
      key,
      item,
      title: t.title,
      subjectType: t.subjectType,
      repo: t.owner === '' || t.name === '' ? '' : `${t.owner}/${t.name}`,
      reasons: t.reason === '' ? [] : [t.reason],
      updatedAt: t.updatedAt,
      count: 1,
    }

    byKey.set(key, group)
    groups.push(group)
  }

  return groups
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
