import { useEffect, useEffectEvent, useReducer, useRef, useState, type RefObject } from 'react'

import {
  cleanUpFinishedWorktrees,
  cleanUpWorktree,
  clearCleanups,
  deleteBranches,
  forceRemoveWorktree,
  listWorktrees,
  pruneWorktrees,
  recentCleanups,
  removeAllCleanWorktrees,
  removeWorktree,
  unlockWorktree,
  type BranchPick,
  type BranchResult,
  type RemoveResult,
  type Worktree,
  type WorktreePick,
  type WorktreeCleanup,
  type WorktreeRepo,
  type WorktreesSnapshot,
} from '../platform'
import { useMutation } from './useMutation'
import { useQuery } from './useQuery'
import { branchSummary, bulkSummary, failedSummary, shortName, singleSummary, type Summary } from './worktreeSummary'

export interface WorktreesOptions {
  // clock returns the current time for the last-commit ages. Keep it stable across renders.
  clock?: () => Date
  // reloadKey lists again whenever it changes, such as after an automatic cleanup.
  reloadKey?: number
  // onSnapshot is told about each listing, so the window can count what needs a look.
  onSnapshot?: (snapshot: WorktreesSnapshot) => void
  // focusHistory scrolls to the recently cleaned-up list once it loads, as when a
  // cleanup toast's View opens the view. Each time it is set again, the list is scrolled
  // to again.
  focusHistory?: boolean
  // onSummary is told what each change did, or why it failed, as it finishes, so the
  // window can confirm it where the user will see it.
  onSummary?: (summary: Summary) => void
}

// Target is a worktree a bulk action offers to take, in its repository.
export interface Target {
  repoPath: string
  worktree: Worktree
}

// Confirmation is the destructive action waiting for the user's yes: removing, force
// removing, or cleaning up one worktree; removing every clean one or cleaning up every
// finished one, from the targets listed; or deleting the clones' merged branches, which
// the dialog looks up itself.
export type Confirmation =
  | { kind: 'remove' | 'force' | 'cleanUp'; repoPath: string; worktree: Worktree }
  | { kind: 'all' | 'finished'; targets: Target[] }
  | { kind: 'merged' }

// Choice is what the user ticked in a confirmation: the branches to delete with one
// worktree, the worktrees a bulk action takes (each with its branches), or the merged
// branches to delete.
export interface Choice {
  branches?: string[]
  picks?: WorktreePick[]
  branchPicks?: BranchPick[]
}

// Bulk is the outcome of a bulk action, with the words to report it in.
export interface Bulk {
  results: RemoveResult[]
  // noun names what the action worked on, such as "clean worktree".
  noun: string
  // verb is what was done to them, such as "Removed".
  verb: string
}

// WorktreeErrors looks up a single action's failure, which shows inline under the
// worktree or the repository it was on.
export interface WorktreeErrors {
  // worktree is the failure on the worktree at path, or ''.
  worktree: (path: string) => string
  // repo is the failure on the repository at path, such as a prune, or ''.
  repo: (path: string) => string
}

