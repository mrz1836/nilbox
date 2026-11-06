import { useEffect, useState } from 'react'

import { formatAge } from '../lib/age'
import { errorMessage } from '../lib/format'
import {
  forceRemoveWorktree,
  listWorktrees,
  pruneWorktrees,
  removeAllCleanWorktrees,
  removeWorktree,
  type RemoveResult,
  type Worktree,
  type WorktreeRepo,
  type WorktreesSnapshot,
} from '../platform'
import { ConfirmDialog } from './ConfirmDialog'

export interface WorktreesViewProps {
  // clock returns the current time for the last-commit ages. Keep it stable across renders.
  clock?: () => Date
}

// Confirmation is the destructive action waiting for the user's yes.
type Confirmation = { kind: 'force'; repoPath: string; worktree: Worktree } | { kind: 'all'; count: number }

function systemClock(): Date {
  return new Date()
}

// plural is "1 file" or "3 files".
function plural(n: number, word: string): string {
  return `${String(n)} ${word}${n === 1 ? '' : 's'}`
}

// rowKey and repoKey key the inline errors, which a repository's main worktree would
// otherwise share with the repository itself.
function rowKey(path: string): string {
  return `worktree:${path}`
}

function repoKey(path: string): string {
  return `repo:${path}`
}

// badge is a status marker: weight and color mark it, with no pill around it.
const badge = 'text-xs font-medium'

