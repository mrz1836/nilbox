import { useState } from 'react'

import { agentName } from '../lib/launch'
import { pulses, sinceWhen, stateDot, stateLabel } from '../lib/sessions'
import { formatAgo, formatFull } from '../lib/time'
import { useMutation } from '../lib/useMutation'
import { copyText, focusSession, resumeSession, type AgentInfo, type AgentSession } from '../platform'
import { quietButtonClass } from './controls'
import { CheckIcon, CopyIcon, PlayIcon, TerminalIcon } from './icons'
import { RelativeTime } from './RelativeTime'

// sessionWhat names what a session ran with: its profile and prompt, or that it resumed
// an earlier one.
function sessionWhat(s: AgentSession): string {
  const parts = [s.profile === '' ? '' : `${s.profile} profile`, s.prompt === '' ? 'resumed' : `${s.prompt} prompt`]

  return parts.filter((p) => p !== '').join(' · ')
}

// StateLine says what a session is doing and since when, with its dot: "needs approval ·
// 2m ago", and what the agent says it waits for.
function StateLine({ session }: { session: AgentSession }) {
  const since = sinceWhen(session)

  return (
    <p className="flex min-w-0 items-center gap-1.5 text-xs text-text">
      <span
        aria-hidden="true"
        className={`size-2 shrink-0 rounded-full ${stateDot[session.state]} ${pulses(session.state) ? 'motion-safe:animate-pulse' : ''}`}
      />
      <span className="font-medium">{stateLabel(session.state)}</span>
      {since !== '' && (
        <span className="text-muted" title={formatFull(since)}>
          {' · '}
          <RelativeTime iso={since} show={formatAgo} />
        </span>
      )}
      {session.detail !== '' && <span className="min-w-0 truncate text-muted">· {session.detail}</span>}
    </p>
  )
}

// SessionEntry is one session: who ran it with what, what it is doing, where, what it last
// said, and what can be done with it from here.
function SessionEntry({ session, agents }: { session: AgentSession; agents: readonly AgentInfo[] }) {
  const focus = useMutation(() => focusSession(session.id))
  const resume = useMutation(() => resumeSession(session.id))
  const copy = useMutation(() => copyText(session.agentSessionId))
  // copied is whether the ID was just copied; it clears on the next copy.
  const [copied, setCopied] = useState(false)
  const error = focus.error || resume.error || copy.error

  const copyID = async () => {
    setCopied(false)
    setCopied((await copy.run()).ok)
  }

  return (
    <li className="flex flex-col gap-1.5 border-b border-border py-2.5 first:pt-0 last:border-b-0 last:pb-0">
      <div className="flex min-w-0 items-baseline gap-2">
        <span className="text-xs font-semibold text-text">{agentName(agents, session.agent)}</span>
        <span className="min-w-0 truncate text-[0.6875rem] text-muted">{sessionWhat(session)}</span>
        <span className="ml-auto shrink-0 text-[0.6875rem] text-muted" title={formatFull(session.launchedAt)}>
          started <RelativeTime iso={session.launchedAt} show={formatAgo} />
        </span>
      </div>
      <StateLine session={session} />
      <p className="truncate font-mono text-[0.6875rem] text-muted" title={session.dir}>
        {session.dir}
      </p>
      {session.lastMessage !== '' && (
        <blockquote className="line-clamp-4 border-l-2 border-border pl-2 text-xs leading-relaxed whitespace-pre-line text-text">
          {session.lastMessage}
        </blockquote>
      )}
      <div className="flex flex-wrap items-center gap-1">
        {session.canFocus && (
          <button
            type="button"
            disabled={focus.pending}
            onClick={() => {
              void focus.run()
            }}
            className={quietButtonClass}
          >
            <TerminalIcon className="size-3.5" />
            Show in terminal
          </button>
        )}
        {session.canResume && (
          <button
            type="button"
            disabled={resume.pending}
            onClick={() => {
              void resume.run()
            }}
            className={quietButtonClass}
          >
            <PlayIcon className="size-3.5" />
            {resume.pending ? 'Resuming…' : 'Resume'}
          </button>
        )}
        {session.agentSessionId !== '' && (
          <button
            type="button"
            title={session.agentSessionId}
            onClick={() => {
              void copyID()
            }}
            className={quietButtonClass}
          >
            {copied ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
            {copied ? 'Copied' : 'Copy session ID'}
          </button>
        )}
      </div>
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
    </li>
  )
}

export interface SessionPanelProps {
  // sessions are the item's sessions, newest first.
  sessions: readonly AgentSession[]
  agents: readonly AgentInfo[]
}

// SessionPanel lists an item's agent sessions, newest first: each one's agent, profile,
// and prompt, what it is doing and for how long, the folder it runs in, what the agent
// last said, and buttons to show its terminal, resume it once it ended, and copy its ID.
export function SessionPanel({ sessions, agents }: SessionPanelProps) {
  return (
    <div className="flex flex-col gap-2.5">
      <h3 className="text-sm font-semibold text-text">Agent sessions</h3>
      <ul className="flex flex-col">
        {sessions.map((s) => (
          <SessionEntry key={s.id} session={s} agents={agents} />
        ))}
      </ul>
    </div>
  )
}
