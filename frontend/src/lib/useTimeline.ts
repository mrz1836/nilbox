// useTimeline follows a sheet's conversation through one visit to an issue or a pull
// request: the time the visit starts from, which tells what is new, and the pages before
// the read's own, read when the user asks for them and joined to it.

import { useLayoutEffect, useRef, useState } from 'react'

import type { TimelineEntry, TimelinePage } from '../platform'
import { attempt } from './call'

// Conversing is what a sheet reads of an item that has a conversation: its inbox ID, the
// time of the read the user last opened it on, and its conversation's newest page.
export interface Conversing {
  id: number
  lastSeenAt: string
  timeline: TimelinePage
}

export interface TimelineState {
  // timeline is the read's conversation joined to the earlier pages read since, or null
  // until the first read answers.
  timeline: TimelinePage | null
  // lastSeenAt is GitHub's time of the read the user last opened the item on, what is newer
  // being new to them: the visit's first answer's, kept for the visit, since the sheet asks
  // Open again whenever the page shows again. '' on a first visit.
  lastSeenAt: string
  // loadEarlier reads the page of the conversation before the entries the sheet holds;
  // loadingEarlier reports one under way, and earlierError why the last failed.
  loadEarlier: () => Promise<void>
  loadingEarlier: boolean
  earlierError: string
}

// Earlier is the conversation's earlier pages, read since: anchor is the oldest entry of
// the read they were read before, items the entries before it, oldest first, and before
// the cursor that reads further back.
interface Earlier {
  id: number
  anchor: string
  items: readonly TimelineEntry[]
  before: string
}

// Visit is the user's visit to one item: the time it starts from.
interface Visit {
  id: number
  lastSeenAt: string
}

// EarlierRead is a read of earlier entries under way for one item, or why the last failed.
interface EarlierRead {
  id: number
  loading: boolean
  error: string
}

// joined is a read's page after the earlier pages that run up to it, with the cursor that
// reads further back: the pages run up to the read while its oldest entry is their anchor,
// or one of theirs, once newer entries pushed the read's page on. Pages that don't, the
// read having moved past them, are left out.
export function joined(page: TimelinePage, earlier: Earlier | null): TimelinePage {
  const oldest = page.items.at(0)?.id

  if (earlier === null || oldest === undefined) {
    return page
  }

  if (oldest === earlier.anchor) {
    return { ...page, items: [...earlier.items, ...page.items], before: earlier.before }
  }

  const at = earlier.items.findIndex((it) => it.id === oldest)

  return at < 0 ? page : { ...page, items: [...earlier.items.slice(0, at), ...page.items], before: earlier.before }
}

// useTimeline follows the conversation of the item with id: base is the latest answer about
// it, and first the visit's first, or null and undefined until one comes. readEarlier reads
// the page before a cursor. An answer about another item, from before the sheet stepped to
// this one, is never shown.
export function useTimeline(
  id: number,
  base: Conversing | null,
  first: Conversing | undefined,
  readEarlier: (id: number, before: string) => Promise<TimelinePage>,
): TimelineState {
  const [visit, setVisit] = useState<Visit | null>(null)
  const [earlier, setEarlier] = useState<Earlier | null>(null)
  const [read, setRead] = useState<EarlierRead>({ id, loading: false, error: '' })
  // shown is the item the sheet shows now, for a read of earlier entries that answers once
  // the sheet has stepped on, which it drops.
  const shown = useRef(id)

  useLayoutEffect(() => {
    shown.current = id
  }, [id])

  if (first !== undefined && first.id === id && visit?.id !== id) {
    setVisit({ id, lastSeenAt: first.lastSeenAt })
  }

  const mine = base?.id === id ? base : null
  const pages = earlier?.id === id ? earlier : null
  const timeline = mine === null ? null : joined(mine.timeline, pages)
  const earlierRead = read.id === id ? read : null

  return {
    timeline,
    lastSeenAt: visit?.id === id ? visit.lastSeenAt : first?.id === id ? first.lastSeenAt : '',
    loadingEarlier: earlierRead?.loading ?? false,
    earlierError: earlierRead?.error ?? '',
    loadEarlier: async () => {
      if (mine === null || timeline === null || timeline.before === '') {
        return
      }

      // The pages read so far, while they still run up to the read; else a fresh start
      // before the read's own page.
      const kept = timeline.before === mine.timeline.before ? null : pages
      const anchor = kept?.anchor ?? mine.timeline.items.at(0)?.id ?? ''
      const held = kept?.items ?? []

      setRead({ id, loading: true, error: '' })

      const result = await attempt(readEarlier(id, timeline.before))

      if (shown.current !== id) {
        return
      }

      setRead({ id, loading: false, error: result.ok ? '' : result.error })

      if (result.ok) {
        setEarlier({ id, anchor, items: [...result.data.items, ...held], before: result.data.before })
      }
    },
  }
}
