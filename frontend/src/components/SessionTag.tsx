import { useId, type ReactNode } from 'react'

import { agentName } from '../lib/launch'
import { cardSession, sinceWhen, stateLabel, stateText, stateTint } from '../lib/sessions'
import { formatAge } from '../lib/time'
import { useNowAs } from '../lib/useNow'
import { usePopover } from '../lib/usePopover'
import type { AgentInfo, AgentSession } from '../platform'
import { WorktreeIcon } from './icons'
import { PopoverPanel } from './PopoverPanel'
import { RelativeTime } from './RelativeTime'
import { SessionDot } from './SessionDot'
import { SessionPanel } from './SessionPanel'

export interface SessionsButtonProps {
  // sessions are the item's agent sessions, newest first, which the panel lists.
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
  // align puts the panel's left edge under the button's, or its right edge.
  align: 'left' | 'right'
  // label is the button's accessible name, when its text alone doesn't say it, and title
  // its tooltip.
  label?: string | undefined
  title?: string | undefined
  // shrinks lets the button narrow below its words in a crowded row, as a card's pill
  // does beside its primary; by default it keeps its width.
  shrinks?: boolean
  className: string
  children: ReactNode
}

// SessionsButton opens an item's agent sessions in a panel (SessionPanel): the session
// tag's button, and the card's pills and buttons that show what an agent did.
export function SessionsButton({
  sessions,
  agents,
  align,
  label,
  title,
  shrinks = false,
  className,
  children,
}: SessionsButtonProps) {
  const id = useId()
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ placement: 'fit' })
  const { open, toggle, triggerRef } = popover

  return (
    <span className={`relative inline-flex min-w-0 ${shrinks ? '' : 'shrink-0'}`}>
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        title={title}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={className}
      >
        {children}
      </button>
      <PopoverPanel
        state={popover}
        align={align}
        elevation="card"
        id={id}
        role="dialog"
        aria-label="Agent sessions"
        className="z-20 flex max-h-[calc(100vh-8rem)] w-[28rem] flex-col overflow-y-auto p-3.5 text-left"
      >
        <SessionPanel sessions={sessions} agents={agents} />
      </PopoverPanel>
    </span>
  )
}

export interface SessionTagProps {
  // sessions are the item's agent sessions, newest first.
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
  // markWorktree puts the worktree mark on a session in the item's worktree; the card
  // turns it off while its own worktree tag already says so.
  markWorktree?: boolean
  // align puts the sessions' panel's left edge under the tag's, by default, or its right
  // edge, for a tag near the window's right side.
  align?: 'left' | 'right'
}

// SessionTag follows an item's agent session on its card: a status dot and "Claude ·
// needs approval · 2m", for the newest session still running, or else one that ended in
// the last day, on a faint backing while it works, waits on the user, or failed, so it
// stands apart from the card's other details. A session at work has a ring swelling out
// of its dot (SessionDot), and one in the item's worktree carries the worktree mark,
// wherever it was launched from, unless markWorktree is off. It opens the item's sessions
// (SessionsButton). An item with neither shows nothing.
export function SessionTag({ sessions, agents, markWorktree = true, align = 'left' }: SessionTagProps) {
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
  const inWorktree = shown.worktree !== ''
  const mark = inWorktree && markWorktree
  const what = shown.detail === '' ? `${name} ${label}` : `${name} ${label}: ${shown.detail}`

  return (
    <SessionsButton
      sessions={sessions}
      agents={agents}
      align={align}
      label={`${name} session: ${label}${inWorktree ? ', in its worktree' : ''}. Show the item's sessions`}
      title={inWorktree ? `${what}\nIn its worktree: ${shown.dir}` : what}
      className={`inline-flex max-w-72 items-center gap-1.5 rounded-full px-2 py-px text-caption leading-4 font-medium transition-colors hover:bg-raised aria-expanded:ring-1 aria-expanded:ring-border ${stateText[shown.state]} ${stateTint[shown.state]}`}
    >
      <SessionDot state={shown.state} />
      <span className="truncate">
        {name} · {label}
        {since !== '' && (
          <>
            {' · '}
            <RelativeTime iso={since} show={formatAge} />
          </>
        )}
      </span>
      {mark && <WorktreeIcon className="size-3 shrink-0 opacity-80" />}
    </SessionsButton>
  )
}
