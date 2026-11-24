// Sorting an inbox list by a name its items carry, the repository or the author: the
// groups A to Z, the pinned items first, and each group newest first as the list was.

import type { InboxItem } from '../platform'
import { isPinned } from './triage'

// byName compares two names as a reader would: ignoring case and accents, and the numbers
// in them by value, so "repo-2" comes before "repo-10".
const byName = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true })

// sortByName is the items with the pinned ones first, as they were, then the rest A to Z
// by nameOf. The sort is stable, so the items of one name keep the order they were given
// in, newest first. The items it was given are left as they were.
export function sortByName(items: readonly InboxItem[], nameOf: (item: InboxItem) => string): InboxItem[] {
  const pinned = items.filter(isPinned)
  const rest = items.filter((item) => !isPinned(item))

  return [...pinned, ...rest.sort((a, b) => byName.compare(nameOf(a), nameOf(b)))]
}

// sortByRepo groups the items by repository, "owner/name", A to Z.
export function sortByRepo(items: readonly InboxItem[]): InboxItem[] {
  return sortByName(items, (item) => item.repo)
}

// sortByAuthor groups the items by their author's login, A to Z.
export function sortByAuthor(items: readonly InboxItem[]): InboxItem[] {
  return sortByName(items, (item) => item.author)
}
