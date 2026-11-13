// useIssue loads the issue sheet's issue: at once from what the sheet read before (Open,
// which asks GitHub nothing), then from GitHub (Refresh), and the conversation's earlier
// pages when the user asks for them.

import { useLayoutEffect, useRef, useState } from 'react'

import {
  issueEarlier,
  openIssue,
  readIssueAgain,
  refreshIssue,
  type Issue,
  type IssueTimelineItem,
  type IssueTimelinePage,
} from '../platform'
import { attempt } from './call'
import { useQuery } from './useQuery'

export interface IssueState {
  // issue is the issue as last read, its conversation joined to the earlier pages read
  // since, or null until the first read answers.
  issue: Issue | null
  // checking reports a read from GitHub under way.
  checking: boolean
  // error is why the last read failed, or ''.
  error: string
  // lastSeenAt is GitHub's time of the read the user last opened the issue on, what is
  // newer being new to them: the visit's first answer's, kept for the visit, since the
  // sheet asks Open again whenever the page shows again. '' on a first visit.
  lastSeenAt: string
  // reload reads the issue from GitHub again, however recently it was read: the user
  // asked for it.
  reload: () => void
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
  items: readonly IssueTimelineItem[]
  before: string
}

// Visit is the user's visit to one issue: the time it starts from.
interface Visit {
  id: number
  lastSeenAt: string
}

// Round is the reads asked for after the first, for one issue: n counts them, and force
// marks the latest as one the user asked for, which reads GitHub however recently the issue
// was read. Stepping to another issue starts its rounds afresh, so a reload asked for on one
// issue never forces the reads of the next.
interface Round {
  id: number
  n: number
  force: boolean
}

// EarlierRead is a read of earlier entries under way for one issue, or why the last failed.
interface EarlierRead {
  id: number
  loading: boolean
  error: string
}

// joined is a read's page after the earlier pages that run up to it, with the cursor that
// reads further back: the pages run up to the read while its oldest entry is their anchor,
// or one of theirs, once newer entries pushed the read's page on. Pages that don't, the
// read having moved past them, are left out.
export function joined(page: IssueTimelinePage, earlier: Earlier | null): IssueTimelinePage {
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

// useIssue loads the issue with id. An answer about another issue, from before the sheet
// stepped to this one, is never shown.
export function useIssue(id: number): IssueState {
  const opened = useQuery(() => openIssue(id), [id])
  const [round, setRound] = useState<Round>({ id, n: 0, force: false })

  if (round.id !== id) {
    setRound({ id, n: 0, force: false })
  }

  const force = round.id === id && round.force
  const refreshed = useQuery(() => (force ? readIssueAgain(id) : refreshIssue(id)), [id, round.n])
  const [visit, setVisit] = useState<Visit | null>(null)
  const [earlier, setEarlier] = useState<Earlier | null>(null)
  const [read, setRead] = useState<EarlierRead>({ id, loading: false, error: '' })
  // shown is the issue the sheet shows now, for a read of earlier entries that answers
  // once the sheet has stepped on, which it drops.
  const shown = useRef(id)

  useLayoutEffect(() => {
    shown.current = id
  }, [id])

  const mine = (is: Issue | undefined) => (is?.id === id ? is : undefined)
  const base = mine(refreshed.data) ?? mine(opened.data) ?? null
  const first = mine(opened.data) ?? mine(refreshed.data)

  if (first !== undefined && visit?.id !== id) {
    setVisit({ id, lastSeenAt: first.lastSeenAt })
  }

  const pages = earlier?.id === id ? earlier : null
  const issue = base === null ? null : { ...base, timeline: joined(base.timeline, pages) }

  // A read under way speaks for itself once it answers: until then no error shows, so the
  // issue stepped from never lends the next its failure.
  const readError = refreshed.pending ? '' : refreshed.error
  const openError = opened.pending ? '' : opened.error
  const earlierRead = read.id === id ? read : null

  return {
    issue,
    checking: refreshed.pending,
    error: readError === '' ? openError : readError,
    lastSeenAt: visit?.id === id ? visit.lastSeenAt : (first?.lastSeenAt ?? ''),
    reload: () => {
      setRound((r) => ({ id, n: r.n + 1, force: true }))
    },
    loadingEarlier: earlierRead?.loading ?? false,
    earlierError: earlierRead?.error ?? '',
    loadEarlier: async () => {
      if (base === null || issue === null || issue.timeline.before === '') {
        return
      }

      // The pages read so far, while they still run up to the read; else a fresh start
      // before the read's own page.
      const kept = issue.timeline.before === base.timeline.before ? null : pages
      const anchor = kept?.anchor ?? base.timeline.items.at(0)?.id ?? ''
      const held = kept?.items ?? []

      setRead({ id, loading: true, error: '' })

      const result = await attempt(issueEarlier(id, issue.timeline.before))

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
