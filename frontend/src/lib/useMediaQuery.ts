import { useSyncExternalStore } from 'react'

// MediaStore is one media query of one window as an external store.
interface MediaStore {
  subscribe: (listener: () => void) => () => void
  getSnapshot: () => boolean
}

// stores keeps one store per window and query, so every component asking the same
// question shares it, and a store keeps its identity across renders.
const stores = new WeakMap<Window, Map<string, MediaStore>>()

// storeFor returns the store for a query in a window. Without matchMedia the query never
// matches and never changes.
function storeFor(win: Window, query: string): MediaStore {
  let byQuery = stores.get(win)

  if (byQuery === undefined) {
    byQuery = new Map()
    stores.set(win, byQuery)
  }

  let store = byQuery.get(query)

  if (store === undefined) {
    if (typeof win.matchMedia !== 'function') {
      store = { subscribe: () => () => undefined, getSnapshot: () => false }
    } else {
      const list = win.matchMedia(query)

      store = {
        subscribe: (listener) => {
          list.addEventListener('change', listener)

          return () => {
            list.removeEventListener('change', listener)
          }
        },
        getSnapshot: () => list.matches,
      }
    }

    byQuery.set(query, store)
  }

  return store
}

// useMediaQuery reports whether a media query matches, such as
// "(prefers-color-scheme: dark)", and renders again whenever that changes.
export function useMediaQuery(query: string, win: Window = window): boolean {
  const store = storeFor(win, query)

  return useSyncExternalStore(store.subscribe, store.getSnapshot)
}
