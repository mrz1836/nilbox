import { useEffect, useEffectEvent, useRef, useState } from 'react'

import {
  describeItConversation,
  describeItEvents,
  newDescribeItConversation,
  on,
  sendToDescribeIt,
  stopDescribeIt,
  type AutopilotRunEvent,
  type DescribeAgentChoice,
  type DescribeConversation,
  type DescribeMessage,
} from '../platform'
import { attempt, type Result } from './call'
import { lastSeq } from './describeit'
import { appendEvents } from './runView'
import { useMutation } from './useMutation'

// TurnEvents are one turn's events as the window holds them: those read so far, oldest
// first; whether a first page came; and why the last read failed, '' for none.
export interface TurnEvents {
  events: readonly AutopilotRunEvent[]
  loaded: boolean
  error: string
}

// Conversation is Describe it's current conversation as the window follows it, with what
// the user can do to it: send the next message, stop the turn running, or start another
// conversation, each with its own pending and error states.
export interface Conversation {
  // conversation is the current one, undefined until the first read, and error why the
  // last read failed.
  conversation: DescribeConversation | undefined
  error: string
  // eventsOf is a turn's events, by its number.
  eventsOf: (turn: number) => TurnEvents
  send: (message: DescribeMessage) => Promise<Result<DescribeConversation>>
  sending: boolean
  sendError: string
  stop: () => void
  stopping: boolean
  stopError: string
  startNew: (agent: DescribeAgentChoice) => Promise<Result<DescribeConversation>>
  starting: boolean
  newError: string
  // refresh reads the conversation again, as a card's outcome changes it.
  refresh: () => void
}

// Reads are the reads of one thing, one at a time with one more waiting: a read asked for
// while one runs waits for it to end, and one asked for while another waits is that one,
// which reads what is newest once it runs.
interface Reads {
  tail: Promise<unknown>
  waiting: boolean
}

// queue asks for a read of r: it runs read once the read under way ends, unless one waits
// already. A read that throws ends as one that failed would, so the next still runs.
function queue(r: Reads, read: () => Promise<void>): void {
  if (r.waiting) {
    return
  }

  r.waiting = true
  r.tail = r.tail
    .then(() => {
      r.waiting = false

      return read()
    })
    .catch((err: unknown) => {
      console.error('could not follow the conversation', err)
    })
}

// Follow is what the hook keeps of following one turn's events: the newest it has, whether
// the turn's events are all in, and its reads.
interface Follow {
  last: number
  done: boolean
  reads: Reads
}

// noEvents is a turn's events before its first read.
const noEvents: TurnEvents = { events: [], loaded: false, error: '' }

// turnKey names a turn of a conversation.
function turnKey(id: string, turn: number): string {
  return `${id}#${String(turn)}`
}

// useConversation follows Describe it's current conversation: it reads it as it mounts and
// again on each describeit:updated for it (or for the first one, while there is none), one
// read at a time with one more waiting; it reads each turn's events after the last it has,
// a turn's that ended once and a running turn's again with each read of the conversation
// until the turn has ended; and it sends, stops, and starts conversations anew, showing the
// conversation each answers with.
export function useConversation(): Conversation {
  const [conversation, setConversation] = useState<DescribeConversation | undefined>(undefined)
  const [error, setError] = useState('')
  const [turns, setTurns] = useState<ReadonlyMap<string, TurnEvents>>(new Map())
  // held is the conversation followed, by ID; follows, what is followed of each of its
  // turns; reads, the conversation's reads; and answers counts the conversations a call
  // answered with, so a read that was under way as one came, older than it, is dropped.
  const held = useRef('')
  const follows = useRef(new Map<string, Follow>())
  const reads = useRef<Reads>({ tail: Promise.resolve(), waiting: false })
  const answers = useRef(0)
  const sender = useMutation((m: DescribeMessage) => sendToDescribeIt(m))
  const stopper = useMutation((id: string) => stopDescribeIt(id))
  const starter = useMutation((agent: DescribeAgentChoice) => newDescribeItConversation(agent))

  // keep sets a turn's events, as a read changes them.
  const keep = (key: string, change: (was: TurnEvents) => TurnEvents) => {
    setTurns((was) => new Map(was).set(key, change(was.get(key) ?? noEvents)))
  }

  // readTurn reads the events of the turn n of the conversation id after the last it has,
  // until they are all in.
  const readTurn = (id: string, n: number) => {
    const key = turnKey(id, n)
    const f = follows.current.get(key) ?? { last: 0, done: false, reads: { tail: Promise.resolve(), waiting: false } }

    follows.current.set(key, f)

    if (f.done) {
      return
    }

    queue(f.reads, async () => {
      const page = await attempt(describeItEvents(id, n, f.last))

      if (!page.ok) {
        keep(key, (was) => ({ ...was, loaded: true, error: page.error }))

        return
      }

      keep(key, (was) => ({ events: appendEvents(was.events, page.data.events), loaded: true, error: '' }))
      f.last = Math.max(f.last, lastSeq(page.data.events))
      f.done = page.data.done
    })
  }

  // take shows a conversation, as read or as a call answered with it, and follows its
  // turns' events: another conversation starts with none.
  const take = (c: DescribeConversation) => {
    if (c.id !== held.current) {
      held.current = c.id
      follows.current = new Map()
      setTurns(new Map())
    }

    setConversation(c)

    for (const t of c.turns) {
      readTurn(c.id, t.n)
    }
  }

  // read reads the conversation again.
  const read = () => {
    queue(reads.current, async () => {
      const asked = answers.current
      const got = await attempt(describeItConversation())

      if (asked !== answers.current) {
        return
      }

      if (got.ok) {
        setError('')
        take(got.data)
      } else {
        setError(got.error)
      }
    })
  }

  // answered shows the conversation a call answered with.
  const answered = (c: DescribeConversation) => {
    answers.current += 1
    take(c)
  }

  const readOnUpdate = useEffectEvent((conversationId: string) => {
    if (conversationId === held.current || held.current === '') {
      read()
    }
  })

  const readFirst = useEffectEvent(() => {
    read()
  })

  // The conversation is read as the view mounts, and again each time the shell says it
  // changed.
  useEffect(() => {
    readFirst()

    return on('describeit:updated', ({ conversationId }) => {
      readOnUpdate(conversationId)
    })
  }, [])

  const send = async (message: DescribeMessage) => {
    const sent = await sender.run(message)

    if (sent.ok) {
      answered(sent.data)
    }

    return sent
  }

  const startNew = async (agent: DescribeAgentChoice) => {
    const started = await starter.run(agent)

    if (started.ok) {
      answered(started.data)
    }

    return started
  }

  return {
    conversation,
    error,
    eventsOf: (turn) => turns.get(turnKey(conversation?.id ?? '', turn)) ?? noEvents,
    send,
    sending: sender.pending,
    sendError: sender.error,
    stop: () => {
      if (conversation !== undefined && conversation.id !== '') {
        void stopper.run(conversation.id).then(read)
      }
    },
    stopping: stopper.pending,
    stopError: stopper.error,
    startNew,
    starting: starter.pending,
    newError: starter.error,
    refresh: read,
  }
}
