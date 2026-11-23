// Filtering the Notifications page with the toolbar's search field (see lib/query): which
// entries of the app's own history, and which of the user's GitHub notifications, a query
// keeps, and what the field suggests while it is typed. Everything it reads is what the
// page already holds, so filtering asks the backend nothing.

import type { AppNotification, GitHubThread } from '../platform'
import { reasonText, githubTypeOf, githubTypes, threadGroups, threadItem, type ThreadGroup } from './githubThreads'
import { appTypeOf, appTypes, kindTitle } from './notifications'
import {
  fold,
  haystack,
  isEmpty,
  matchesFilter,
  matchesQuery,
  parseQuery,
  suggest,
  weakCache,
  type Facet,
  type Query,
  type QuerySpec,
  type Suggestions,
} from './query'

// notificationSpec is what the page's filter understands, on either tab: the repository,
// the type (as the tab's Type control names it), whether it is read, and its item's state,
// and, on the GitHub tab, GitHub's reason. A notification can be in several states at
// once (unread, and about a merged pull request), so repeated is: filters must all match.
export const notificationSpec: QuerySpec = { keys: ['repo', 'type', 'is', 'reason'], multi: ['is'] }

// notificationHint says what words match, at the foot of the field's suggestions.
export const notificationHint = 'Matches titles, repos, #numbers, and what happened · -word excludes'

// Entry is one notification as the filter reads it, on either tab.
interface Entry {
  // hay is what its words match, folded.
  hay: string
  // number is its item's number, if it has one.
  number: number | undefined
  // repo, type, is, and reason are its values for each key, folded, and shown the same
  // values as the suggestions show them.
  values: Readonly<Record<string, readonly string[]>>
  shown: Readonly<Record<string, readonly string[]>>
}

// haveOf is a notification's values for a key, folded.
function haveOf(e: Entry, key: string): readonly string[] {
  return e.values[key] ?? []
}

// folded is a notification's values, each shown and folded.
function folded(shown: Readonly<Record<string, readonly string[]>>): Entry['values'] {
  return Object.fromEntries(Object.entries(shown).map(([key, values]) => [key, values.map(fold)]))
}

// appEntryOf reads one entry of the app's history for matching. It is cached per entry,
// which the history keeps from one read to the next while it is unchanged.
const appEntryOf = weakCache((n: AppNotification): Entry => {
  const type = appTypeOf(n)
  const shown = {
    repo: n.repo === '' ? [] : [n.repo],
    type: type === '' ? [] : [type],
    is: [n.read ? 'read' : 'unread'],
    reason: [],
  }

  return {
    hay: haystack([
      n.title,
      n.repo,
      n.itemKind === '' ? '' : `#${String(n.number)}`,
      kindTitle(n.kind),
      n.heading,
      type === '' ? '' : appTypes[type].label,
      n.detail,
    ]),
    number: n.itemKind === '' ? undefined : n.number,
    values: folded(shown),
    shown,
  }
})

// stateValue is the is: value of a GitHub notification's item's state, or '' for none.
function stateValue(t: GitHubThread): string {
  switch (t.state) {
    case 'OPEN':
      return 'open'
    case 'MERGED':
      return 'merged'
    case 'CLOSED':
      return 'closed'
    default:
      return ''
  }
}

// threadEntryOf reads one of the user's GitHub notifications for matching.
const threadEntryOf = weakCache((t: GitHubThread): Entry => {
  const type = githubTypeOf(t)
  const repo = t.owner === '' || t.name === '' ? '' : `${t.owner}/${t.name}`
  const state = stateValue(t)
  const shown = {
    repo: repo === '' ? [] : [repo],
    type: [type],
    is: [t.unread ? 'unread' : 'read', ...(state === '' ? [] : [state])],
    reason: t.reason === '' ? [] : [t.reason],
  }
  const item = threadItem(t)

  return {
    hay: haystack([
      t.title,
      repo,
      item === null ? '' : `#${String(item.number)}`,
      reasonText(t.reason),
      githubTypes[type].label,
      t.subjectType,
    ]),
    number: item?.number,
    values: folded(shown),
    shown,
  }
})

// knownStates are the is: values, all known, so is:read is never a part of unread.
const knownStates: ReadonlySet<string> = new Set(['unread', 'read', 'open', 'merged', 'closed'])

// known is the values each key knows among entries, which a filter value that names one
// matches exactly.
function knownOf(entries: readonly Entry[]): (key: string) => ReadonlySet<string> {
  const sets = new Map<string, Set<string>>()

  for (const key of ['repo', 'type', 'reason']) {
    sets.set(key, new Set(entries.flatMap((e) => haveOf(e, key))))
  }

  return (key) => (key === 'is' ? knownStates : (sets.get(key) ?? new Set()))
}

