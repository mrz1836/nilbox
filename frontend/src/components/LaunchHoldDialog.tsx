import { brand } from '../generated/brand'
import { counted, formatCount } from '../lib/format'
import { changeText } from '../lib/sensitive'
import { openExternal, type LaunchHold } from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { ExternalIcon } from './icons'

export interface LaunchHoldDialogProps {
  hold: LaunchHold
  // agentName names the agent the session runs, as in "Open in Claude".
  agentName: string
  onConfirm: () => void
  onCancel: () => void
}

// where says when the agent would load the pull request's files: at once in a worktree at
// its head, otherwise once a session checks it out.
function where(hold: LaunchHold, agentName: string): string {
  if (hold.atHead) {
    return `The session starts in worktree ${hold.worktree}, at the pull request’s head, so ${agentName} loads these files the moment it starts.`
  }

  return `If the session checks the pull request out, ${agentName} loads these files in it and in every later session in ${hold.worktree === '' ? hold.dir : `worktree ${hold.worktree}`}.`
}

// AgentFilesBody explains a pull request that changes agent configuration, listing the
// files, each opening its diff on GitHub.
function AgentFilesBody({ hold, agentName }: { hold: LaunchHold; agentName: string }) {
  const files = hold.files

  return (
    <div className="flex flex-col gap-3">
      <p>
        {where(hold, agentName)} Hooks in them run commands on your Mac, even in plan mode, and instructions in them
        steer the session.
      </p>
      <ul
        aria-label="Agent configuration it changes"
        className="flex max-h-40 flex-col gap-1 overflow-y-auto rounded-md border border-border bg-background px-3 py-2"
      >
        {files.map((file) => (
          <li key={file.path} className="flex min-w-0 items-baseline gap-2">
            <button
              type="button"
              disabled={file.url === ''}
              title={`Open the diff of ${file.path} on GitHub`}
              onClick={() => {
                openExternal(file.url)
              }}
              className="inline-flex min-w-0 items-baseline gap-1 text-left font-mono text-xs text-text enabled:hover:text-accent enabled:hover:underline"
            >
              <span className="truncate">{file.path}</span>
              {file.url !== '' && <ExternalIcon className="size-3 shrink-0 self-center text-muted" />}
            </button>
            <span className="ml-auto shrink-0 text-[11px] text-muted">{changeText(file)}</span>
          </li>
        ))}
      </ul>
      <p>Read them on GitHub first, and open the session only if they’re safe.</p>
    </div>
  )
}

// UncheckedBody explains a pull request whose files could not all be checked.
function UncheckedBody({ hold, agentName }: { hold: LaunchHold; agentName: string }) {
  const why =
    hold.error !== ''
      ? `GitHub didn’t list its files: ${hold.error}.`
      : `GitHub listed only ${formatCount(hold.checked)} of its ${counted(hold.total, 'changed file', 'changed files')}.`

  return (
    <div className="flex flex-col gap-3">
      <p>
        {why} So {brand.name} can’t tell whether it changes agent configuration, such as hooks that run commands on your
        Mac as soon as {agentName} starts, even in plan mode.
      </p>
      <p>Look over its files on GitHub first, and open the session only if they’re safe.</p>
    </div>
  )
}

// LaunchHoldDialog asks before a launch the app held back: on a pull request from outside
// the repository that changes agent configuration, or whose files could not all be
// checked. Cancel is the default; Open anyway confirms exactly what was shown, so a push
// in the meantime asks again.
export function LaunchHoldDialog({ hold, agentName, onConfirm, onCancel }: LaunchHoldDialogProps) {
  const agent = hold.reason === 'agentFiles'

  return (
    <ConfirmDialog
      title={agent ? 'This pull request changes agent configuration' : 'Its files couldn’t all be checked'}
      confirmLabel="Open anyway"
      tone="danger"
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {agent ? (
        <AgentFilesBody hold={hold} agentName={agentName} />
      ) : (
        <UncheckedBody hold={hold} agentName={agentName} />
      )}
    </ConfirmDialog>
  )
}
