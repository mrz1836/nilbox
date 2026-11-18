import { useRef, useState } from 'react'

import { agentName } from '../lib/launch'
import {
  githubEventTone,
  githubEventWords,
  isLive,
  setupWords,
  sinceWhen,
  stateLabel,
  terminalName,
  usageWords,
} from '../lib/sessions'
import { formatAgo, formatFull } from '../lib/time'
import { useMutation } from '../lib/useMutation'
import { useQuery } from '../lib/useQuery'
import {
  autopilotRunBySession,
  copyText,
  focusSession,
  forgetSession,
  resumeSession,
  sessionUsage,
  showSessionFolder,
  stopAutopilotRun,
  type AgentInfo,
  type AgentSession,
  type LaunchHold,
  type ResumeRequest,
} from '../platform'
import { useTakeOverTerminal, useWatch } from './autopilot/AutopilotChip'
import { TakeOverDialog } from './autopilot/TakeOverDialog'
import { quietButtonClass } from './controls'
import { AlertIcon, CheckIcon, CopyIcon, EyeIcon, FolderIcon, PlayIcon, StopIcon, TerminalIcon, XIcon } from './icons'
import { LaunchHoldDialog } from './LaunchHoldDialog'
import { RelativeTime } from './RelativeTime'
import { SessionDot } from './SessionDot'

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
      <SessionDot state={session.state} />
      <span className="font-medium">{stateLabel(session.state)}</span>
      {since !== '' && (
        <span className="text-muted" title={formatFull(since)}>
          {' · '}
          <RelativeTime iso={since} show={formatAgo} />
        </span>
      )}
      {session.detail !== '' && <span className="min-w-0 truncate text-muted">· {session.detail}</span>}
      {session.runId !== 0 && (
        <span className="shrink-0 text-accent" title="Autopilot runs it in the background">
          · ⚡ in the background
        </span>
      )}
    </p>
  )
}

// Action is one of a session's buttons.
type Action = 'focus' | 'resume' | 'folder' | 'copy' | 'forget' | 'stop'

// stopRun stops the agent's background run a session is, by its session: the run Stop
// of the Autopilot tab stops.
async function stopRun(sessionId: string): Promise<void> {
  const run = await autopilotRunBySession(sessionId)

  if (run === 0) {
    throw new Error('That run is no longer in Autopilot’s history.')
  }

  await stopAutopilotRun(run)
}

// Held is a resume held back for the user to confirm, with the request to send again,
// once confirmed.
interface Held {
  hold: LaunchHold
  request: ResumeRequest
}

// confirmed is request with hold confirmed: a running session's apart from a pull
// request's files, so a resume held back for both is confirmed for each.
function confirmed({ hold, request }: Held): ResumeRequest {
  return hold.reason === 'running' ? { ...request, alongside: hold.key } : { ...request, confirmed: hold.key }
}

