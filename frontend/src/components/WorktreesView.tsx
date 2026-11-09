import type { ReactNode } from 'react'

import { useWorktrees, type WorktreesOptions } from '../lib/useWorktrees'
import type { AgentID, LinkedItem } from '../platform'
import { ErrorBanner } from './ErrorBanner'
import { BulkResults } from './worktrees/BulkResults'
import { CleanupHistory } from './worktrees/CleanupHistory'
import { ConfirmationDialog } from './worktrees/ConfirmationDialog'
import { RepoSection } from './worktrees/RepoSection'
import { plainButton } from './worktrees/shared'

// WorktreesViewProps are useWorktrees' options, which the view passes straight through,
// with how to name an agent and how to launch on a worktree's item.
export type WorktreesViewProps = WorktreesOptions & {
  // agentName names a coding agent for people, as in "Codex".
  agentName?: ((id: AgentID) => string) | undefined
  // launchFor is the "Open in Claude" or "Open in Codex" control for a worktree's item,
  // when the inbox lists it, or null.
  launchFor?: ((item: LinkedItem) => ReactNode) | undefined
}

// WorktreesView lists the git worktrees of every clone under the clone root and removes
// them safely. It loads when it opens, when its Refresh button is clicked, and when
// reloadKey changes, never on a timer of its own: a listing runs git in every clone.
// Removal follows what the backend allows, and the destructive actions (force remove and
// the bulk actions) ask first.
export function WorktreesView({ agentName, launchFor, ...props }: WorktreesViewProps) {
  const {
    snapshot,
    repos,
    now,
    history,
    historyRef,
    loading,
    disabled,
    loadError,
    error,
    clearError,
    errors,
    notice,
    bulk,
    confirmation,
    cleanCount,
    finishedCount,
    ...actions
  } = useWorktrees(props)

  return (
    <section aria-label="Worktrees" className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <button type="button" disabled={disabled} onClick={actions.refresh} className={plainButton}>
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
        {snapshot !== null && <span className="text-muted">Worktrees of the clones under {snapshot.root}</span>}
        <span className="ml-auto flex items-center gap-2">
          <button
            type="button"
            disabled={disabled || finishedCount === 0}
            onClick={actions.askCleanUpFinished}
            className={plainButton}
          >
            Clean up finished ({String(finishedCount)})
          </button>
          <button
            type="button"
            disabled={disabled || cleanCount === 0}
            onClick={actions.askRemoveAllClean}
            className={plainButton}
          >
            Remove all clean ({String(cleanCount)})
          </button>
        </span>
      </div>
      {error !== '' && <ErrorBanner>{error}</ErrorBanner>}
      {snapshot?.sessionsUnknown === true && (
        <p className="text-sm text-muted">
          This system can&apos;t list the running processes, so no worktree shows a session, the automatic cleanup
          removes none, and no worktree can be force-removed.
        </p>
      )}
      {snapshot !== null && snapshot.linkError !== '' && (
        <p className="text-sm text-warning">
          {snapshot.linkError} Worktrees are shown without their pull requests and issues, so none counts as finished.
        </p>
      )}
      {notice !== '' && (
        <p role="status" className="text-sm text-success">
          {notice}
        </p>
      )}
      {bulk !== null && <BulkResults bulk={bulk} />}
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
          onPrune={actions.prune}
          onRemove={actions.remove}
          onForceRemove={actions.askForceRemove}
          onUnlock={actions.unlock}
          onCleanUp={actions.cleanUp}
          agentName={agentName}
          launchFor={launchFor}
        />
      ))}
      {history !== null && history.length > 0 && (
        <CleanupHistory
          cleanups={history}
          now={now}
          disabled={disabled}
          error={clearError}
          onClear={actions.clearHistory}
          sectionRef={historyRef}
        />
      )}
      {confirmation !== null && (
        <ConfirmationDialog confirmation={confirmation} onConfirm={actions.confirm} onCancel={actions.cancel} />
      )}
    </section>
  )
}
