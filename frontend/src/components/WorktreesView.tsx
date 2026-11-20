import { useState, type ReactNode } from 'react'

import { whereClones } from '../lib/locations'
import { useWorktreeDetails } from '../lib/useWorktreeDetails'
import { useWorktrees, type WorktreesOptions } from '../lib/useWorktrees'
import { filterWorktrees } from '../lib/worktreeFilter'
import { factsOf, itemFacts, liveSessions } from '../lib/worktreeParts'
import { attentionSort } from '../lib/worktrees'
import type { AgentID, BranchRules, InboxItem, LinkedItem, SessionsSnapshot } from '../platform'
import { ErrorBanner } from './ErrorBanner'
import type { LaunchPlace } from './LaunchControls'
import { BranchResults } from './worktrees/BranchResults'
import { BulkResults } from './worktrees/BulkResults'
import { CleanupHistory } from './worktrees/CleanupHistory'
import { CloneSection } from './worktrees/CloneSection'
import { ConfirmationDialog } from './worktrees/ConfirmationDialog'
import { skeletonClass, worktreeGridClass } from './worktrees/layout'
import { PageHead } from './worktrees/PageHead'
import { ResultCard } from './worktrees/ResultCard'
import { WorktreeLine } from './worktrees/WorktreeLine'

// defaultRules are the branch rules a confirmation starts from until the settings load:
// the app's defaults.
const defaultRules: BranchRules = { own: true, pullRequest: true, merged: true, protected: ['main', 'master'] }

// noItems is an inbox that lists nothing, until the window's loads.
const noItems: readonly InboxItem[] = []

// resultsClass holds what the latest change did, in the head, at most a third of the
// window tall: past that it scrolls, and the clones under the head keep the rest.
const resultsClass = 'flex max-h-[33vh] min-h-0 flex-col gap-2 overflow-y-auto'

// WorktreesViewProps are useWorktrees' options, which the view passes straight through,
// with how to name an agent, how to launch on a worktree's item, the user's branch rules,
// the toolbar's filter, and what the window follows of the inbox and the agent sessions.
export type WorktreesViewProps = WorktreesOptions & {
  // branchRules tick each branch's box in a confirmation to start with.
  branchRules?: BranchRules | undefined
  // agentName names a coding agent for people, as in "Codex".
  agentName?: ((id: AgentID) => string) | undefined
  // launchFor is the "Open in Claude" or "Open in Codex" control for a worktree's item,
  // when the inbox lists it, or null, placed in the row's cells (place).
  launchFor?: ((item: LinkedItem, place: LaunchPlace) => ReactNode) | undefined
  // query is the toolbar's filter (see lib/worktreeFilter), or '' for none; onQuery
  // writes it, as a chip does. Without onQuery, the page has no chips.
  query?: string | undefined
  onQuery?: ((query: string) => void) | undefined
  // sessions are the agent sessions the window follows, which name what works in each
  // worktree, and inboxItems the items the inbox lists, whose checks and drafts a row
  // shows; none by default.
  sessions?: SessionsSnapshot | null | undefined
  inboxItems?: readonly InboxItem[] | undefined
}

// Skeleton holds the place of a clone and its rows until the first listing arrives.
function Skeleton() {
  return (
    <div aria-hidden="true" className="flex flex-col gap-2">
      <span className={`${skeletonClass} mx-1 my-1.5 w-44`} />
      {['w-32', 'w-40', 'w-28'].map((width) => (
        <span key={width} className="flex items-center gap-3 px-3.5 py-1">
          <span className={`${skeletonClass} size-3 rounded-full`} />
          <span className={`${skeletonClass} ${width}`} />
          <span className={`${skeletonClass} w-64 max-w-[40%]`} />
          <span className={`${skeletonClass} ml-auto w-14`} />
        </span>
      ))}
    </div>
  )
}

// toggled is set with key added, or taken out when it is there.
function toggled(set: ReadonlySet<string>, key: string): ReadonlySet<string> {
  const next = new Set(set)

  if (!next.delete(key)) {
    next.add(key)
  }

  return next
}

