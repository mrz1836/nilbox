// Filtering the inbox by the toolbar's query (see lib/query.ts). The whole inbox is in the
// window, so a query matches in memory, at once, without asking GitHub anything.

import type { InboxItem, ListKey, Reviews } from '../platform'
import {
  fold,
  haystack,
  isEmpty,
  markTerms,
  matchesFilter,
  matchesTerms,
  parseQuery,
  suggest,
  weakCache,
  type Facet,
  type Query,
  type QuerySpec,
  type Suggestions,
} from './query'
import { checksSignal, reviewsSignal } from './signals'
import { statusKey } from './statusOrder'
import { isPinned, type TriagedInbox, type TriageTab } from './triage'

// inboxSpec is what the inbox's filter understands. An item is a pull request or an issue,
// in one repository by one author, with one state of its checks and its reviews; it can
// have several flags (is:), labels, projects, and project statuses.
export const inboxSpec: QuerySpec = {
  keys: ['is', 'repo', 'org', 'author', 'label', 'status', 'project', 'checks', 'review'],
  multi: ['is', 'label', 'status', 'project'],
}

// inboxHint is the line under the filters the field offers: what words match.
export const inboxHint = 'Matches titles, repos, #numbers, branches, authors, labels · -word excludes'

// fixedValues are the values of the keys whose every value is known ahead, which a filter
// names exactly. is:open matches every item, as the inbox holds only open ones.
const fixedValues: Readonly<Record<string, ReadonlySet<string>>> = {
  is: new Set(['pr', 'issue', 'draft', 'pinned', 'conflict', 'open']),
  checks: new Set(['failing', 'passing', 'pending']),
  review: new Set(['approved', 'changes_requested', 'required']),
}

// checkValues and reviewValues name a card's checks and reviews as the filter does, so a
// filter agrees with what the card shows (lib/signals).
const checkValues: Readonly<Record<string, string>> = { failed: 'failing', passed: 'passing', pending: 'pending' }

// reviewValue names a pull request's reviews as review: does: approved, changes requested,
// or waiting on a review the repository requires.
function reviewValue(reviews: Reviews): string[] {
  const { kind } = reviewsSignal(reviews)

  if (kind === 'approved') {
    return ['approved']
  }

  if (kind === 'changes') {
    return ['changes_requested']
  }

  return reviews.decision === 'REVIEW_REQUIRED' ? ['required'] : []
}

// flags are what is: says about an item, but for open, which every item is.
function flags(item: InboxItem): string[] {
  return [
    item.kind,
    ...(item.isDraft ? ['draft'] : []),
    ...(isPinned(item) ? ['pinned'] : []),
    ...(item.details?.mergeable === 'CONFLICTING' ? ['conflict'] : []),
  ]
}

// valuesOf lists an item's values for a key as they read on its card, or null when the
// item can't tell yet: its details arrive with the next refresh.
function valuesOf(item: InboxItem, key: string): string[] | null {
  const details = item.details

  switch (key) {
    case 'is':
      return flags(item)
    case 'repo':
      return [item.repo]
    case 'org':
      return [item.repo.split('/', 1).join('')]
    case 'author':
      return [item.author]
    default:
      break
  }

  if (details === null) {
    return null
  }

  switch (key) {
    case 'label':
      return details.labels.map((l) => l.name)
    case 'status':
      return details.projects.map((p) => p.status.trim()).filter((s) => s !== '')
    case 'project':
      return details.projects.map((p) => p.title)
    case 'checks': {
      const value = item.kind === 'pr' ? checkValues[checksSignal(details.checks).kind] : undefined

      return value === undefined ? [] : [value]
    }
    default:
      return item.kind === 'pr' ? reviewValue(details.reviews) : []
  }
}

// suggestedValues are an item's values the field offers for a key: none while its
// details are still to come.
function suggestedValues(item: InboxItem, key: string): string[] {
  return valuesOf(item, key) ?? []
}

// Facts are what the filter reads of an item, folded once for as long as the item lives:
// the text its words match, and its values for every key.
interface Facts {
  hay: string
  values: ReadonlyMap<string, readonly string[] | null>
}

const factsOf = weakCache((item: InboxItem): Facts => {
  const details = item.details
  const values = new Map<string, string[] | null>()

  for (const key of inboxSpec.keys) {
    const found = valuesOf(item, key)

    values.set(key, found?.map((v) => fold(key === 'status' ? statusKey(v) : v)) ?? null)
  }

  // Every item in the inbox is open.
  values.set('is', [...flags(item), 'open'])

  return {
    hay: haystack([
      item.title,
      item.repo,
      `#${String(item.number)}`,
      item.author,
      item.branch,
      ...(details?.labels.map((l) => l.name) ?? []),
      ...(details?.projects.flatMap((p) => [p.title, p.status]) ?? []),
      ...item.reviewTeams,
    ]),
    values,
  }
})

// Known holds, for each key, the values some item has, which a filter names exactly.
export type Known = ReadonlyMap<string, ReadonlySet<string>>

