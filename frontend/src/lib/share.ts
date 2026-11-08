import type { InboxItem, InboxList, InboxSnapshot, ItemResolution } from '../platform'

// Every answer from the backend arrives as freshly decoded JSON: a new object for every
// item, changed or not. Sharing keeps the objects of the answer before wherever the data
// is the same, so only what changed looks new to the components, and a row whose item
// did not change has nothing to render again.

// same reports whether two values decoded from JSON hold the same data: equal plain
// values, or arrays and objects whose members are all the same.
export function same(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) {
    return true
  }

  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) {
    return false
  }

  if (Array.isArray(a) || Array.isArray(b)) {
    return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((v, i) => same(v, b[i]))
  }

  const left = a as Record<string, unknown>
  const right = b as Record<string, unknown>
  const keys = Object.keys(left)

  return (
    keys.length === Object.keys(right).length && keys.every((k) => Object.hasOwn(right, k) && same(left[k], right[k]))
  )
}

// keep returns was when it holds the same data as next, else next.
function keep<T>(was: T, next: T): T {
  return same(was, next) ? was : next
}

// shareList returns next with the items known from before in their place, or was itself
// when nothing in the list changed.
function shareList(was: InboxList, next: InboxList, known: ReadonlyMap<number, InboxItem>): InboxList {
  const items = next.items.map((item) => {
    const old = known.get(item.id)

    return old !== undefined && same(old, item) ? old : item
  })
  const unchanged =
    was.shown === next.shown &&
    was.total === next.total &&
    was.items.length === items.length &&
    items.every((item, i) => item === was.items[i])

  return unchanged ? was : { ...next, items }
}

// shareSnapshot returns next with every part that holds the same data as in was taken
// from was: each unchanged item, each list whose items and counts are unchanged, the
// account, and the budget reading. An item moved from one list to another keeps its
// object too. With nothing changed at all it returns was.
export function shareSnapshot(was: InboxSnapshot | null, next: InboxSnapshot): InboxSnapshot {
  if (was === null) {
    return next
  }

  const known = new Map<number, InboxItem>()

  for (const list of [was.reviewRequested, was.authored, was.assigned]) {
    for (const item of list.items) {
      known.set(item.id, item)
    }
  }

  const shared: InboxSnapshot = {
    ...next,
    account: keep(was.account, next.account),
    rate: keep(was.rate, next.rate),
    reviewRequested: shareList(was.reviewRequested, next.reviewRequested, known),
    authored: shareList(was.authored, next.authored, known),
    assigned: shareList(was.assigned, next.assigned, known),
  }

  return same(was, shared) ? was : shared
}

// shareResolutions returns the resolutions in next by item ID, each one unchanged since
// was kept as it was, or was itself when none changed and none came or went.
export function shareResolutions(
  was: ReadonlyMap<number, ItemResolution>,
  next: readonly ItemResolution[],
): ReadonlyMap<number, ItemResolution> {
  const shared = new Map<number, ItemResolution>()
  let changed = was.size !== next.length

  for (const resolution of next) {
    const old = was.get(resolution.itemId)

    if (old !== undefined && same(old, resolution)) {
      shared.set(resolution.itemId, old)
    } else {
      shared.set(resolution.itemId, resolution)
      changed = true
    }
  }

  return changed || shared.size !== was.size ? shared : was
}
