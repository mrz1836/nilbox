import type { WorktreeErrors } from '../../lib/useWorktrees'
import type { ReactNode } from 'react'

import type { AgentID, LinkedItem, Worktree, WorktreeRepo } from '../../platform'
import { plainButton, plural } from './shared'
import { WorktreeRow } from './WorktreeRow'

export interface RepoSectionProps {
  repo: WorktreeRepo
  // worktrees are the repository's worktrees to show, when a filter keeps only some of
  // them; all of them without one.
  worktrees?: readonly Worktree[] | undefined
  now: Date
  disabled: boolean
  errors: WorktreeErrors
  onPrune: (repo: WorktreeRepo) => void
  onRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onForceRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onUnlock: (repo: WorktreeRepo, worktree: Worktree) => void
  onCleanUp: (repo: WorktreeRepo, worktree: Worktree) => void
  // agentName names a coding agent for people.
  agentName?: ((id: AgentID) => string) | undefined
  // launchFor is the launch control for a worktree's item, when the inbox lists it.
  launchFor?: ((item: LinkedItem) => ReactNode) | undefined
}

// RepoSection is one clone and its worktrees, with Prune when git holds stale entries for
// it, and the failure of an action taken on the repository or one of its worktrees. While
// a filter shows only some of its worktrees, the count says how many of them.
export function RepoSection({
  repo,
  worktrees = repo.worktrees,
  now,
  disabled,
  errors,
  onPrune,
  onRemove,
  onForceRemove,
  onUnlock,
  onCleanUp,
  agentName,
  launchFor,
}: RepoSectionProps) {
  const pruneError = errors.repo(repo.path)
  const total = repo.worktrees.length
  const count =
    worktrees.length < total ? `${String(worktrees.length)} of ${plural(total, 'worktree')}` : plural(total, 'worktree')

  return (
    <section aria-label={repo.name} className="rounded-lg border border-border bg-surface">
      <header className="flex items-center gap-3 border-b border-border px-3 py-2">
        <h2 className="text-sm font-semibold">{repo.name}</h2>
        <span className="text-xs text-muted">{count}</span>
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
              agentName={agentName}
              launch={wt.item !== null && !wt.main ? launchFor?.(wt.item) : undefined}
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