// SessionEntry is one session: who ran it with what, what it is doing, what it started
// with and where, what its launch worked around, what it last said, and what can be done
// with it from here.
function SessionEntry({ session, agents }: { session: AgentSession; agents: readonly AgentInfo[] }) {
  const focus = useMutation((id: string) => focusSession(id))
  const resume = useMutation((request: ResumeRequest) => resumeSession(request))
  const folder = useMutation((id: string) => showSessionFolder(id))
  const copy = useMutation(() => copyText(session.agentSessionId))
  const forget = useMutation((id: string) => forgetSession(id))
  const stop = useMutation((id: string) => stopRun(id))
  const actions = { focus, resume, folder, copy, forget, stop }
  const terminal = terminalName(session)
  const watch = useWatch()
  const takeOverIn = useTakeOverTerminal()
  // background reports an agent's run in the background, which Watch opens, Stop stops,
  // and Take over carries on in the terminal, in place of Resume.
  const background = session.runId !== 0
  const live = isLive(session.state)
  const [takingOver, setTakingOver] = useState(false)
  const setup = setupWords(session)
  // onGitHub is what came of its item on GitHub while it ran, as the inbox learned it, or
  // '': a review, a merge, or a close, by anyone.
  const onGitHub = githubEventWords(session)
  // usage is how many tokens it used, as the agent's own files report them, or a run in
  // the background's record, read again each time it changes state; a session in the
  // terminal the agent hasn't named yet has no files.
  const usage = useQuery(() => sessionUsage(session.id), [session.id, session.stateAt], {
    enabled: session.agentSessionId !== '' || session.runId !== 0,
    log: 'Could not read a session’s token counts',
  })
  const tokens = usageWords(usage.data)
  // copied is whether the ID was just copied; it clears on the next copy.
  const [copied, setCopied] = useState(false)
  // last is the button pressed last. Only its outcome shows, so one that works clears
  // what an earlier one said went wrong.
  const [last, setLast] = useState<Action | null>(null)
  // notice is what the latest resume worked around, as a launch reports it, or ''.
  const [notice, setNotice] = useState('')
  // held is the resume waiting for the user to confirm a hold, or null.
  const [held, setHeld] = useState<Held | null>(null)
  // presses numbers the presses, so a resume that finishes after another press shows no
  // notice.
  const presses = useRef(0)
  const error = last === null ? '' : actions[last].error

  // press starts an action: it becomes the one whose outcome shows. It returns the
  // press's number.
  const press = (action: Action) => {
    presses.current += 1
    setLast(action)
    setNotice('')

    return presses.current
  }

  const showTerminal = () => {
    press('focus')
    void focus.run(session.id)
  }

  // resumeNow resumes the session, or holds it back, as a launch is held, until the user
  // confirms what the hold asks.
  const resumeNow = async (request: ResumeRequest) => {
    const n = press('resume')
    setHeld(null)

    const resumed = await resume.run(request)

    if (!resumed.ok || n !== presses.current) {
      return
    }

    if (resumed.data.hold !== null) {
      setHeld({ hold: resumed.data.hold, request })
    } else {
      setNotice(resumed.data.notice)
    }
  }

  const showFolder = () => {
    press('folder')
    void folder.run(session.id)
  }

  const copyID = async () => {
    press('copy')
    setCopied(false)
    setCopied((await copy.run()).ok)
  }

  // remove forgets the session once it ended; the list reads again when the app says the
  // sessions changed, and this entry leaves it.
  const remove = () => {
    press('forget')
    void forget.run(session.id)
  }

  return (
    <li className="flex flex-col gap-1.5 border-b border-border py-2.5 first:pt-0 last:border-b-0 last:pb-0">
      <div className="flex min-w-0 items-baseline gap-2">
        <span className="text-xs font-semibold text-text">{agentName(agents, session.agent)}</span>
        <span className="min-w-0 truncate text-[0.6875rem] text-muted">{sessionWhat(session)}</span>
        <span className="ml-auto shrink-0 text-[0.6875rem] text-muted" title={formatFull(session.launchedAt)}>
          started <RelativeTime iso={session.launchedAt} show={formatAgo} />
        </span>
        {!isLive(session.state) && (
          <button
            type="button"
            disabled={forget.pending}
            onClick={remove}
            aria-label="Remove this session"
            title="Remove this ended session from the list"
            className="-my-0.5 flex size-5 shrink-0 items-center justify-center self-center rounded-md text-muted transition-colors hover:bg-raised hover:text-danger disabled:opacity-50"
          >
            <XIcon className="size-3.5" />
          </button>
        )}
      </div>
      <StateLine session={session} />
      {setup !== '' && <p className="truncate text-[0.6875rem] text-muted">{setup}</p>}
      {onGitHub !== '' && (
        <p
          className="truncate text-[0.6875rem] text-muted"
          title="What came of it on GitHub while this session ran. It doesn’t say who did it."
        >
          On GitHub: <span className={githubEventTone(session)}>{onGitHub}</span> ·{' '}
          <RelativeTime iso={session.githubEventAt} show={formatAgo} />
        </p>
      )}
      {tokens !== null && (
        <p className="truncate text-[0.6875rem] text-muted" title={tokens.title}>
          {tokens.text}
        </p>
      )}
      <p className="truncate font-mono text-[0.6875rem] text-muted" title={session.dir}>
        {session.dir}
      </p>
      {session.notice !== '' && (
        <p className="flex items-start gap-1.5 text-xs text-warning">
          <AlertIcon className="mt-0.5 size-3.5 shrink-0" />
          <span>{session.notice}</span>
        </p>
      )}
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
            onClick={showTerminal}
            title={`Bring forward the ${terminal === '' ? 'terminal' : terminal} tab it runs in`}
            className={quietButtonClass}
          >
            <TerminalIcon className="size-3.5" />
            Show in terminal
          </button>
        )}
        {background && watch !== undefined && (
          <button
            type="button"
            onClick={() => {
              watch(session.id)
            }}
            title="Open its run in the Autopilot tab"
            className={quietButtonClass}
          >
            <EyeIcon className="size-3.5" />
            Watch
          </button>
        )}
        {background && live && (
          <button
            type="button"
            disabled={stop.pending}
            onClick={() => {
              press('stop')
              void stop.run(session.id)
            }}
            className={quietButtonClass}
          >
            <StopIcon className="size-3.5 text-danger" />
            {stop.pending ? 'Stopping…' : 'Stop'}
          </button>
        )}
        {background && session.agentSessionId !== '' && (
          <button
            type="button"
            onClick={() => {
              press('resume')
              setTakingOver(true)
            }}
            title={`Carry its conversation on in ${takeOverIn}`}
            className={quietButtonClass}
          >
            <TerminalIcon className="size-3.5" />
            Take over
          </button>
        )}
        {!background && session.canResume && (
          <button
            type="button"
            disabled={resume.pending}
            onClick={() => {
              void resumeNow({ sessionId: session.id, access: '', alongside: '', confirmed: '' })
            }}
            className={quietButtonClass}
          >
            <PlayIcon className="size-3.5" />
            {resume.pending ? 'Resuming…' : 'Resume'}
          </button>
        )}
        <button
          type="button"
          disabled={folder.pending}
          onClick={showFolder}
          title={`Show ${session.dir} in Finder`}
          className={quietButtonClass}
        >
          <FolderIcon className="size-3.5" />
          Show in Finder
        </button>
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
      {notice !== '' && (
        <p role="status" className="text-xs text-muted">
          {notice}
        </p>
      )}
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
      {takingOver && (
        <TakeOverDialog
          sessionId={session.id}
          live={live}
          terminal={takeOverIn}
          where={session.worktree}
          agentName={agentName(agents, session.agent)}
          onCancel={() => {
            setTakingOver(false)
          }}
          onTaken={(result) => {
            setTakingOver(false)
            setNotice(result.notice === '' ? `Its conversation carries on in ${takeOverIn}.` : result.notice)
          }}
        />
      )}
      {held !== null && (
        <LaunchHoldDialog
          hold={held.hold}
          agentName={agentName(agents, session.agent)}
          agents={agents}
          item={{ repo: session.repo, number: session.number }}
          onCancel={() => {
            setHeld(null)
          }}
          onConfirm={() => {
            void resumeNow(confirmed(held))
          }}
          onShow={(sessionId) => {
            setHeld(null)
            press('focus')
            void focus.run(sessionId)
          }}
        />
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
// and prompt, what it is doing and for how long, the permissions it started with, how many
// tokens it used where the agent's own files say, the folder it runs in and what its
// launch did to the item's worktree, what the launch worked around, what the agent last
// said, and buttons to show its terminal, resume it
// once it ended, show its folder in Finder, copy its ID, and remove it once it ended.
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
