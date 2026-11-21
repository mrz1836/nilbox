import { brand } from '../generated/brand'
import { counted, formatCount } from '../lib/format'
import { changeText } from '../lib/sensitive'
import { openExternal, type LaunchHold } from '../platform'
import { ExternalIcon } from './icons'

// HeldStart is what a held start shows of its hold, the launcher's own, as the launch menu's
// hold dialog and a routine's waiting card both have it: why it is held (agentFiles,
// unchecked, or dirty), the pull request's agent files and how many of its files were
// checked, where the session would start, and how many of its worktree's files have
// uncommitted changes.
export type HeldStart = Pick<LaunchHold, 'files' | 'checked' | 'total' | 'error' | 'atHead' | 'dir' | 'worktree'> & {
  reason: string
  dirty: number
}

// whereText names where the session would start: its worktree, or its folder.
function whereText(hold: HeldStart): string {
  return hold.worktree === '' ? hold.dir : `worktree ${hold.worktree}`
}

// loads says when the agent would load the pull request's files: at once in a worktree at
// its head, otherwise once a session checks it out.
function loads(hold: HeldStart, agentName: string): string {
  if (hold.atHead) {
    return `The session starts in worktree ${hold.worktree}, at the pull request’s head, so ${agentName} loads these files the moment it starts.`
  }

  return `If the session checks the pull request out, ${agentName} loads these files in it and in every later session in ${whereText(hold)}.`
}

interface BodyProps {
  hold: HeldStart
  agentName: string
  // go is what confirming does, as the last sentence says it: "open the session".
  go: string
}

// AgentFilesBody explains a pull request that changes agent configuration, listing the
// files, each opening its diff on GitHub.
function AgentFilesBody({ hold, agentName, go }: BodyProps) {
  return (
    <div className="flex flex-col gap-3">
      <p>
        {loads(hold, agentName)} Hooks in them run commands on your Mac, even in plan or read-only mode, and
        instructions in them steer the session.
      </p>
      <ul
        aria-label="Agent configuration it changes"
        className="flex max-h-40 flex-col gap-1 overflow-y-auto rounded-md border border-border bg-background px-3 py-2"
      >
        {hold.files.map((file) => (
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
            <span className="ml-auto shrink-0 text-caption text-muted">{changeText(file)}</span>
          </li>
        ))}
      </ul>
      <p>Read them on GitHub first, and {go} only if they’re safe.</p>
    </div>
  )
}

// UncheckedBody explains a pull request whose files could not all be checked.
function UncheckedBody({ hold, agentName, go }: BodyProps) {
  const why =
    hold.error !== ''
      ? `GitHub didn’t list its files: ${hold.error}.`
      : `GitHub listed only ${formatCount(hold.checked)} of its ${counted(hold.total, 'changed file', 'changed files')}.`

  return (
    <div className="flex flex-col gap-3">
      <p>
        {why} So {brand.name} can’t tell whether it changes agent configuration, such as hooks that run commands on your
        Mac as soon as {agentName} starts, even in plan or read-only mode.
      </p>
      <p>Look over its files on GitHub first, and {go} only if they’re safe.</p>
    </div>
  )
}

// DirtyBody explains a worktree with uncommitted changes, which may be the user's own work
// in progress.
function DirtyBody({ hold, agentName, go }: BodyProps) {
  return (
    <div className="flex flex-col gap-3">
      <p>
        {hold.worktree === '' ? hold.dir : hold.worktree} has{' '}
        {counted(hold.dirty, 'uncommitted file', 'uncommitted files')}, which may be your own work in progress.{' '}
        {agentName} would work on top of them, and could change or commit them.
      </p>
      <p>Commit or put them aside first, or {go} if they can go with it.</p>
    </div>
  )
}

export interface HoldDetailsProps {
  hold: HeldStart
  // agentName names the agent the session runs, as in "Claude loads these files".
  agentName: string
  // go is what confirming does, as the last sentence says: "open the session" by default.
  go?: string
}

// HoldDetails explains why a start is held back for the user's OK: a pull request from
// outside the repository that changes agent configuration, listing the files; one whose
// files could not all be checked; or a worktree with uncommitted changes.
export function HoldDetails({ hold, agentName, go = 'open the session' }: HoldDetailsProps) {
  switch (hold.reason) {
    case 'agentFiles':
      return <AgentFilesBody hold={hold} agentName={agentName} go={go} />
    case 'dirty':
      return <DirtyBody hold={hold} agentName={agentName} go={go} />
    default:
      return <UncheckedBody hold={hold} agentName={agentName} go={go} />
  }
}

// holdTitle names what holds a start back, as a dialog's or a card's heading.
export function holdTitle(hold: Pick<HeldStart, 'reason'>): string {
  switch (hold.reason) {
    case 'agentFiles':
      return 'This pull request changes agent configuration'
    case 'dirty':
      return 'Its worktree has uncommitted changes'
    default:
      return 'Its files couldn’t all be checked'
  }
}