interface WorktreeRowProps {
  worktree: Worktree
  now: Date
  disabled: boolean
  error: string
  onRemove: () => void
  onForceRemove: () => void
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

// WorktreeStatus is the row's clean, dirty, locked, prunable, and origin markers.
function WorktreeStatus({ worktree }: { worktree: Worktree }) {
  return (
    <>
      {worktree.prunable ? (
        <span className={`${badge} text-warning`}>prunable</span>
      ) : worktree.dirty ? (
        <span className={`${badge} text-warning`}>dirty · {plural(worktree.dirtyCount, 'uncommitted file')}</span>
      ) : (
        worktree.error === '' && <span className={`${badge} text-success`}>clean</span>
      )}
      {worktree.locked && <span className={`${badge} text-danger`}>locked</span>}
      {worktree.claudeCreated && <span className={`${badge} text-muted`}>created by Claude</span>}
    </>
  )
}

// WorktreeNotes explains a locked or prunable worktree and how to clear it.
function WorktreeNotes({ worktree }: { worktree: Worktree }) {
  return (
    <>
      {worktree.locked && (
        <p className="text-xs text-muted">
          Locked{worktree.lockReason === '' ? '' : `: ${worktree.lockReason}`}. A locked worktree usually belongs to a
          running session, so it cannot be removed here. Once that session has ended, unlock it from the repository with{' '}
          <code className="rounded bg-raised px-1">git worktree unlock {worktree.displayPath}</code>.
        </p>
      )}
      {worktree.locked && worktree.staleLock && (
        <p className="text-xs text-warning">The session appears to have ended (stale lock).</p>
      )}
      {worktree.prunable && (
        <p className="text-xs text-muted">
          Its directory is gone{worktree.prunableReason === '' ? '' : ` (${worktree.prunableReason})`}. Prune the
          repository to clear the entry.
        </p>
      )}
    </>
  )
}

// WorktreeActions offers only what the backend reports is allowed: Remove for a clean,
// unlocked worktree and Force remove for a dirty, unlocked one. The main worktree gets a
// label instead, and a locked worktree gets nothing.
function WorktreeActions({ worktree, disabled, onRemove, onForceRemove }: Omit<WorktreeRowProps, 'now' | 'error'>) {
  if (worktree.main) {
    return <span className="text-xs text-muted">main worktree</span>
  }

  const button = 'rounded-md border px-3 py-1 text-sm font-medium disabled:opacity-60'

  return (
    <>
      {worktree.canRemove && (
        <button
          type="button"
          disabled={disabled}
          onClick={onRemove}
          className={`${button} border-border bg-surface hover:bg-raised`}
        >
          Remove
        </button>
      )}
      {worktree.canForceRemove && (
        <button
          type="button"
          disabled={disabled}
          onClick={onForceRemove}
          className={`${button} border-danger text-danger hover:bg-raised`}
        >
          Force remove
        </button>
      )}
    </>
  )
}

function WorktreeRow({ worktree, now, disabled, error, onRemove, onForceRemove }: WorktreeRowProps) {
  return (
    <li className="flex flex-col gap-1.5 border-b border-border px-3 py-2.5 last:border-b-0">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">
          {worktree.detached || worktree.branch === '' ? 'detached' : worktree.branch}
        </span>
        <WorktreeStatus worktree={worktree} />
        <span className="ml-auto flex items-center gap-2">
          <WorktreeActions worktree={worktree} disabled={disabled} onRemove={onRemove} onForceRemove={onForceRemove} />
        </span>
      </div>
      <p className="text-xs text-muted">
        <span>{worktree.displayPath}</span>
        <LastCommit unix={worktree.lastCommitUnix} now={now} />
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

interface RepoSectionProps {
  repo: WorktreeRepo
  now: Date
  disabled: boolean
  errors: Record<string, string>
  onPrune: (repo: WorktreeRepo) => void
  onRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onForceRemove: (repo: WorktreeRepo, worktree: Worktree) => void
}

function RepoSection({ repo, now, disabled, errors, onPrune, onRemove, onForceRemove }: RepoSectionProps) {
  const worktrees = repo.worktrees ?? []
  const pruneError = errors[repoKey(repo.path)] ?? ''

  return (
    <section aria-label={repo.name} className="rounded-lg border border-border bg-surface">
      <header className="flex items-center gap-3 border-b border-border px-3 py-2">
        <h2 className="text-sm font-semibold">{repo.name}</h2>
        <span className="text-xs text-muted">{plural(worktrees.length, 'worktree')}</span>
        {repo.hasStale && (
          <button
            type="button"
            disabled={disabled}
            onClick={() => {
              onPrune(repo)
            }}
            className="ml-auto rounded-md border border-border bg-surface px-3 py-1 text-sm font-medium hover:bg-raised disabled:opacity-60"
          >
            Prune
          </button>
        )}
      </header>
      {repo.error !== '' && <p className="px-3 py-2 text-sm text-danger">{repo.error}</p>}
      {pruneError !== '' && (
        <p role="alert" className="px-3 py-2 text-sm text-danger">
          {pruneError}
        </p>
      )}
      {worktrees.length > 0 && (
        <ul>
          {worktrees.map((wt) => (
            <WorktreeRow
              key={wt.path}
              worktree={wt}
              now={now}
              disabled={disabled}
              error={errors[rowKey(wt.path)] ?? ''}
              onRemove={() => {
                onRemove(repo, wt)
              }}
              onForceRemove={() => {
                onForceRemove(repo, wt)
              }}
            />
          ))}
        </ul>
      )}
    </section>
  )
}

// BulkResults reports the outcome of "Remove all clean", listing every worktree that was
// not removed with the reason.
function BulkResults({ results }: { results: RemoveResult[] }) {
  if (results.length === 0) {
    return (
      <p role="status" className="text-sm text-muted">
        There were no clean worktrees to remove.
      </p>
    )
  }

  const removed = results.filter((r) => r.removed).length
  const kept = results.filter((r) => !r.removed)

  return (
    <div role="status" className="flex flex-col gap-1 text-sm">
      <p className={kept.length === 0 ? 'text-success' : 'text-warning'}>
        Removed {String(removed)} of {plural(results.length, 'clean worktree')}.
      </p>
      {kept.length > 0 && (
        <ul className="list-disc pl-5 text-xs text-muted">
          {kept.map((r) => (
            <li key={r.path}>
              {r.displayPath}: {r.reason}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// ConfirmationDialog words the confirmation for the pending destructive action.
function ConfirmationDialog({
  confirmation,
  onConfirm,
  onCancel,
}: {
  confirmation: Confirmation
  onConfirm: () => void
  onCancel: () => void
}) {
  if (confirmation.kind === 'all') {
    return (
      <ConfirmDialog
        title="Remove all clean worktrees?"
        confirmLabel={`Remove ${plural(confirmation.count, 'worktree')}`}
        onConfirm={onConfirm}
        onCancel={onCancel}
      >
        This removes {plural(confirmation.count, 'clean, unlocked worktree')}. Their branches are kept.
      </ConfirmDialog>
    )
  }

  return (
    <ConfirmDialog
      title="Force remove this worktree?"
      confirmLabel="Force remove"
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      {confirmation.worktree.displayPath} has {plural(confirmation.worktree.dirtyCount, 'uncommitted file')}, counting
      untracked files. Force removing it discards them for good. Its branch is kept.
    </ConfirmDialog>
  )
}

// WorktreesView lists the git worktrees of every clone under the clone root and removes
// them safely. It loads when it opens and when its Refresh button is clicked, never on a
// timer: a listing runs git in every clone. Removal follows what the backend allows, and
// the destructive actions (force remove and remove all clean) ask first.
export function WorktreesView({ clock = systemClock }: WorktreesViewProps) {
  const [snapshot, setSnapshot] = useState<WorktreesSnapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  // generation bumps to list again: on Refresh and after a change.
  const [generation, setGeneration] = useState(0)
  const [now, setNow] = useState(clock)
  const [busy, setBusy] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [actionError, setActionError] = useState('')
  const [notice, setNotice] = useState('')
  const [bulkResults, setBulkResults] = useState<RemoveResult[] | null>(null)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)

  useEffect(() => {
    let active = true

    listWorktrees()
      .then((next) => {
        if (active) {
          setSnapshot(next)
          setLoadError('')
          setNow(clock())
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setLoadError(errorMessage(err))
        }
      })
      .finally(() => {
        if (active) {
          setLoading(false)
        }
      })

    return () => {
      active = false
    }
  }, [clock, generation])

  const reload = () => {
    setLoading(true)
    setGeneration((g) => g + 1)
  }

  const resetMessages = () => {
    setErrors({})
    setActionError('')
    setNotice('')
    setBulkResults(null)
  }

  // run performs one change, then lists again so the view shows what git now reports. A
  // failure is shown inline under key.
  const run = (key: string, change: () => Promise<void>, done: string) => {
    resetMessages()
    setBusy(true)

    change()
      .then(() => {
        setNotice(done)
        reload()
      })
      .catch((err: unknown) => {
        setErrors({ [key]: errorMessage(err) })
      })
      .finally(() => {
        setBusy(false)
      })
  }

  const remove = (repo: WorktreeRepo, wt: Worktree) => {
    run(rowKey(wt.path), () => removeWorktree(repo.path, wt.path), `Removed ${wt.displayPath}.`)
  }

  const forceRemove = (repoPath: string, wt: Worktree) => {
    run(rowKey(wt.path), () => forceRemoveWorktree(repoPath, wt.path), `Force removed ${wt.displayPath}.`)
  }

  const prune = (repo: WorktreeRepo) => {
    run(repoKey(repo.path), () => pruneWorktrees(repo.path), `Pruned the stale entries of ${repo.displayPath}.`)
  }

  const removeAllClean = () => {
    resetMessages()
    setBusy(true)

    removeAllCleanWorktrees()
      .then((results) => {
        setBulkResults(results)
        reload()
      })
      .catch((err: unknown) => {
        setActionError(errorMessage(err))
      })
      .finally(() => {
        setBusy(false)
      })
  }

  const confirm = () => {
    if (confirmation === null) {
      return
    }

    setConfirmation(null)

    if (confirmation.kind === 'all') {
      removeAllClean()
    } else {
      forceRemove(confirmation.repoPath, confirmation.worktree)
    }
  }

  const disabled = busy || loading
  const repos = snapshot?.repos ?? []
  const cleanCount = snapshot?.cleanCount ?? 0
  const error = actionError !== '' ? actionError : loadError

  return (
    <section aria-label="Worktrees" className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            resetMessages()
            reload()
          }}
          className="rounded-md border border-border bg-surface px-3 py-1 font-medium hover:bg-raised disabled:opacity-60"
        >
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
        {snapshot !== null && <span className="text-muted">Worktrees of the clones under {snapshot.root}</span>}
        <button
          type="button"
          disabled={disabled || cleanCount === 0}
          onClick={() => {
            setConfirmation({ kind: 'all', count: cleanCount })
          }}
          className="ml-auto rounded-md border border-border bg-surface px-3 py-1 font-medium hover:bg-raised disabled:opacity-60"
        >
          Remove all clean ({String(cleanCount)})
        </button>
      </div>
      {error !== '' && (
        <p role="alert" className="rounded-md border border-danger px-3 py-2 text-sm text-danger">
          {error}
        </p>
      )}
      {notice !== '' && (
        <p role="status" className="text-sm text-success">
          {notice}
        </p>
      )}
      {bulkResults !== null && <BulkResults results={bulkResults} />}
      {snapshot === null
        ? loadError === '' && <p className="text-sm text-muted">Listing the worktrees…</p>
        : repos.length === 0 && (
            <p className="text-sm text-muted">No clone under {snapshot.root} has linked worktrees.</p>
          )}
      {repos.map((repo) => (
        <RepoSection
          key={repo.path}
          repo={repo}
          now={now}
          disabled={disabled}
          errors={errors}
          onPrune={prune}
          onRemove={remove}
          onForceRemove={(r, wt) => {
            setConfirmation({ kind: 'force', repoPath: r.path, worktree: wt })
          }}
        />
      ))}
      {confirmation !== null && (
        <ConfirmationDialog
          confirmation={confirmation}
          onConfirm={confirm}
          onCancel={() => {
            setConfirmation(null)
          }}
        />
      )}
    </section>
  )
}
