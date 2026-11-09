import { useId } from 'react'

import { agentName } from '../lib/launch'
import { cardSession, pulses, sinceWhen, stateDot, stateLabel, stateText } from '../lib/sessions'
import { formatAge } from '../lib/time'
import { useNowAs } from '../lib/useNow'
import { usePopover } from '../lib/usePopover'
import type { AgentInfo, AgentSession } from '../platform'
import { PopoverPanel } from './PopoverPanel'
import { RelativeTime } from './RelativeTime'
import { SessionPanel } from './SessionPanel'

export interface SessionTagProps {
  // sessions are the item's agent sessions, newest first.
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
}

// SessionTag follows an item's agent session on its card: a status dot and "Claude ·
// needs approval · 2m", for the newest session still running, or else one that ended in
// the last day. It opens the item's sessions (SessionPanel). An item with neither shows
// nothing.
export function SessionTag({ sessions, agents }: SessionTagProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ placement: 'fit' })
  const { open, toggle, triggerRef } = popover
  // shown is the session the tag follows; it reads the clock so an ended session leaves
  // the card a day after it ended.
  const shownID = useNowAs((now) => cardSession(sessions, now)?.id ?? null)
  const shown = sessions.find((s) => s.id === shownID)

  if (shown === undefined) {
    return null
  }

  const name = agentName(agents, shown.agent)
  const label = stateLabel(shown.state)
  const since = sinceWhen(shown)

  return (
    <span className="relative inline-flex min-w-0 shrink-0">
      <button
        ref={triggerRef}
        type="button"
        aria-label={`${name} session: ${label}. Show the item's sessions`}
        title={shown.detail === '' ? `${name} ${label}` : `${name} ${label}: ${shown.detail}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={`inline-flex max-w-72 items-center gap-1.5 rounded-full px-1.5 py-px text-[0.6875rem] leading-4 font-medium transition-colors hover:bg-raised aria-expanded:ring-1 aria-expanded:ring-border ${stateText[shown.state]}`}
      >
        <span
          aria-hidden="true"
          className={`size-1.5 shrink-0 rounded-full ${stateDot[shown.state]} ${pulses(shown.state) ? 'motion-safe:animate-pulse' : ''}`}
        />
        <span className="truncate">
          {name} · {label}
          {since !== '' && (
            <>
              {' · '}
              <RelativeTime iso={since} show={formatAge} />
            </>
          )}
        </span>
      </button>
      <PopoverPanel
        state={popover}
        align="left"
        id={id}
        role="dialog"
        aria-label="Agent sessions"
        className="z-20 flex max-h-[calc(100vh-8rem)] w-[28rem] flex-col overflow-y-auto p-3.5 text-left shadow-black/25"
      >
        <SessionPanel sessions={sessions} agents={agents} />
      </PopoverPanel>
    </span>
  )
}