// matchesEntry reports whether a notification matches a query.
function matchesEntry(e: Entry, query: Query, known: (key: string) => ReadonlySet<string>): boolean {
  return matchesQuery(query, e.hay, e.number, (key) => haveOf(e, key), known)
}

// filterBy keeps the items a query matches, in their order, or returns null for a query
// that filters nothing.
function filterBy<T>(items: readonly T[], query: string, entryOf: (item: T) => Entry): T[] | null {
  const parsed = parseQuery(query, notificationSpec)

  if (isEmpty(parsed)) {
    return null
  }

  const entries = items.map(entryOf)
  const known = knownOf(entries)

  return items.filter((_, i) => {
    const e = entries[i]

    return e !== undefined && matchesEntry(e, parsed, known)
  })
}

// filterAppNotifications keeps the entries of the app's history a query matches, in their
// order, or returns null for a query that filters nothing.
export function filterAppNotifications(items: readonly AppNotification[], query: string): AppNotification[] | null {
  return filterBy(items, query, appEntryOf)
}

// GitHubFilter is what a query keeps of the user's GitHub notifications: the lines to
// show, each every kept notification about one item.
export interface GitHubFilter {
  groups: ThreadGroup[]
  matches: number
}

// filterGitHubThreads keeps the notifications a query matches and groups them by item, or
// returns null for a query that filters nothing. A line counts once however many of its
// notifications match.
export function filterGitHubThreads(threads: readonly GitHubThread[], query: string): GitHubFilter | null {
  const kept = filterBy(threads, query, threadEntryOf)

  if (kept === null) {
    return null
  }

  const groups = threadGroups(kept)

  return { groups, matches: groups.length }
}

// reposNamed returns the repositories among repos ("owner/name") that a query's repo:
// filters keep, as the GitHub tab's list keeps them, or null when it names none: a bulk
// mark of the tab starts with those ticked, as GitHub's own per-repository Mark as done
// does once the inbox is filtered to one.
export function reposNamed(query: string, repos: readonly string[]): ReadonlySet<string> | null {
  const filters = parseQuery(query, notificationSpec).filters.filter((f) => f.key === 'repo')

  if (filters.length === 0) {
    return null
  }

  const known = new Set(repos.map(fold))

  return new Set(repos.filter((repo) => filters.every((f) => matchesFilter(f, [fold(repo)], known))))
}

// facets are what the field offers on an empty query, on each tab.
const appFacets: readonly Facet[] = [
  { label: 'Unread', key: 'is', value: 'unread', optional: true },
  { label: 'Type', key: 'type', noun: ['type', 'types'] },
  { label: 'Repository', key: 'repo', noun: ['repo', 'repos'] },
]

const githubFacets: readonly Facet[] = [
  { label: 'Unread', key: 'is', value: 'unread', optional: true },
  { label: 'Merged', key: 'is', value: 'merged', optional: true },
  { label: 'Closed', key: 'is', value: 'closed', optional: true },
  { label: 'Type', key: 'type', noun: ['type', 'types'] },
  { label: 'Repository', key: 'repo', noun: ['repo', 'repos'] },
  { label: 'Reason', key: 'reason', noun: ['reason', 'reasons'] },
]

// nouns name each key's values, for the suggestions list.
const nouns: Readonly<Record<string, string>> = {
  repo: 'Repositories',
  type: 'Types',
  is: 'States',
  reason: 'Reasons',
}

// suggestFor offers what fits where the caret is in a query among entries.
function suggestFor(entries: readonly Entry[], src: string, caret: number, facets: readonly Facet[]): Suggestions {
  const known = knownOf(entries)

  return suggest(src, caret, {
    spec: notificationSpec,
    items: entries,
    matches: (e, query) => matchesEntry(e, query, known),
    values: (e, key) => e.shown[key] ?? [],
    facets,
    hint: notificationHint,
    nouns,
  })
}

// appNotificationSuggestions offers what fits where the caret is in a query on the app
// tab: the unread, the types, and the repositories on an empty query, or the values of
// the key being typed, each counted among the entries the rest of the query keeps.
export function appNotificationSuggestions(items: readonly AppNotification[], src: string, caret: number): Suggestions {
  return suggestFor(items.map(appEntryOf), src, caret, appFacets)
}

// githubNotificationSuggestions offers the same on the GitHub tab, with its items' states
// and GitHub's reasons besides, counted among the notifications.
export function githubNotificationSuggestions(
  threads: readonly GitHubThread[],
  src: string,
  caret: number,
): Suggestions {
  return suggestFor(threads.map(threadEntryOf), src, caret, githubFacets)
}
