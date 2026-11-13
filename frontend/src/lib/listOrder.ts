// The order a list's cards show in, which the sheets step through as the list shows them.

import type { InboxItem, InboxOrder, ListKey, ListKind } from '../platform'
import type { FilteredList } from './inboxFilter'
import { sortByStatus, statusesOf, statusOrder } from './statusOrder'
import type { TriagedList, TriageTab } from './triage'

// ShownOrder is how a list orders its own tab, by newest activity or by project status in
// the user's order of status names (see statusOrder), and which kind of item it shows
// there: every one, or only its pull requests or its issues.
export interface ShownOrder {
  byStatus: boolean
  stored: readonly string[]
  kind: ListKind
}

// shownOrder is how the inbox's order sorts and narrows the list key, or every item by
// newest activity while the stored order loads.
export function shownOrder(order: InboxOrder | null, key: ListKey): ShownOrder {
  return {
    byStatus: order?.statusSorted.includes(key) ?? false,
    stored: order?.statusOrder ?? [],
    kind: order?.kinds[key] ?? 'all',
  }
}

// ofKind is the items of the kind a list shows, every one for 'all'.
export function ofKind(items: readonly InboxItem[], kind: ListKind): readonly InboxItem[] {
  return kind === 'all' ? items : items.filter((i) => i.kind === kind)
}

// statusRanking is the order a list's own tab sorts its statuses in: the user's stored
// order, with each status of the list's items they haven't placed placed by its name.
// The statuses are the whole list's, matched by a filter or not.
export function statusRanking(list: TriagedList, stored: readonly string[]): string[] {
  return statusOrder(
    stored,
    statusesOf(list.inbox).map((s) => s.name),
  )
}

// shownItems is a list tab's items as its section shows them: the filter's matches while
// one is on, and on the list's own tab only the kind it shows (ofKind), ordered by project
// status (statusRanking) while it is sorted so.
export function shownItems(
  list: TriagedList,
  tab: TriageTab,
  matched: FilteredList | undefined,
  order: ShownOrder,
): readonly InboxItem[] {
  const shown = matched ?? list

  if (tab !== 'inbox') {
    return shown[tab]
  }

  const items = ofKind(shown.inbox, order.kind)

  return order.byStatus ? sortByStatus(items, statusRanking(list, order.stored)) : items
}

// keptInPlace returns shown, the cards a sheet steps through, holding the card with id,
// the one the sheet shows, should it be missing: a refresh can change the card so that the
// filter no longer matches it while the sheet is open on it. The card goes back where all,
// the tab's cards in the same order before any filter, places it, so stepping from it still
// reaches the cards beside it on screen. A card the tab no longer holds stays out.
export function keptInPlace(shown: readonly InboxItem[], all: readonly InboxItem[], id: number): readonly InboxItem[] {
  const at = all.findIndex((i) => i.id === id)
  const card = all[at]

  if (card === undefined || shown.some((i) => i.id === id)) {
    return shown
  }

  // shown keeps all's order, so the card goes before the first shown card after it.
  const before = new Set(all.slice(0, at).map((i) => i.id))
  const after = shown.findIndex((i) => !before.has(i.id))
  const place = after < 0 ? shown.length : after

  return [...shown.slice(0, place), card, ...shown.slice(place)]
}

// sheetCards are the cards a sheet opened from a list's tab steps through: the tab's cards
// as the list shows them (shownItems), with the card with id, the one the sheet shows,
// kept in its place (keptInPlace) among every card of the tab, of either kind, so a card
// the list's kind leaves out, as one a notification opened, still has its neighbours.
export function sheetCards(
  list: TriagedList,
  tab: TriageTab,
  matched: FilteredList | undefined,
  order: ShownOrder,
  id: number,
): readonly InboxItem[] {
  return keptInPlace(
    shownItems(list, tab, matched, order),
    shownItems(list, tab, undefined, { ...order, kind: 'all' }),
    id,
  )
}
