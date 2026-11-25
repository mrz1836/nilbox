import { startTransition, useOptimistic, useRef, useState } from 'react'

import {
  dismissItem,
  refreshItem as refreshInboxItem,
  getInboxSnapshot,
  markItemsSeen,
  markItemUnseen,
  nudgeAuthor,
  pinItem,
  refreshInbox,
  removeReviewRequest as removeItemReviewRequest,
  restoreItem,
  snoozeItem,
  unassignItem,
  unpinItem,
  type InboxProgress,
  type InboxSnapshot,
  type Nudged,
} from '../platform'
import { attempt, type Result } from './call'
import { shareSnapshot } from './share'
import { applyTriage, snoozeEnd, type TriageChange } from './triage'
import { useMutation } from './useMutation'
import { useClock } from './useNow'
import { useOn } from './useOn'
import { useQuery } from './useQuery'

// InboxState is the stored inbox and what the window is doing with it.
export interface InboxState {
  // snapshot is null until the first load finishes. A triage action under way shows in
  // it at once. Each answer keeps the objects of the one before wherever nothing changed
  // (shareSnapshot), so only the rows whose items changed render again.
  snapshot: InboxSnapshot | null
  loadError: string
  refreshError: string
  // refreshing is set while a whole refresh runs, whoever started it: a click, the timer,
  // the menu bar, or the app's start. The window knows from the snapshot (its refreshing),
  // from its own click, and from the refresh's progress, which tells it one started.
  refreshing: boolean
  // progress is how far the refresh under way has got, or null while none runs, or before
  // it first says.
  progress: InboxProgress | null
  // caughtUp is set once the window has seen no refresh under way: until then, as when it
  // opens on one, what is stored may be about to change.
  caughtUp: boolean
  // refresh fetches the inbox from GitHub now.
  refresh: () => void
  // reload reads the stored inbox again, without asking GitHub, such as after a setting
  // that changes how items are shown.
  reload: () => void
  // dismiss hides an item until it is restored, snooze hides it for a number of days,
  // and restore brings back either. Each shows at once, and resolves with the new inbox
  // once it is stored, or with why it failed, when the item goes back as it was.
  dismiss: (id: number) => Promise<Result<InboxSnapshot>>
  snooze: (id: number, days: number) => Promise<Result<InboxSnapshot>>
  restore: (id: number) => Promise<Result<InboxSnapshot>>
  // pin keeps an item at the top of its lists, and unpin puts it back among the rest,
  // each shown at once like the actions above.
  pin: (id: number) => Promise<Result<InboxSnapshot>>
  unpin: (id: number) => Promise<Result<InboxSnapshot>>
  // refreshItem reads one item from GitHub again, without refreshing the rest, and
  // resolves with the new inbox once it is shown.
  refreshItem: (id: number) => Promise<Result<InboxSnapshot>>
  // unassign takes the user off an item's assignees on GitHub, and removeReviewRequest
  // off a pull request's requested reviewers. Each changes GitHub first, so nothing shows
  // until GitHub has made the change; then each resolves with the new inbox once it is
  // shown, the item gone from that list, or with why it failed.
  unassign: (id: number) => Promise<Result<InboxSnapshot>>
  removeReviewRequest: (id: number) => Promise<Result<InboxSnapshot>>
  // nudge posts body as a comment on a pull request's conversation, on GitHub, asking its
  // author about the changes the user requested. Like the edits above, nothing shows until
  // GitHub has posted it; then it resolves with the new inbox once it is shown, the item
  // saying when it was nudged, and the comment's address, or with why it failed.
  nudge: (id: number, body: string) => Promise<Result<Nudged>>
  // markRead marks items read, and markUnread one unread, on this Mac alone, whatever
  // Settings → Cards' Unread dots says: the window marks what the user saw and did (an
  // item's sheet opening, stepping away, or closing, the item opened on GitHub, a card's
  // edits on GitHub), and a card's menu and peek offer both. Each shows at once, and
  // resolves with the new inbox once it is stored, or with why it failed, when the items
  // go back as they were. A mark tells no one the inbox changed, so each reads it back.
  markRead: (ids: readonly number[]) => Promise<Result<InboxSnapshot>>
  markUnread: (id: number) => Promise<Result<InboxSnapshot>>
  // marks counts the marks stored since the window opened, so a list that reads its items
  // itself (a routine's Watching list) reads them again after one, as it does on the
  // shell's update, which a mark never sends.
  marks: number
}

