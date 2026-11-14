// useIssue loads the issue sheet's issue: at once from what the sheet read before (Open,
// which asks GitHub nothing), then from GitHub (Refresh), and the conversation's earlier
// pages when the user asks for them.

import { useState } from 'react'

import { issueEarlier, openIssue, readIssueAgain, refreshIssue, type Issue } from '../platform'
import { useQuery } from './useQuery'
import { useTimeline } from './useTimeline'

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

// Round is the reads asked for after the first, for one issue: n counts them, and force
// marks the latest as one the user asked for, which reads GitHub however recently the issue
// was read. Stepping to another issue starts its rounds afresh, so a reload asked for on one
// issue never forces the reads of the next.
interface Round {
  id: number
  n: number
  force: boolean
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
  const mine = (is: Issue | undefined) => (is?.id === id ? is : undefined)
  const base = mine(refreshed.data) ?? mine(opened.data) ?? null
  const first = mine(opened.data) ?? mine(refreshed.data)
  const conversation = useTimeline(id, base, first, issueEarlier)
  const issue = base === null || conversation.timeline === null ? null : { ...base, timeline: conversation.timeline }

  // A read under way speaks for itself once it answers: until then no error shows, so the
  // issue stepped from never lends the next its failure.
  const readError = refreshed.pending ? '' : refreshed.error
  const openError = opened.pending ? '' : opened.error

  return {
    issue,
    checking: refreshed.pending,
    error: readError === '' ? openError : readError,
    lastSeenAt: conversation.lastSeenAt,
    reload: () => {
      setRound((r) => ({ id, n: r.n + 1, force: true }))
    },
    loadingEarlier: conversation.loadingEarlier,
    earlierError: conversation.earlierError,
    loadEarlier: conversation.loadEarlier,
  }
}
