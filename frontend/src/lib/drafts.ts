// A sheet's drafts: what the user has written for each item, kept for the session, so
// closing the sheet, or stepping to another item and back, loses nothing. A draft is
// cleared once its action succeeds.

import { useState } from 'react'

// Drafts is every item's draft of one kind, by inbox item ID.
export interface Drafts<D> {
  // get returns the item's draft, or the empty one before the user did anything.
  get: (id: number) => D
  update: (id: number, change: (draft: D) => D) => void
  clear: (id: number) => void
}

// useDrafts keeps the drafts for as long as the component that holds them lives: the
// window, for the session. empty is an item's draft before the user does anything.
export function useDrafts<D>(empty: D): Drafts<D> {
  const [drafts, setDrafts] = useState<ReadonlyMap<number, D>>(new Map())

  return {
    get: (id) => drafts.get(id) ?? empty,
    update: (id, change) => {
      setDrafts((was) => new Map(was).set(id, change(was.get(id) ?? empty)))
    },
    clear: (id) => {
      setDrafts((was) => {
        const next = new Map(was)
        next.delete(id)

        return next
      })
    },
  }
}