// Answer is a request's inbox, with the request's number.
interface Answer {
  n: number
  snapshot: InboxSnapshot
}

// newer returns the answer of the later request; either may be missing.
function newer(a: Answer | null | undefined, b: Answer | null | undefined): Answer | null {
  if (a === null || a === undefined) {
    return b ?? null
  }

  return b === null || b === undefined || a.n > b.n ? a : b
}

// showTriage applies the changes of a triage action still under way to the inbox shown.
function showTriage(snapshot: InboxSnapshot | null, changes: readonly TriageChange[]): InboxSnapshot | null {
  return snapshot === null ? null : changes.reduce((shown, change) => applyTriage(shown, change), snapshot)
}

// useInbox loads the stored inbox, loads it again whenever the shell reports a change
// (the background refresh), and refreshes from GitHub when asked. The whole window shares
// one InboxState: the inbox lists, the account menu, and the budget gauge. Each answer
// moves the window's clock (useClock) on to the moment it arrived, so the ages and the
// snoozes it shows are measured from then.
//
// Loads, refreshes, and triage can overlap, and their answers can arrive out of order.
// Each request is numbered as it starts, and the inbox shown is the answer of the latest
// request answered, so an older inbox never replaces a newer one. A load that a newer load
// replaces is cancelled. Every change to the stored inbox is followed by an update from
// the shell, whose load is the latest.
export function useInbox(): InboxState {
  const clock = useClock()
  // started numbers the requests as they start.
  const started = useRef(0)
  // known is the latest inbox answered, whose objects the next answer keeps where its
  // data is the same.
  const known = useRef<InboxSnapshot | null>(null)
  // answered is the latest answer of a refresh or a triage action.
  const [answered, setAnswered] = useState<Answer | null>(null)
  // refreshFrom is the number of the latest refresh: a load numbered after it shows the
  // inbox as stored since, so its failure is no longer news.
  const [refreshFrom, setRefreshFrom] = useState(0)

  // ask numbers a request for the inbox and tags its answer with the number, sharing what
  // it has in common with the inbox answered before.
  const ask = (n: number, call: PromiseLike<InboxSnapshot>): PromiseLike<Answer> =>
    call.then((answer) => {
      const snapshot = shareSnapshot(known.current, answer)

      known.current = snapshot
      clock.read()

      return { n, snapshot }
    })

  const next = () => {
    started.current += 1

    return started.current
  }

  // show shows an answer, unless a later one is shown by then.
  const show = (got: Answer) => {
    setAnswered((was) => newer(was, got))
  }

  // record asks with a request and shows its answer.
  const record = async (n: number, call: PromiseLike<InboxSnapshot>): Promise<InboxSnapshot> => {
    const got = await ask(n, call)

    show(got)

    return got.snapshot
  }

  // nudged posts a nudge and shows the inbox it answers with, numbered as it starts.
  const nudged = async (id: number, body: string): Promise<Nudged> => {
    const n = next()
    const answer = await nudgeAuthor({ itemId: id, body })

    return { ...answer, snapshot: await record(n, Promise.resolve(answer.snapshot)) }
  }

  const loaded = useQuery(() => ask(next(), getInboxSnapshot()), [])

  useOn('inbox:updated', () => {
    void loaded.refetch()
  })

  // heard is the latest progress a refresh told, with the number of the last request
  // started when it came: an answer to a request started after it that says no refresh
  // runs means that refresh ended, while one started before it may not know of it yet.
  const [heard, setHeard] = useState<{ progress: InboxProgress; at: number } | null>(null)

  useOn('inbox:progress', (progress) => {
    setHeard({ progress, at: started.current })
  })

  const shown = newer(loaded.data, answered)
  const [snapshot, showChange] = useOptimistic(shown?.snapshot ?? null, showTriage)
  // A refresh that fails stored why, and whether the inbox tries it again; the inbox is read
  // back before the failure shows, so the window never shows it without them (a banner for
  // a moment, where a countdown to the next try belongs). The failure is news until a load
  // numbered after that read.
  const refresher = useMutation(async (n: number) => {
    try {
      return await record(n, refreshInbox())
    } catch (err) {
      await loaded.refetch()
      setRefreshFrom(next())

      throw err
    }
  })
  const lastLoad = loaded.data?.n ?? 0

  if (heard !== null && shown !== null && shown.n > heard.at && !shown.snapshot.refreshing) {
    setHeard(null)
  }

  const refreshing = (shown?.snapshot.refreshing ?? false) || refresher.pending || heard !== null
  const [caughtUp, setCaughtUp] = useState(false)

  if (!caughtUp && shown !== null && !refreshing) {
    setCaughtUp(true)
  }

  const refresh = () => {
    const n = next()

    setRefreshFrom(n)
    void refresher.run(n)
  }

  // triage runs a triage call, showing changes at once while it is under way. It runs as
  // a transition, which holds the changes until the answer that replaces them is shown.
  // Transitions are kept to quick, local writes such as these: while any async transition
  // is under way, React holds back the updates of every other.
  const triage = (changes: readonly TriageChange[], call: () => PromiseLike<Answer>) =>
    new Promise<Result<InboxSnapshot>>((resolve) => {
      startTransition(async () => {
        showChange(changes)

        const result = await attempt(call())

        if (result.ok) {
          startTransition(() => {
            show(result.data)
          })
        }

        resolve(result.ok ? { ok: true, data: result.data.snapshot } : result)
      })
    })

  // change is a triage call numbered as it starts, which shows change at once.
  const change = (set: TriageChange, call: () => PromiseLike<InboxSnapshot>) => triage([set], () => ask(next(), call()))

  const [marks, setMarks] = useState(0)

  // mark stores a seen mark of the items with ids, showing it at once. A mark tells no one,
  // so the inbox is read back once it is stored, numbered then, so no read asked before
  // the mark shows the items as they were.
  const mark = async (ids: readonly number[], unread: boolean, store: () => PromiseLike<void>) => {
    const changes = ids.map((id) => ({ id, set: { unread } }))
    const result = await triage(changes, async () => {
      await store()

      return ask(next(), getInboxSnapshot())
    })

    if (result.ok) {
      setMarks((n) => n + 1)
    }

    return result
  }

  const stamp = () => clock.read().toISOString()

  return {
    snapshot,
    loadError: loaded.error,
    refreshError: lastLoad > refreshFrom ? '' : refresher.error,
    refreshing,
    progress: refreshing ? (heard?.progress ?? null) : null,
    caughtUp,
    refresh,
    reload: () => {
      void loaded.refetch()
    },
    // The user's own triage takes over the routine's mark it changes, as the backend's
    // does: a dismissal, a snooze, or a restore is the user's, and so is a pin, or none.
    dismiss: (id) =>
      change({ id, set: { dismissedAt: stamp(), snoozedUntil: '', setAsideBy: 0 } }, () => dismissItem(id)),
    snooze: (id, days) =>
      change(
        { id, set: { snoozedUntil: snoozeEnd(clock.read(), days).toISOString(), dismissedAt: '', setAsideBy: 0 } },
        () => snoozeItem(id, days),
      ),
    restore: (id) =>
      change({ id, set: { dismissedAt: '', snoozedUntil: '', wokeBy: null, setAsideBy: 0 } }, () => restoreItem(id)),
    pin: (id) => change({ id, set: { pinnedAt: stamp(), pinnedBy: 0 } }, () => pinItem(id)),
    unpin: (id) => change({ id, set: { pinnedAt: '', pinnedBy: 0 } }, () => unpinItem(id)),
    refreshItem: (id) => attempt(record(next(), refreshInboxItem(id))),
    // An edit waits on GitHub, so it is not a transition: it would hold back every other
    // update while it runs.
    unassign: (id) => attempt(record(next(), unassignItem(id))),
    removeReviewRequest: (id) => attempt(record(next(), removeItemReviewRequest(id))),
    nudge: (id, body) => attempt(nudged(id, body)),
    markRead: (ids) => mark(ids, false, () => markItemsSeen([...ids])),
    markUnread: (id) => mark([id], true, () => markItemUnseen(id)),
    marks,
  }
}
