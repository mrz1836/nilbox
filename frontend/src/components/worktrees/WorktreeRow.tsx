import { formatAge } from '../../lib/time'
import { openExternal, type LinkedItem, type Worktree, type WorktreeStage } from '../../platform'
import { buttonBase, plainButton, plural } from './shared'

// badge is a status marker: weight and color mark it, with no pill around it.
const badge = 'text-xs font-medium'

// stageLabels name each stage the backend reports, with its color.
const stageLabels: Readonly<Partial<Record<WorktreeStage, { text: string; className: string }>>> = {
  'in-use': { text: 'in use', className: 'text-info' },
  finished: { text: 'ready to clean up', className: 'text-success' },
  'needs-review': { text: 'needs a look', className: 'text-warning' },
}

export interface WorktreeRowProps {
  worktree: Worktree
  now: Date
  disabled: boolean
  error: string
  onRemove: () => void
  onForceRemove: () => void
  onUnlock: () => void
  onCleanUp: () => void
}

// LastCommit is the age of the worktree's HEAD commit, when it is known.
function LastCommit({ unix, now }: { unix: number; now: Date }) {
  if (unix <= 0) {
    return null
  }

  const iso = new Date(unix * 1000).toISOString()
  const age = formatAge(now, iso)

  return (
    <>
      {' · last commit '}
      <time dateTime={iso}>{age === 'just now' ? age : `${age} ago`}</time>
    </>
  )
}

// ItemLink names the pull request or issue a worktree was made for, opens it on GitHub,
// and says whether it is still open.
function ItemLink({ item, now }: { item: LinkedItem; now: Date }) {
  const kind = item.kind === 'pr' ? 'PR' : 'Issue'
  const age = item.closedAt === '' ? '' : formatAge(now, item.closedAt)

  return (
    <>
      {' · '}
      <button
        type="button"
        title={`${item.owner}/${item.name}#${String(item.number)}: ${item.title}`}
        onClick={() => {
          openExternal(item.url)
        }}
        className="font-medium text-text hover:underline"
      >
        {kind} #{item.number}
      </button>{' '}
      {item.state === 'open' ? (
        'open'
      ) : (
        <>
          {item.state}
          {age !== '' && (
            <>
              {' '}
              <time dateTime={item.closedAt}>{age === 'just now' ? age : `${age} ago`}</time>
            </>
          )}
        </>
      )}
    </>
  )
}

// WorktreeStatus is the row's clean, dirty, locked, prunable, origin, and stage markers.
function WorktreeStatus({ worktree }: { worktree: Worktree }) {
  const stage = stageLabels[worktree.stage]
  const notOnGitHub = !worktree.main && !worktree.onGitHub && worktree.unpushed > 0

  return (
    <>
      {worktree.prunable ? (
        <span className={`${badge} text-warning`}>prunable</span>
      ) : worktree.dirty ? (
        <span className={`${badge} text-warning`}>dirty · {plural(worktree.dirtyCount, 'uncommitted file')}</span>
      ) : (
        worktree.error === '' && <span className={`${badge} text-success`}>clean</span>
      )}
      {notOnGitHub && (
        <span className={`${badge} text-warning`}>{plural(worktree.unpushed, 'commit')} not on GitHub</span>
      )}
      {worktree.locked && <span className={`${badge} text-danger`}>locked</span>}
      {worktree.claudeCreated && <span className={`${badge} text-muted`}>created by Claude</span>}
      {stage !== undefined && <span className={`${badge} ${stage.className}`}>{stage.text}</span>}
    </>
  )
}