// knownValues gathers the values the items have for every key.
export function knownValues(items: readonly InboxItem[]): Known {
  const known = new Map<string, Set<string>>()

  for (const key of inboxSpec.keys) {
    const fixed = fixedValues[key]
    const set = new Set(fixed)

    if (fixed === undefined) {
      for (const item of items) {
        for (const value of factsOf(item).values.get(key) ?? []) {
          set.add(value)
        }
      }
    }

    known.set(key, set)
  }

  return known
}

const noValues: ReadonlySet<string> = new Set()

// withViewer reads author:@me as the signed-in account. Before the inbox knows who that
// is, @me matches no one.
function withViewer(query: Query, viewer: string): Query {
  if (viewer === '' || !query.filters.some((f) => f.key === 'author' && f.values.includes('@me'))) {
    return query
  }

  const me = fold(viewer)

  return {
    ...query,
    filters: query.filters.map((f) =>
      f.key === 'author' ? { ...f, values: f.values.map((v) => (v === '@me' ? me : v)) } : f,
    ),
  }
}

// matchesItem reports whether an item matches every part of a query: each word somewhere
// in what the card says, and each filter by its key.
export function matchesItem(item: InboxItem, query: Query, known: Known): boolean {
  const facts = factsOf(item)

  return (
    matchesTerms(query.terms, facts.hay, item.number) &&
    query.filters.every((f) => matchesFilter(f, facts.values.get(f.key) ?? null, known.get(f.key) ?? noValues))
  )
}

// inboxItems lists every item in the inbox once, from every list and tab.
export function inboxItems(triaged: TriagedInbox): InboxItem[] {
  const seen = new Map<number, InboxItem>()

  for (const list of Object.values(triaged.lists)) {
    for (const item of [...list.inbox, ...list.snoozed, ...list.dismissed]) {
      if (!seen.has(item.id)) {
        seen.set(item.id, item)
      }
    }
  }

  return [...seen.values()]
}

// FilteredList is the items of one list's tabs that match, each tab in its own order.
export type FilteredList = Readonly<Record<TriageTab, readonly InboxItem[]>>

// InboxFilter is the inbox as a query narrows it.
export interface InboxFilter {
  // query is the query as typed.
  query: string
  lists: Readonly<Record<ListKey, FilteredList>>
  // matches counts the items that match in every list and tab, each once.
  matches: number
  // marks is what each matching card highlights in its title (markTerms).
  marks: string
}

// filterInbox narrows every list and tab of the inbox to the items that match a query,
// keeping their order, or returns null when the query filters nothing. viewer is the
// signed-in login, which author:@me names.
export function filterInbox(triaged: TriagedInbox, src: string, viewer: string): InboxFilter | null {
  const query = withViewer(parseQuery(src, inboxSpec), viewer)

  if (isEmpty(query)) {
    return null
  }

  const known = knownValues(inboxItems(triaged))
  const matched = new Set<number>()
  const keep = (items: readonly InboxItem[]) =>
    items.filter((item) => {
      const hit = matchesItem(item, query, known)

      if (hit) {
        matched.add(item.id)
      }

      return hit
    })
  const narrow = (key: ListKey): FilteredList => {
    const list = triaged.lists[key]

    return { inbox: keep(list.inbox), snoozed: keep(list.snoozed), dismissed: keep(list.dismissed) }
  }
  const lists = {
    reviewRequested: narrow('reviewRequested'),
    authored: narrow('authored'),
    assigned: narrow('assigned'),
  }

  return { query: src, lists, matches: matched.size, marks: markTerms(query) }
}

// inboxFacets are the ways to narrow the inbox the field offers on an empty query.
const inboxFacets: readonly Facet[] = [
  { label: 'Pull requests', key: 'is', value: 'pr' },
  { label: 'Issues', key: 'is', value: 'issue' },
  { label: 'Drafts', key: 'is', value: 'draft' },
  { label: 'Pinned', key: 'is', value: 'pinned' },
  { label: 'Repository', key: 'repo', noun: ['repo', 'repos'] },
  { label: 'Label', key: 'label', noun: ['label', 'labels'] },
  { label: 'Author', key: 'author', noun: ['author', 'authors'] },
  { label: 'Project status', key: 'status', noun: ['status', 'statuses'], optional: true },
  { label: 'Failing checks', key: 'checks', value: 'failing', optional: true },
]

// keyNouns name each key's values, for the list of them.
const keyNouns: Readonly<Record<string, string>> = {
  is: 'Kinds',
  repo: 'Repositories',
  org: 'Organizations',
  author: 'Authors',
  label: 'Labels',
  status: 'Project statuses',
  project: 'Projects',
  checks: 'Checks',
  review: 'Reviews',
}

// inboxSuggestions offers what fits where the caret is in an inbox query, each choice
// counted among the items the rest of the query matches.
export function inboxSuggestions(items: readonly InboxItem[], src: string, caret: number, viewer: string): Suggestions {
  const known = knownValues(items)

  return suggest(src, caret, {
    spec: inboxSpec,
    items,
    matches: (item, query) => matchesItem(item, withViewer(query, viewer), known),
    values: suggestedValues,
    facets: inboxFacets,
    hint: inboxHint,
    nouns: keyNouns,
  })
}
