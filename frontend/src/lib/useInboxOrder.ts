import { useState } from 'react'

import { getInboxOrder, setStatusOrder, setStatusSort, type InboxOrder, type ListKey } from '../platform'
import { serial } from './call'
import { useQuery } from './useQuery'

// InboxOrderState is how the inbox orders its lists, as the user last left it.
export interface InboxOrderState {
  // order is which lists are sorted by project status, and the user's order of the
  // statuses. It is null until the stored order loads, so a list never shows in one
  // order and then jumps to another.
  order: InboxOrder | null
  // setStatusSort sorts a list by project status, or by newest activity again.
  setStatusSort: (list: ListKey, on: boolean) => void
  // setStatusOrder stores the user's order of status names, first ranked highest; an
  // empty one puts every status back where its name places it.
  setStatusOrder: (order: readonly string[]) => void
}

// newestFirst is every list by newest activity, as a stored order that cannot be read
// shows.
const newestFirst: InboxOrder = { statusSorted: [], statusOrder: [] }

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
    setStatusOrder: (order) => {
      change(
        (was) => ({ ...was, statusOrder: [...order] }),
        () => setStatusOrder([...order]),
        'the status order',
      )
    },
  }
}
