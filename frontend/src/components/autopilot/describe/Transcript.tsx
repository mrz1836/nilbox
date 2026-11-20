import { useState } from 'react'

import { cardPlaces, revisedCards, turnEntries, type Entry } from '../../../lib/describeit'
import type { TurnEvents } from '../../../lib/useConversation'
import type { DescribeCard, DescribeConversation, DescribeTurn } from '../../../platform'
import { chipClass, quietButtonClass } from '../../controls'
import { AlertIcon } from '../../icons'
import { Markdown } from '../../Markdown'
import { Spinner } from '../../Spinner'
import type { CardKit } from './parts'
import { ProposalCard } from './ProposalCard'

export interface TranscriptProps {
  conversation: DescribeConversation
  // eventsOf is a turn's events as read so far.
  eventsOf: (turn: number) => TurnEvents
  kit: CardKit
}

// stateMarks mark how one of Claude's tools came out.
const stateMarks: Readonly<Record<Extract<Entry, { kind: 'activity' }>['state'], { mark: string; label: string }>> = {
  ok: { mark: '✓', label: 'done' },
  failed: { mark: '✗', label: 'failed' },
  pending: { mark: '…', label: 'under way' },
  '': { mark: '', label: '' },
}

// Line is one line of what Claude did: what it said, through Markdown; a plain line for a
// tool it used, marked with how it came out; or a note.
function Line({ entry }: { entry: Entry }) {
  switch (entry.kind) {
    case 'say':
      return <Markdown text={entry.text} />
    case 'activity': {
      const state = stateMarks[entry.state]

      return (
        <p className={`flex items-baseline gap-1.5 text-xs ${entry.state === 'failed' ? 'text-danger' : 'text-muted'}`}>
          <span>{entry.text}</span>
          {state.mark !== '' && (
            <span role="img" aria-label={state.label} className={entry.state === 'ok' ? 'text-success' : ''}>
              {state.mark}
            </span>
          )}
          {entry.detail !== '' && <span>{entry.detail}</span>}
        </p>
      )
    }
    case 'note':
      return <p className={`text-xs ${entry.error ? 'text-danger' : 'text-muted'}`}>{entry.text}</p>
  }
}

// Revised is an older card a later one revises, in one line, which opens it whole.
function Revised({ card, kit }: { card: DescribeCard; kit: CardKit }) {
  const [open, setOpen] = useState(false)

  if (open) {
    return <ProposalCard card={card} kit={kit} />
  }

  return (
    <p className="flex items-center gap-2 text-xs text-muted">
      <span className="min-w-0 truncate" title={card.summary}>
        {`Revised below: ${card.summary}`}
      </span>
      <button
        type="button"
        aria-expanded={false}
        onClick={() => {
          setOpen(true)
        }}
        className={quietButtonClass}
      >
        Show this card
      </button>
    </p>
  )
}

// YourMessage is what the user sent in a turn: their words, what they attached, and,
// quietly, what the turn told Claude they did since the turn before.
function YourMessage({ turn }: { turn: DescribeTurn }) {
  return (
    <div className="flex max-w-[85%] flex-col gap-1.5 self-end rounded-lg bg-raised px-3 py-2">
      <span className="sr-only">You:</span>
      <p className="text-sm leading-relaxed whitespace-pre-wrap text-text">{turn.text}</p>
      {turn.attach.length > 0 && (
        <ul aria-label="Attached" className="flex flex-wrap gap-1.5">
          {turn.attach.map((a) => (
            <li key={`${a.kind}:${String(a.id)}`} className={chipClass}>
              {a.label}
            </li>
          ))}
        </ul>
      )}
      {turn.feedback !== '' && <p className="text-xs text-muted">{`Claude heard: ${turn.feedback}`}</p>}
    </div>
  )
}

interface TurnViewProps {
  turn: DescribeTurn
  events: TurnEvents
  revised: ReadonlySet<string>
  kit: CardKit
}

// TurnView is one turn: the user's message, then what Claude did (what it said, its tools,
// and its cards, each after the proposal that made it), its reply, and how the turn ended
// when it didn't finish.
function TurnView({ turn, events, revised, kit }: TurnViewProps) {
  const live = turn.state === 'running'
  const entries = turnEntries(events.events, live, turn.reply)
  const places = cardPlaces(entries, turn.cards)

  const card = (c: DescribeCard) =>
    revised.has(c.ref.proposal) ? (
      <Revised key={c.ref.proposal} card={c} kit={kit} />
    ) : (
      <ProposalCard key={c.ref.proposal} card={c} kit={kit} />
    )

  return (
    <div className="flex flex-col gap-3">
      <YourMessage turn={turn} />
      <div className="flex flex-col gap-2">
        <span className="sr-only">Claude:</span>
        {entries.map((e) => {
          const at = places.at.get(e.key)

          return (
            <div key={e.key} className="flex flex-col gap-2">
              <Line entry={e} />
              {at !== undefined && card(at)}
            </div>
          )
        })}
        {places.rest.map(card)}
        {turn.reply !== '' && <Markdown text={turn.reply} />}
        {live && (
          <p role="status" className="flex items-center gap-2 text-xs text-muted">
            <Spinner className="size-3.5" />
            Claude is working on it…
          </p>
        )}
        {turn.state === 'failed' && (
          <p className="flex items-start gap-1.5 text-xs text-danger">
            <AlertIcon className="mt-0.5 size-3 shrink-0" />
            <span>{turn.error === '' ? 'This turn ended in an error.' : turn.error}</span>
          </p>
        )}
        {turn.state === 'stopped' && <p className="text-xs text-muted">Stopped. Send another message to go on.</p>}
        {events.error !== '' && (
          <p className="text-xs text-danger">{`What Claude did couldn’t be read: ${events.error}`}</p>
        )}
      </div>
    </div>
  )
}

// Transcript is the conversation, oldest turn first: each the user's message and what
// Claude did, its cards among its lines; an older card a later one revises shows in one
// line.
export function Transcript({ conversation, eventsOf, kit }: TranscriptProps) {
  const revised = revisedCards(conversation.turns.flatMap((t) => t.cards))

  return (
    <div role="log" aria-label="Your conversation with Claude" className="flex flex-col gap-6">
      {conversation.turns.map((t) => (
        <TurnView key={t.n} turn={t} events={eventsOf(t.n)} revised={revised} kit={kit} />
      ))}
    </div>
  )
}
