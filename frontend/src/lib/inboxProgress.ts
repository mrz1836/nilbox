import type { InboxProgress } from '../platform'

// The words for how far a refresh of the inbox has got, as the inbox's toolbar shows them
// while it runs (see useInbox). The backend names the phases (internal/event.InboxPhase);
// this file only words them.

// Step is a refresh's progress in words, with the share of its phase done, from 0 to 1, or
// null for a phase that counts nothing.
export interface Step {
  text: string
  share: number | null
}

// newItems is "1 new item" or "8 new items".
function newItems(n: number): string {
  return `${String(n)} new ${n === 1 ? 'item' : 'items'}`
}

// progressStep words where a refresh stands: what it is doing now, and how far through.
// Before the refresh first says how far it has got, as when the window opens on one under
// way, it is only refreshing.
export function progressStep(progress: InboxProgress | null): Step {
  if (progress === null) {
    return { text: 'Refreshing from GitHub…', share: null }
  }

  const share = progress.total > 0 ? Math.min(progress.done / progress.total, 1) : null

  switch (progress.phase) {
    case 'search':
      return { text: 'Asking GitHub…', share: null }
    case 'details':
      return { text: `Reading ${String(progress.done)} of ${newItems(progress.total)}…`, share }
    case 'files':
      return { text: `Checking files: ${String(progress.done)} of ${String(progress.total)}…`, share }
  }
}
