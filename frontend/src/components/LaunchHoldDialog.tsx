import { agentName as agentLabel } from '../lib/launch'
import { formatAgo } from '../lib/time'
import type { AgentInfo, LaunchHold } from '../platform'
import { quietButtonClass } from './controls'
import { ConfirmDialog } from './ConfirmDialog'
import { HoldDetails, holdTitle } from './HoldDetails'
import { EyeIcon, TerminalIcon } from './icons'
import { RelativeTime } from './RelativeTime'

export interface LaunchHoldDialogProps {
  hold: LaunchHold
  // agentName names the agent the session runs, as in "Open in Claude".
  agentName: string
  // agents name the agents of the sessions already running, for a launch held back for
  // them.
  agents?: readonly AgentInfo[]
  // item is the item the launch is for, so a running session started on another item,
  // as two can share a clone, is named; number 0 for a repository's launch, which has no
  // item.
  item?: { repo: string; number: number } | undefined
  onConfirm: () => void
  onCancel: () => void
  // onShow brings a running session's terminal tab forward, by its ID, instead of
  // starting another; without it the dialog offers no such button.
  onShow?: ((sessionId: string) => void) | undefined
  // onWatch opens an agent's background run's view, and onTakeOver takes it over, by its
  // session: a hold for such a run offers them, and never Start another.
  onWatch?: ((sessionId: string) => void) | undefined
  onTakeOver?: ((sessionId: string) => void) | undefined
}

// BackgroundBody says an agent of Autopilot's works in the folder, in the background, with
// Watch it and Take over in place of starting another session beside it.
function BackgroundBody({
  hold,
  run,
  agents,
  onWatch,
  onTakeOver,
}: {
  hold: LaunchHold
  run: LaunchHold['running'][number]
  agents: readonly AgentInfo[]
  onWatch?: ((sessionId: string) => void) | undefined
  onTakeOver?: ((sessionId: string) => void) | undefined
}) {
  const where = hold.worktree === '' ? hold.dir : hold.worktree

  return (
    <div className="flex flex-col gap-3">
      <p>
        {agentLabel(agents, run.agent)} is working in {where} in the background, for Autopilot. Two agents working in
        one worktree undo each other’s changes, so another session doesn’t start beside it. Watch it, or take it over to
        carry its conversation on in your terminal.
      </p>
      <div className="flex flex-wrap gap-1">
        {onWatch !== undefined && (
          <button
            type="button"
            onClick={() => {
              onWatch(run.sessionId)
            }}
            className={quietButtonClass}
          >
            <EyeIcon className="size-3.5" />
            Watch it
          </button>
        )}
        {onTakeOver !== undefined && (
          <button
            type="button"
            onClick={() => {
              onTakeOver(run.sessionId)
            }}
            className={quietButtonClass}
          >
            <TerminalIcon className="size-3.5" />
            Take over
          </button>
        )}
      </div>
    </div>
  )
}

// RunningBody lists the sessions already running where the launch would start, with a
// button to go to the first one's terminal instead. A session started on another item is
// named by it, or by its repository alone when a repository's launch started it, with no
// item to number.
function RunningBody({
  hold,
  agents,
  item,
  onShow,
}: {
  hold: LaunchHold
  agents: readonly AgentInfo[]
  item?: { repo: string; number: number } | undefined
  onShow?: ((sessionId: string) => void) | undefined
}) {
  const where = hold.worktree === '' ? hold.dir : `worktree ${hold.worktree}`
  const focusable = hold.running.find((r) => r.canFocus)

  return (
    <div className="flex flex-col gap-3">
      <ul aria-label="Sessions running here" className="flex flex-col gap-1">
        {hold.running.map((r) => (
          <li key={r.sessionId} className="text-text">
            <span className="font-medium">{agentLabel(agents, r.agent)}</span> {r.doing}
            {item !== undefined && (r.repo !== item.repo || r.number !== item.number) && (
              <span className="text-muted">
                {' on '}
                {r.number === 0 ? r.repo : `${r.repo}#${String(r.number)}`}
              </span>
            )}
            {r.since !== '' && (
              <span className="text-muted">
                {' · '}
                <RelativeTime iso={r.since} show={formatAgo} />
              </span>
            )}
          </li>
        ))}
      </ul>
      <p>
        Two agents working in {where} at once can undo each other’s changes and wait on each other’s git commands. Carry
        on in the session that’s running, or start another beside it.
      </p>
      {onShow !== undefined && focusable !== undefined && (
        <button
          type="button"
          onClick={() => {
            onShow(focusable.sessionId)
          }}
          className={`${quietButtonClass} self-start`}
        >
          <TerminalIcon className="size-3.5" />
          Show in terminal
        </button>
      )}
    </div>
  )
}

// LaunchHoldDialog asks before a launch the app held back: on a pull request from outside
// the repository that changes agent configuration, or whose files could not all be
// checked, into a worktree with uncommitted changes, or where a session is already running
// in the folder the launch would start in.
// Cancel is the default; Open anyway (or Start another) confirms exactly what was shown,
// so a push, or another session, in the meantime asks again. Where one of Autopilot's
// agents works in the background, nothing starts beside it: the dialog offers Watch it and
// Take over instead, and closes.
export function LaunchHoldDialog({
  hold,
  agentName,
  agents = [],
  item,
  onConfirm,
  onCancel,
  onShow,
  onWatch,
  onTakeOver,
}: LaunchHoldDialogProps) {
  const background = hold.reason === 'running' ? hold.running.find((r) => r.background) : undefined

  if (background !== undefined) {
    return (
      <ConfirmDialog
        title="Autopilot’s agent is working here"
        confirmLabel="Close"
        cancelLabel={null}
        tone="primary"
        onConfirm={onCancel}
        onCancel={onCancel}
      >
        <BackgroundBody hold={hold} run={background} agents={agents} onWatch={onWatch} onTakeOver={onTakeOver} />
      </ConfirmDialog>
    )
  }

  if (hold.reason === 'running') {
    return (
      <ConfirmDialog
        title={hold.running.length === 1 ? 'A session is already running here' : 'Sessions are already running here'}
        confirmLabel="Start another"
        tone="primary"
        onConfirm={onConfirm}
        onCancel={onCancel}
      >
        <RunningBody hold={hold} agents={agents} item={item} onShow={onShow} />
      </ConfirmDialog>
    )
  }

  return (
    <ConfirmDialog
      title={holdTitle(hold)}
      confirmLabel="Open anyway"
      tone="danger"
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <HoldDetails hold={hold} agentName={agentName} />
    </ConfirmDialog>
  )
}