// WorktreesState is the worktrees as the view shows them, and what can be done to them.
export interface WorktreesState {
  // snapshot is the latest listing, or null until the first one arrives.
  snapshot: WorktreesSnapshot | null
  // repos are the listed repositories, none until the first listing arrives.
  repos: readonly WorktreeRepo[]
  // now is when the listing arrived, or when the view opened until then; the ages are
  // measured against it.
  now: Date
  // history is the recently cleaned-up worktrees, or null until they have loaded.
  history: readonly WorktreeCleanup[] | null
  // historyRef goes on the recently cleaned-up list, which focusHistory scrolls to.
  historyRef: RefObject<HTMLElement | null>
  // loading reports a listing that holds the actions: the first one, or one asked for
  // here, by Refresh or after a change.
  loading: boolean
  // disabled holds the actions while a change, or a listing that holds them, runs.
  disabled: boolean
  // loadError is why the latest listing failed, or ''.
  loadError: string
  // error is the failure that holds up the whole view: a bulk action's, else the
  // listing's, or ''.
  error: string
  // clearError is why the history could not be cleared, or ''.
  clearError: string
  errors: WorktreeErrors
  // notice says what the latest single change did, or null.
  notice: Summary | null
  // bulk is the outcome of the latest bulk action, or null.
  bulk: Bulk | null
  // confirmation is the destructive action waiting for the user's yes, or null.
  confirmation: Confirmation | null
  // cleanCount and finishedCount are how many worktrees each bulk action would take.
  cleanCount: number
  finishedCount: number
  // refresh lists again now.
  refresh: () => void
  // branchResults is the outcome of the latest deletion of merged branches, or null.
  branchResults: BranchResult[] | null
  // unlock and prune make one change at once; askRemove, askForceRemove, askCleanUp,
  // askRemoveAllClean, askCleanUpFinished, and askCleanUpMerged wait for confirm, with
  // what the user ticked, or cancel.
  askRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  askForceRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  askCleanUp: (repo: WorktreeRepo, worktree: Worktree) => void
  unlock: (repo: WorktreeRepo, worktree: Worktree) => void
  prune: (repo: WorktreeRepo) => void
  askRemoveAllClean: () => void
  askCleanUpFinished: () => void
  askCleanUpMerged: () => void
  confirm: (choice: Choice) => void
  cancel: () => void
  // clearHistory forgets the recently cleaned-up worktrees.
  clearHistory: () => void
  // dismiss puts away what the latest change did.
  dismiss: () => void
}

// Failure is a single action that failed, kept with where it failed. A repository's main
// worktree shares the repository's path, so scope tells the two apart.
interface Failure {
  scope: 'worktree' | 'repo'
  path: string
  message: string
}

// State is what the view reports and holds besides the listings and the writes.
interface State {
  failure: Failure | null
  notice: Summary | null
  bulk: Bulk | null
  branchResults: BranchResult[] | null
  confirmation: Confirmation | null
  // refreshing is a listing asked for here, by Refresh or after a change, which holds the
  // actions while it runs, as the first listing does. A listing after a background sweep
  // (reloadKey) runs quietly.
  refreshing: boolean
}

type Action =
  | { type: 'reset' }
  | { type: 'changed'; notice: Summary }
  | { type: 'failed'; failure: Failure }
  | { type: 'bulkDone'; bulk: Bulk }
  | { type: 'branchesDone'; results: BranchResult[] }
  | { type: 'ask'; confirmation: Confirmation }
  | { type: 'cancel' }
  | { type: 'refreshing' }
  | { type: 'refreshed' }

const initialState: State = {
  failure: null,
  notice: null,
  bulk: null,
  branchResults: null,
  confirmation: null,
  refreshing: false,
}

// reducer applies an action to the state. Every change, bulk action, and refresh starts
// with reset, so only the outcome of the latest one shows.
function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'reset':
      return { ...state, failure: null, notice: null, bulk: null, branchResults: null }
    case 'changed':
      return { ...state, notice: action.notice }
    case 'failed':
      return { ...state, failure: action.failure }
    case 'bulkDone':
      return { ...state, bulk: action.bulk }
    case 'branchesDone':
      return { ...state, branchResults: action.results }
    case 'ask':
      return { ...state, confirmation: action.confirmation }
    case 'cancel':
      return { ...state, confirmation: null }
    case 'refreshing':
      return { ...state, refreshing: true }
    case 'refreshed':
      return { ...state, refreshing: false }
  }
}

function systemClock(): Date {
  return new Date()
}

// failureAt is the message of failure when it was on the scope and path given, or ''.
function failureAt(failure: Failure | null, scope: Failure['scope'], path: string): string {
  return failure !== null && failure.scope === scope && failure.path === path ? failure.message : ''
}

// targetsOf lists the worktrees a bulk action offers to take: those take keeps.
function targetsOf(repos: readonly WorktreeRepo[], take: (wt: Worktree) => boolean): Target[] {
  return repos.flatMap((repo) => repo.worktrees.filter(take).map((worktree) => ({ repoPath: repo.path, worktree })))
}

