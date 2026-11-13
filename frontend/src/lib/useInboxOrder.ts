import { useState } from 'react'

import {
  getInboxOrder,
  setListKind,
  setStatusOrder,
  setStatusSort,
  type InboxOrder,
  type ListKey,
  type ListKind,
} from '../platform'
import { serial } from './call'
import { useQuery } from './useQuery'

// InboxOrderState is how the inbox orders its lists, as the user last left it.
export interface InboxOrderState {
  // order is which lists are sorted by project status, the user's order of the statuses,
  // and which lists show only one kind of item. It is null until the stored order loads, so a list never shows in one
  // order and then jumps to another.
  order: InboxOrder | null
  // setStatusSort sorts a list by project status, or by newest activity again.
  setStatusSort: (list: ListKey, on: boolean) => void
  // setListKind shows only a list's pull requests, or only its issues, or every item again.
  setListKind: (list: ListKey, kind: ListKind) => void
  // setStatusOrder stores the user's order of status names, first ranked highest; an
  // empty one puts every status back where its name places it.
  setStatusOrder: (order: readonly string[]) => void
}

// newestFirst is every list by newest activity, as a stored order that cannot be read
// shows.
const newestFirst: InboxOrder = { statusSorted: [], statusOrder: [], kinds: {} }

// useInboxOrder loads how the inbox orders its lists once, and stores each change. A
// change shows at once and is stored in the order it was made; one that cannot be stored
// still holds for this session, and the failure is only logged, since nothing else
// depends on it.
export function useInboxOrder(): InboxOrderState {
  const stored = useQuery(() => getInboxOrder(), [], { log: 'could not read how the inbox is ordered' })
  const [queue] = useState(serial)
  const loaded = stored.data !== undefined || stored.error !== ''

  const change = (next: (was: InboxOrder) => InboxOrder, write: () => PromiseLike<unknown>, what: string) => {
    stored.set((was) => next(was ?? newestFirst))
    queue(write).catch((err: unknown) => {
      console.error(`could not store ${what}`, err)
    })
  }

  return {
    order: loaded ? (stored.data ?? newestFirst) : null,
    setStatusSort: (list, on) => {
      change(
        (was) => ({
          ...was,
          statusSorted: on
            ? [...was.statusSorted.filter((l) => l !== list), list]
            : was.statusSorted.filter((l) => l !== list),
        }),
        () => setStatusSort(list, on),
        `how the ${list} list is sorted`,
      )
    },
    setListKind: (list, kind) => {
      change(
        (was) => {
          // Every other list keeps its kind; this one is left out for every item.
          const others = Object.entries(was.kinds).filter(([l]) => l !== list)

          return { ...was, kinds: Object.fromEntries(kind === 'all' ? others : [...others, [list, kind]]) }
        },
        () => setListKind(list, kind),
        `which items the ${list} list shows`,
      )
    },
    setStatusOrder: (order) => {
      change(
        (was) => ({ ...was, statusOrder: [...order] }),
        () => setStatusOrder([...order]),
        'the status order',
      )
    },
  }
}