// WorktreesView lists the git worktrees of every clone in the repository locations and removes
// them safely. It loads when it opens, when Check again is clicked, and when reloadKey
// changes, never on a timer of its own: a listing runs git in every clone. Its head stays put
// over the clones, which scroll under it: the title says how many worktrees there are and
// when they were checked, or, in the same place, how far the listing or the change under way
// has got, so the rows never move; then the cleanups, a chip for each state, the notes the
// listing carries, and what the latest change did.
//
// Each clone has a heading that sticks while its rows scroll, with its main checkout as a
// note, and one line per linked worktree, in attention order: what runs, what needs a look,
// what holds work, then the rest. A row unfolds into its drawer, several at once, and stays
// unfolded across listings; the details a drawer shows are read once a listing, for the whole
// page. Removal follows what the backend allows, and every removal and cleanup asks first,
// showing a box for each worktree and branch it would take. While a change runs, each worktree
// it takes shows it; once done, each shows what became of it until the list is read again.
//
// The toolbar's filter narrows the clones and worktrees shown, and says at the top when
// nothing matches. The bulk cleanups still offer every worktree they would take, since
// their confirmation lists each one, and the recently cleaned-up list is not filtered.
export function WorktreesView({
  agentName = (id) => id,
  launchFor,
  branchRules = defaultRules,
  query = '',
  onQuery,
  sessions = null,
  inboxItems = noItems,
  ...props
}: WorktreesViewProps) {
  const {
    snapshot,
    repos,
    listing,
    now,
    history,
    historyRef,
    checking,
    progress,
    disabled,
    working,
    workOn,
    clearing,
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
  const details = useWorktreeDetails(listing)
  // open are the worktrees whose drawers are unfolded, and folded the clones whose rows are
  // put away, by path: both outlast a listing.
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set())
  const [folded, setFolded] = useState<ReadonlySet<string>>(() => new Set())
  const filter = filterWorktrees(repos, query, { agentName, errors })
  const shown =
    filter === null ? repos.map((repo) => ({ repo, worktrees: repo.worktrees.filter((wt) => !wt.main) })) : filter.repos
  // doing is the change under way, while it runs; the listing after it shows as any does.
  const doing = working !== null && !working.listing ? working : null
  const facts = itemFacts(inboxItems)
  const live = liveSessions(sessions)

  return (
    <section aria-label="Worktrees" className="flex h-full min-h-0 flex-col">
      <PageHead
        snapshot={snapshot}
        repos={repos}
        now={now}
        checking={checking}
        progress={progress}
        loadError={loadError}
        doing={doing}
        disabled={disabled}
        finishedCount={finishedCount}
        cleanCount={cleanCount}
        query={query}
        onQuery={onQuery}
        onRefresh={actions.refresh}
        onCleanUpFinished={actions.askCleanUpFinished}
        onRemoveAllClean={actions.askRemoveAllClean}
        onCleanUpMerged={actions.askCleanUpMerged}
      >
        {(error !== '' || notice !== null || bulk !== null || branchResults !== null) && (
          <div data-results className={resultsClass}>
            {error !== '' && <ErrorBanner>{error}</ErrorBanner>}
            {notice !== null && <ResultCard summary={notice} onDismiss={actions.dismiss} />}
            {bulk !== null && <BulkResults bulk={bulk} onDismiss={actions.dismiss} />}
            {branchResults !== null && <BranchResults results={branchResults} onDismiss={actions.dismiss} />}
          </div>
        )}
      </PageHead>
      <div className="flex min-h-0 flex-1 flex-col gap-3.5 overflow-y-auto px-3 pb-4">
        {snapshot === null
          ? loadError === '' && <Skeleton />
          : repos.length === 0 && (
              <p className="px-2 text-[0.84375rem] text-muted">
                No clone {whereClones(snapshot.roots)} has linked worktrees.
              </p>
            )}
        {repos.length > 0 && filter?.matches === 0 && (
          <p className="px-2 text-[0.84375rem] text-muted">
            No worktree matches “{query.trim()}”.
            {onQuery !== undefined && (
              <>
                {' '}
                <button
                  type="button"
                  onClick={() => {
                    onQuery('')
                  }}
                  className="text-accent hover:underline"
                >
                  Clear the filter
                </button>
              </>
            )}
          </p>
        )}
        {shown.length > 0 && (
          <div className={worktreeGridClass}>
            {shown.map(({ repo, worktrees }) => (
              <CloneSection
                key={repo.path}
                repo={repo}
                shown={worktrees.length}
                filtered={filter !== null}
                open={!folded.has(repo.path)}
                onToggle={() => {
                  setFolded((was) => toggled(was, repo.path))
                }}
                disabled={disabled}
                pruning={doing?.kind === 'prune' && doing.repo === repo.path}
                error={errors.repo(repo.path)}
                onPrune={actions.prune}
              >
                {attentionSort(worktrees).map((wt) => (
                  <WorktreeLine
                    key={wt.path}
                    repo={repo}
                    worktree={wt}
                    open={open.has(wt.path)}
                    onToggle={() => {
                      setOpen((was) => toggled(was, wt.path))
                    }}
                    details={details}
                    listing={listing}
                    disabled={disabled}
                    error={errors.worktree(wt.path)}
                    work={workOn(wt.path)}
                    facts={factsOf(facts, wt.item)}
                    session={live.get(wt.displayPath)}
                    agentName={agentName}
                    launchFor={launchFor}
                    onCleanUp={actions.askCleanUp}
                    onUnlock={actions.unlock}
                    onRemove={actions.askRemove}
                    onForceRemove={actions.askForceRemove}
                    onPrune={actions.prune}
                  />
                ))}
              </CloneSection>
            ))}
          </div>
        )}
        {history !== null && history.length > 0 && (
          <CleanupHistory
            cleanups={history}
            now={now}
            disabled={disabled}
            clearing={clearing}
            error={clearError}
            onClear={actions.clearHistory}
            sectionRef={historyRef}
          />
        )}
      </div>
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
