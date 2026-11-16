import { agentName as agentLabel } from '../lib/launch'
import { formatAgo } from '../lib/time'
import type { AgentInfo, LaunchHold } from '../platform'
import { quietButtonClass } from './controls'
import { ConfirmDialog } from './ConfirmDialog'
import { HoldDetails, holdTitle } from './HoldDetails'
import { TerminalIcon } from './icons'
import { RelativeTime } from './RelativeTime'

export interface LaunchHoldDialogProps {
  hold: LaunchHold
  // agentName names the agent the session runs, as in "Open in Claude".
  agentName: string
  // agents name the agents of the sessions already running, for a launch held back for
  // them.
  agents?: readonly AgentInfo[]
  // item is the item the launch is for, so a running session started on another item,
  // as two can share a clone, is named.
  item?: { repo: string; number: number } | undefined
  onConfirm: () => void
  onCancel: () => void
  // onShow brings a running session's terminal tab forward, by its ID, instead of
  // starting another; without it the dialog offers no such button.
  onShow?: ((sessionId: string) => void) | undefined
}

// RunningBody lists the sessions already running where the launch would start, with a
// button to go to the first one's terminal instead.
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
                {r.repo}#{r.number}
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
// so a push, or another session, in the meantime asks again.
export function LaunchHoldDialog({
  hold,
  agentName,
  agents = [],
  item,
  onConfirm,
  onCancel,
  onShow,
}: LaunchHoldDialogProps) {
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