// LockNote explains a worktree's lock: who holds it and what can be done about it.
function LockNote({ worktree }: { worktree: Worktree }) {
  const reason = worktree.lockReason === '' ? '' : ` (${worktree.lockReason})`

  if (worktree.agentLock && worktree.staleLock) {
    return (
      <p className="text-xs text-muted">
        Locked by an agent session that has ended{reason}.{' '}
        {worktree.canCleanUp ? 'Clean up releases the lock first.' : 'Unlock it to remove it.'}
      </p>
    )
  }

  if (worktree.agentLock) {
    return (
      <p className="text-xs text-muted">
        An agent session is running in this worktree{reason}. It can be removed once that session ends.
      </p>
    )
  }

  return (
    <p className="text-xs text-muted">
      Locked{worktree.lockReason === '' ? '' : `: ${worktree.lockReason}`}. It was locked by hand, so it cannot be
      removed here. To remove it, unlock it from the repository with{' '}
      <code className="rounded bg-raised px-1">git worktree unlock {worktree.displayPath}</code>.
    </p>
  )
}

// WorktreeNotes explains a locked or prunable worktree and how to clear it.
function WorktreeNotes({ worktree }: { worktree: Worktree }) {
  return (
    <>
      {worktree.locked && <LockNote worktree={worktree} />}
      {worktree.prunable && (
        <p className="text-xs text-muted">
          Its directory is gone{worktree.prunableReason === '' ? '' : ` (${worktree.prunableReason})`}. Prune the
          repository to clear the entry.
        </p>
      )}
    </>
  )
}

// WorktreeActions offers only what the backend reports is allowed: Clean up for a
// finished worktree, Unlock for a lock whose session has ended, Remove for a clean,
// unlocked worktree, and Force remove for a dirty, unlocked one. The main worktree gets a
// label instead.
function WorktreeActions({
  worktree,
  disabled,
  onRemove,
  onForceRemove,
  onUnlock,
  onCleanUp,
}: Omit<WorktreeRowProps, 'now' | 'error'>) {
  if (worktree.main) {
    return <span className="text-xs text-muted">main worktree</span>
  }

  return (
    <>
      {worktree.canCleanUp && (
        <button
          type="button"
          disabled={disabled}
          onClick={onCleanUp}
          title={
            worktree.ownBranch
              ? `Removes the worktree and its branch ${worktree.branch}; every commit is on GitHub`
              : 'Removes the worktree; every commit is on GitHub, and its branch is kept'
          }
          className={`${buttonBase} border-success text-success hover:bg-raised`}
        >
          Clean up
        </button>
      )}
      {worktree.canUnlock && !worktree.canCleanUp && (
        <button type="button" disabled={disabled} onClick={onUnlock} className={plainButton}>
          Unlock
        </button>
      )}
      {worktree.canRemove && !worktree.canCleanUp && (
        <button type="button" disabled={disabled} onClick={onRemove} className={plainButton}>
          Remove
        </button>
      )}
      {worktree.canForceRemove && (
        <button
          type="button"
          disabled={disabled}
          onClick={onForceRemove}
          className={`${buttonBase} border-danger text-danger hover:bg-raised`}
        >
          Force remove
        </button>
      )}
    </>
  )
}

// WorktreeRow is one worktree: its branch, status, actions, where it is, what it was made
// for, and why it cannot simply be removed, with the failure of an action taken on it.
export function WorktreeRow({ worktree, now, disabled, error, ...actions }: WorktreeRowProps) {
  return (
    <li className="flex flex-col gap-1.5 border-b border-border px-3 py-2.5 last:border-b-0">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">
          {worktree.detached || worktree.branch === '' ? 'detached' : worktree.branch}
        </span>
        <WorktreeStatus worktree={worktree} />
        <span className="ml-auto flex items-center gap-2">
          <WorktreeActions worktree={worktree} disabled={disabled} {...actions} />
        </span>
      </div>
      <p className="text-xs text-muted">
        <span>{worktree.displayPath}</span>
        <LastCommit unix={worktree.lastCommitUnix} now={now} />
        {worktree.item !== null && <ItemLink item={worktree.item} now={now} />}
      </p>
      <WorktreeNotes worktree={worktree} />
      {worktree.error !== '' && <p className="text-xs text-danger">{worktree.error}</p>}
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
    </li>
  )
}
