import { startTransition, useEffect, useEffectEvent, useOptimistic, useRef, useState } from 'react'

import {
  dismissItem,
  refreshItem as refreshInboxItem,
  getInboxSnapshot,
  on,
  pinItem,
  refreshInbox,
  restoreItem,
  snoozeItem,
  unpinItem,
  type InboxSnapshot,
} from '../platform'
import { attempt, type Result } from './call'
import { shareSnapshot } from './share'
import { applyTriage, snoozeEnd, type TriageChange } from './triage'
import { useMutation } from './useMutation'
import { useClock } from './useNow'
import { useQuery } from './useQuery'

// InboxState is the stored inbox and what the window is doing with it.
export interface InboxState {
  // snapshot is null until the first load finishes. A triage action under way shows in
  // it at once. Each answer keeps the objects of the one before wherever nothing changed
  // (shareSnapshot), so only the rows whose items changed render again.
  snapshot: InboxSnapshot | null
  loadError: string
  refreshError: string
  refreshing: boolean
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

// showTriage applies a triage change still under way to the inbox shown.
function showTriage(snapshot: InboxSnapshot | null, change: TriageChange): InboxSnapshot | null {
  return snapshot === null ? null : applyTriage(snapshot, change)
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

  const loaded = useQuery(() => ask(next(), getInboxSnapshot()), [])
  const reloadOnUpdate = useEffectEvent(() => {
    void loaded.refetch()
  })

  useEffect(
    () =>
      on('inbox:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  const shown = newer(loaded.data, answered)
  const [snapshot, showChange] = useOptimistic(shown?.snapshot ?? null, showTriage)
  const refresher = useMutation((n: number) => record(n, refreshInbox()))
  const lastLoad = loaded.data?.n ?? 0

  const refresh = () => {
    const n = next()

    setRefreshFrom(n)
    void refresher.run(n)
  }

  // triage runs a triage call, showing change at once while it is under way. It runs as
  // a transition, which holds the change until the answer that replaces it is shown.
  // Transitions are kept to quick, local writes such as these: while any async transition
  // is under way, React holds back the updates of every other.
  const triage = (change: TriageChange, call: () => PromiseLike<InboxSnapshot>) =>
    new Promise<Result<InboxSnapshot>>((resolve) => {
      startTransition(async () => {
        showChange(change)

        const result = await attempt(ask(next(), call()))

        if (result.ok) {
          startTransition(() => {
            show(result.data)
          })
        }

        resolve(result.ok ? { ok: true, data: result.data.snapshot } : result)
      })
    })

  const stamp = () => clock.read().toISOString()

  return {
    snapshot,
    loadError: loaded.error,
    refreshError: lastLoad > refreshFrom ? '' : refresher.error,
    refreshing: refresher.pending,
    refresh,
    reload: () => {
      void loaded.refetch()
    },
    dismiss: (id) => triage({ id, set: { dismissedAt: stamp(), snoozedUntil: '' } }, () => dismissItem(id)),
    snooze: (id, days) =>
      triage({ id, set: { snoozedUntil: snoozeEnd(clock.read(), days).toISOString(), dismissedAt: '' } }, () =>
        snoozeItem(id, days),
      ),
    restore: (id) => triage({ id, set: { dismissedAt: '', snoozedUntil: '', wokeBy: null } }, () => restoreItem(id)),
    pin: (id) => triage({ id, set: { pinnedAt: stamp() } }, () => pinItem(id)),
    unpin: (id) => triage({ id, set: { pinnedAt: '' } }, () => unpinItem(id)),
    refreshItem: (id) => attempt(record(next(), refreshInboxItem(id))),
  }
}
