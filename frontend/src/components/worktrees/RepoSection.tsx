import type { WorktreeErrors } from '../../lib/useWorktrees'
import type { Worktree, WorktreeRepo } from '../../platform'
import { plainButton, plural } from './shared'
import { WorktreeRow } from './WorktreeRow'

export interface RepoSectionProps {
  repo: WorktreeRepo
  now: Date
  disabled: boolean
  errors: WorktreeErrors
  onPrune: (repo: WorktreeRepo) => void
  onRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onForceRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onUnlock: (repo: WorktreeRepo, worktree: Worktree) => void
  onCleanUp: (repo: WorktreeRepo, worktree: Worktree) => void
}

// RepoSection is one clone and its worktrees, with Prune when git holds stale entries for
// it, and the failure of an action taken on the repository or one of its worktrees.
export function RepoSection({
  repo,
  now,
  disabled,
  errors,
  onPrune,
  onRemove,
  onForceRemove,
  onUnlock,
  onCleanUp,
}: RepoSectionProps) {
  const worktrees = repo.worktrees
  const pruneError = errors.repo(repo.path)

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
            className={`ml-auto ${plainButton}`}
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
              error={errors.worktree(wt.path)}
              onRemove={() => {
                onRemove(repo, wt)
              }}
              onForceRemove={() => {
                onForceRemove(repo, wt)
              }}
              onUnlock={() => {
                onUnlock(repo, wt)
              }}
              onCleanUp={() => {
                onCleanUp(repo, wt)
              }}
            />
          ))}
        </ul>
      )}
    </section>
  )
}