// useWorktrees lists the git worktrees of every clone under the clone root, with the
// worktrees cleaned up recently, and makes the changes the view offers. It lists when it
// mounts, when refresh is called, and when reloadKey changes, never on a timer of its own:
// a listing runs git in every clone. After each change it lists again, so the view shows
// what git now reports.
export function useWorktrees({
  clock = systemClock,
  reloadKey = 0,
  onSnapshot,
  focusHistory = false,
  onSummary,
}: WorktreesOptions = {}): WorktreesState {
  // listing is the worktrees as git reports them, with when the listing arrived, which
  // the ages are measured against.
  const listing = useQuery(() => listWorktrees().then((snapshot) => ({ snapshot, at: clock() })), [reloadKey])
  // cleanups are the recently cleaned-up worktrees.
  const cleanups = useQuery(() => recentCleanups(), [reloadKey], {
    log: 'could not read the recently cleaned-up worktrees',
  })
  const [opened] = useState(clock)
  const [state, dispatch] = useReducer(reducer, initialState)
  const changer = useMutation((change: () => Promise<Summary>) => change())
  const bulker = useMutation((action: () => Promise<RemoveResult[]>) => action())
  const brancher = useMutation((picks: BranchPick[]) => deleteBranches(picks))
  const clearer = useMutation(() => clearCleanups())
  const historyRef = useRef<HTMLElement>(null)
  // focused records that focusHistory has scrolled since it was set, so later listings
  // stay put.
  const focused = useRef(false)
  const snapshot = listing.data?.snapshot ?? null
  const repos = snapshot?.repos ?? []
  const history = cleanups.data ?? null
  const loading = state.refreshing || (listing.data === undefined && listing.error === '')
  const busy = changer.pending || bulker.pending || brancher.pending

  // Each listing is reported, to the latest onSnapshot, so a new callback never lists
  // again.
  const report = useEffectEvent((next: WorktreesSnapshot) => {
    onSnapshot?.(next)
  })

  useEffect(() => {
    if (listing.data !== undefined) {
      report(listing.data.snapshot)
    }
  }, [listing.data])

  useEffect(() => {
    if (!focusHistory) {
      focused.current = false

      return
    }

    if (!focused.current && history !== null && history.length > 0) {
      focused.current = true
      historyRef.current?.scrollIntoView({ block: 'start' })
    }
  }, [focusHistory, history])

  const reload = async () => {
    dispatch({ type: 'refreshing' })
    void cleanups.refetch()
    await listing.refetch()
    dispatch({ type: 'refreshed' })
  }

  const resetMessages = () => {
    dispatch({ type: 'reset' })
    bulker.reset()
    brancher.reset()
    clearer.reset()
  }

  const clearHistory = async () => {
    resetMessages()

    if ((await clearer.run()).ok) {
      cleanups.set(() => [])
    }
  }

  // run performs one change, then lists again. The change returns what it did; a
  // failure is kept with where it happened, and failed names the change for its report.
  const run = async (scope: Failure['scope'], path: string, failed: string, change: () => Promise<Summary>) => {
    resetMessages()

    const result = await changer.run(change)

    if (result.ok) {
      dispatch({ type: 'changed', notice: result.data })
      onSummary?.(result.data)
      void reload()
    } else {
      dispatch({ type: 'failed', failure: { scope, path, message: result.error } })
      onSummary?.(failedSummary(failed, result.error))
    }
  }

  // single makes one confirmed change to one worktree, with the branches ticked.
  const single = (confirmation: Extract<Confirmation, { repoPath: string }>, branches: string[]) => {
    const { repoPath, worktree: wt } = confirmation
    const failed = `Couldn’t ${confirmation.kind === 'cleanUp' ? 'clean up' : 'remove'} ${shortName(wt.displayPath)}`

    void run('worktree', wt.path, failed, async () => {
      switch (confirmation.kind) {
        case 'remove':
          return singleSummary('Removed', wt, await removeWorktree(repoPath, wt.path, branches))
        case 'force':
          return singleSummary('Force removed', wt, await forceRemoveWorktree(repoPath, wt.path, branches))
        default:
          return singleSummary('Cleaned up', wt, await cleanUpWorktree(repoPath, wt.path, branches))
      }
    })
  }

  // runBranches deletes the merged branches ticked, reports each outcome, and lists again.
  const runBranches = async (picks: BranchPick[]) => {
    resetMessages()

    const result = await brancher.run(picks)

    if (result.ok) {
      dispatch({ type: 'branchesDone', results: result.data })
      onSummary?.(branchSummary(result.data))
      void reload()
    } else {
      onSummary?.(failedSummary('Couldn’t delete the branches', result.error))
    }
  }

  // runBulk performs a bulk action and reports each worktree's outcome; failed names the
  // action for a report of its failure.
  const runBulk = async (action: () => Promise<RemoveResult[]>, noun: string, verb: string, failed: string) => {
    resetMessages()

    const result = await bulker.run(action)

    if (result.ok) {
      dispatch({ type: 'bulkDone', bulk: { results: result.data, noun, verb } })
      onSummary?.(bulkSummary(result.data, noun, verb))
      void reload()
    } else {
      onSummary?.(failedSummary(failed, result.error))
    }
  }

  const cleanCount = snapshot?.cleanCount ?? 0
  const finishedCount = snapshot?.finishedCount ?? 0

  return {
    snapshot,
    repos,
    now: listing.data?.at ?? opened,
    history,
    historyRef,
    loading,
    disabled: busy || loading,
    loadError: listing.error,
    error: bulker.error !== '' ? bulker.error : brancher.error !== '' ? brancher.error : listing.error,
    clearError: clearer.error,
    errors: {
      worktree: (path) => failureAt(state.failure, 'worktree', path),
      repo: (path) => failureAt(state.failure, 'repo', path),
    },
    notice: state.notice,
    bulk: state.bulk,
    branchResults: state.branchResults,
    confirmation: state.confirmation,
    cleanCount,
    finishedCount,
    refresh: () => {
      resetMessages()
      void reload()
    },
    askRemove: (repo, wt) => {
      dispatch({ type: 'ask', confirmation: { kind: 'remove', repoPath: repo.path, worktree: wt } })
    },
    askForceRemove: (repo, wt) => {
      dispatch({ type: 'ask', confirmation: { kind: 'force', repoPath: repo.path, worktree: wt } })
    },
    askCleanUp: (repo, wt) => {
      dispatch({ type: 'ask', confirmation: { kind: 'cleanUp', repoPath: repo.path, worktree: wt } })
    },
    unlock: (repo, wt) => {
      void run('worktree', wt.path, `Couldn’t unlock ${shortName(wt.displayPath)}`, async () => {
        await unlockWorktree(repo.path, wt.path)

        return {
          title: `Unlocked ${shortName(wt.displayPath)}`,
          detail: 'The lock its ended session left was released, so it can be removed.',
          tone: 'success',
        }
      })
    },
    prune: (repo) => {
      void run('repo', repo.path, `Couldn’t prune ${repo.displayPath}`, async () => {
        await pruneWorktrees(repo.path)

        return {
          title: `Pruned ${repo.displayPath}`,
          detail: 'The entries of worktrees whose folders are gone were cleared.',
          tone: 'success',
        }
      })
    },
    askRemoveAllClean: () => {
      dispatch({ type: 'ask', confirmation: { kind: 'all', targets: targetsOf(repos, (wt) => wt.canRemove) } })
    },
    askCleanUpFinished: () => {
      dispatch({ type: 'ask', confirmation: { kind: 'finished', targets: targetsOf(repos, (wt) => wt.canCleanUp) } })
    },
    askCleanUpMerged: () => {
      dispatch({ type: 'ask', confirmation: { kind: 'merged' } })
    },
    confirm: (choice) => {
      const { confirmation } = state

      if (confirmation === null) {
        return
      }

      dispatch({ type: 'cancel' })

      switch (confirmation.kind) {
        case 'all':
          void runBulk(
            () => removeAllCleanWorktrees(choice.picks ?? []),
            'clean worktree',
            'Removed',
            'Couldn’t remove the clean worktrees',
          )
          break
        case 'finished':
          void runBulk(
            () => cleanUpFinishedWorktrees(choice.picks ?? []),
            'finished worktree',
            'Cleaned up',
            'Couldn’t clean up the finished worktrees',
          )
          break
        case 'merged':
          void runBranches(choice.branchPicks ?? [])
          break
        default:
          single(confirmation, choice.branches ?? [])
      }
    },
    cancel: () => {
      dispatch({ type: 'cancel' })
    },
    clearHistory: () => {
      void clearHistory()
    },
    dismiss: resetMessages,
  }
}
