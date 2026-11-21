import { useEffect, useEffectEvent, useRef, useState } from 'react'

import {
  autopilotRunEvents,
  autopilotRunEventsBefore,
  type AutopilotRunEnding,
  type AutopilotRunEvent,
  type AutopilotRunEvents,
  type AutopilotRunProgress,
} from '../platform'
import { attempt } from './call'
import { appendEvents, joinEvents } from './runView'
import { useOn } from './useOn'

// RunTimeline is a run's timeline as the run view holds it: the events read so far, oldest
// first, how far the run has got, how it came out once it ended, and whether earlier
// events are in its log, which loadEarlier reads a page at a time.
export interface RunTimeline {
  events: readonly AutopilotRunEvent[]
  progress: AutopilotRunProgress
  ending: AutopilotRunEnding | null
  done: boolean
  loaded: boolean
  earlier: boolean
  loadingEarlier: boolean
  error: string
  loadEarlier: () => void
}

// noProgress is a run's progress before its first read.
const noProgress: AutopilotRunProgress = {
  step: '',
  todos: [],
  tokensIn: 0,
  tokensOut: 0,
  cacheRead: 0,
  events: 0,
  unknown: 0,
}

// Held is what the hook keeps of a run's timeline.
interface Held {
  events: AutopilotRunEvent[]
  progress: AutopilotRunProgress
  ending: AutopilotRunEnding | null
  done: boolean
  loaded: boolean
  earlier: boolean
  before: number
}

const empty: Held = {
  events: [],
  progress: noProgress,
  ending: null,
  done: false,
  loaded: false,
  earlier: false,
  before: 0,
}

// lastSeq is the newest event followed from memory, 0 for none: a page read back from the
// log numbers none.
function lastSeq(events: readonly AutopilotRunEvent[]): number {
  return events.reduce((high, e) => Math.max(high, e.seq), 0)
}

// useRunTimeline follows the ledger's run runId's timeline: its last events first, from
// memory while it works or its log's last page once it ended; then what is new each time
// the shell says the run's stream moved on (autopilot:run) or Autopilot changed, until it
// ends; and, on loadEarlier, the page of its log before the first event shown, so a long
// timeline pages back a page at a time and never loads whole.
export function useRunTimeline(runId: number): RunTimeline {
  const [held, setHeld] = useState<Held>(empty)
  const [error, setError] = useState('')
  const [loadingEarlier, setLoadingEarlier] = useState(false)
  // reading is whether a read of what is new is under way; again, that another was asked
  // for meanwhile, which runs once it ends.
  const reading = useRef(false)
  const again = useRef(false)

  // take merges a page read after the last event into what the view holds.
  const take = (page: AutopilotRunEvents) => {
    setHeld((h) => {
      const events = appendEvents(h.events, page.events)
      const first = h.loaded ? h : { ...h, earlier: page.earlier, before: page.before }

      return {
        ...first,
        events,
        progress: page.progress,
        ending: page.ending,
        done: page.done,
        loaded: true,
      }
    })
  }

  // askedAgain reports whether another read was asked for while one was under way.
  const askedAgain = () => again.current

  // readNew reads what is new since the event numbered from, one read at a time: one asked
  // for while another is under way runs once it ends, from the last event that read brought,
  // until the run has ended.
  const readNew = async (from: number) => {
    if (reading.current) {
      again.current = true

      return
    }

    reading.current = true

    let next = from

    do {
      again.current = false

      const page = await attempt(autopilotRunEvents(runId, next))

      if (!page.ok) {
        setError(page.error)

        break
      }

      setError('')
      take(page.data)
      next = Math.max(next, lastSeq(page.data.events))

      if (page.data.done) {
        break
      }
    } while (askedAgain())

    reading.current = false
  }

  // moved reads what is new, while the run goes.
  const moved = () => {
    if (!held.done) {
      void readNew(lastSeq(held.events))
    }
  }

  // readFirst reads the run's last events.
  const readFirst = useEffectEvent(() => {
    void readNew(0)
  })

  // The view is keyed by its run, so a run's timeline starts empty and is read once here,
  // before it follows the run.
  useEffect(() => {
    readFirst()
  }, [runId])

  useOn('autopilot:run', ({ runId: changed }) => {
    if (changed === runId) {
      moved()
    }
  })

  useOn('autopilot:updated', () => {
    moved()
  })

  const loadEarlier = () => {
    const { earlier, before } = held

    if (!earlier || loadingEarlier) {
      return
    }

    setLoadingEarlier(true)

    void attempt(autopilotRunEventsBefore(runId, before)).then((page) => {
      setLoadingEarlier(false)

      if (!page.ok) {
        setError(page.error)

        return
      }

      setError('')
      setHeld((h) => ({
        ...h,
        events: joinEvents(page.data.events, h.events),
        earlier: page.data.earlier,
        before: page.data.before,
      }))
    })
  }

  return {
    events: held.events,
    progress: held.progress,
    ending: held.ending,
    done: held.done,
    loaded: held.loaded,
    earlier: held.earlier,
    loadingEarlier,
    error,
    loadEarlier,
  }
}
