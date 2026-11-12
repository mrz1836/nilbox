import type { ReactNode } from 'react'

import { useWorktrees, type WorktreesOptions } from '../lib/useWorktrees'
import { formatAgo } from '../lib/time'
import { filterWorktrees } from '../lib/worktreeFilter'
import { progressStep } from '../lib/worktreeProgress'
import type { AgentID, BranchRules, LinkedItem, WorktreesProgress } from '../platform'
import { ErrorBanner } from './ErrorBanner'
import { RelativeTime } from './RelativeTime'
import { Spinner } from './Spinner'
import { BranchResults } from './worktrees/BranchResults'
import { BulkResults } from './worktrees/BulkResults'
import { CleanupHistory } from './worktrees/CleanupHistory'
import { ConfirmationDialog } from './worktrees/ConfirmationDialog'
import { RepoSection } from './worktrees/RepoSection'
import { ResultCard } from './worktrees/ResultCard'
import { plainButton } from './worktrees/shared'

// defaultRules are the branch rules a confirmation starts from until the settings load:
// the app's defaults.
const defaultRules: BranchRules = { own: true, pullRequest: true, merged: true, protected: ['main', 'master'] }

// WorktreesViewProps are useWorktrees' options, which the view passes straight through,
// with how to name an agent, how to launch on a worktree's item, and the user's branch
// rules.
export type WorktreesViewProps = WorktreesOptions & {
  // branchRules tick each branch's box in a confirmation to start with.
  branchRules?: BranchRules | undefined
  // agentName names a coding agent for people, as in "Codex".
  agentName?: ((id: AgentID) => string) | undefined
  // launchFor is the "Open in Claude" or "Open in Codex" control for a worktree's item,
  // when the inbox lists it, or null.
  launchFor?: ((item: LinkedItem) => ReactNode) | undefined
  // query is the toolbar's filter (see lib/worktreeFilter), or '' for none.
  query?: string | undefined
}

// Checking says a listing is under way, whichever started it, and how far it has got:
// the phase in words, with a thin bar for a phase that counts its clones or worktrees.
function Checking({ progress, root }: { progress: WorktreesProgress | null; root: string }) {
  const step = progressStep(progress, root)

  return (
    <span role="status" className="flex min-w-0 items-center gap-2 text-muted">
      <Spinner className="size-3.5" />
      <span className="truncate">{step.text}</span>
      {step.share !== null && (
        <span aria-hidden="true" className="h-1 w-20 shrink-0 overflow-hidden rounded-full bg-border">
          <span
            className="block h-full rounded-full bg-accent transition-[width] duration-200"
            style={{ width: `${String(Math.round(step.share * 100))}%` }}
          />
        </span>
      )}
    </span>
  )
}

// WorktreesView lists the git worktrees of every clone under the clone root and removes
// them safely. It loads when it opens, when its Refresh button is clicked, and when
// reloadKey changes, never on a timer of its own: a listing runs git in every clone.
// Whatever starts a listing, a quiet one after a background sweep included, the toolbar
// says it is checking and how far it has got; otherwise, when the list was checked.
// Removal follows what the backend allows, and every removal and cleanup asks first,
// showing a box for each worktree and branch it would take.
//
// The toolbar's filter narrows the clones and worktrees shown, and says at the top when
// nothing matches. The bulk cleanups still offer every worktree they would take, since
// their confirmation lists each one, and the recently cleaned-up list is not filtered.
export function WorktreesView({
  agentName,
  launchFor,
  branchRules = defaultRules,
  query = '',
  ...props
}: WorktreesViewProps) {
  const {
    snapshot,
    repos,
    now,
    history,
    historyRef,
    loading,
    checking,
    progress,
    disabled,
    loadError,
    error,
    clearError,
    errors,
    notice,
    bulk,
    branchResults,
    confirmation,
    cleanCount,
    finishedCount,
    ...actions
  } = useWorktrees(props)
  const filter = filterWorktrees(repos, query, { agentName, errors })
  const shown = filter === null ? repos.map((repo) => ({ repo, worktrees: repo.worktrees })) : filter.repos

  return (
    <section aria-label="Worktrees" className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <button type="button" disabled={disabled} onClick={actions.refresh} className={plainButton}>
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
        {checking && snapshot !== null ? (
          <Checking progress={progress} root={snapshot.root} />
        ) : (
          snapshot !== null && (
            <span className="text-muted">
              Worktrees of the clones under {snapshot.root} · checked{' '}
              <RelativeTime iso={now.toISOString()} show={formatAgo} />
            </span>
          )
        )}
        <span className="ml-auto flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={disabled}
            onClick={actions.askCleanUpMerged}
            title="Find the local branches in your clones whose pull requests merged"
            className={plainButton}
          >
            Clean up merged branches…
          </button>
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
      {notice !== null && <ResultCard summary={notice} onDismiss={actions.dismiss} />}
      {bulk !== null && <BulkResults bulk={bulk} onDismiss={actions.dismiss} />}
      {branchResults !== null && <BranchResults results={branchResults} onDismiss={actions.dismiss} />}
      {snapshot === null
        ? loadError === '' && (
            <p className="text-sm">
              <Checking progress={progress} root="" />
            </p>
          )
        : repos.length === 0 && (
            <p className="text-sm text-muted">No clone under {snapshot.root} has linked worktrees.</p>
          )}
      {repos.length > 0 && filter?.matches === 0 && (
        <p className="text-sm text-muted">No worktree matches “{query.trim()}”.</p>
      )}
      {shown.map(({ repo, worktrees }) => (
        <RepoSection
          key={repo.path}
          repo={repo}
          worktrees={worktrees}
          now={now}
          disabled={disabled}
          errors={errors}
          onPrune={actions.prune}
          onRemove={actions.askRemove}
          onForceRemove={actions.askForceRemove}
          onUnlock={actions.unlock}
          onCleanUp={actions.askCleanUp}
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
        <ConfirmationDialog
          confirmation={confirmation}
          rules={branchRules}
          now={now}
          onConfirm={actions.confirm}
          onCancel={actions.cancel}
        />
      )}
    </section>
  )
}
