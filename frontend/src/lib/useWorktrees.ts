import { useEffect, useEffectEvent, useReducer, useRef, useState, type RefObject } from 'react'

import {
  cleanUpFinishedWorktrees,
  cleanUpWorktree,
  clearCleanups,
  forceRemoveWorktree,
  listWorktrees,
  pruneWorktrees,
  recentCleanups,
  removeAllCleanWorktrees,
  removeWorktree,
  unlockWorktree,
  type RemoveResult,
  type Worktree,
  type WorktreeCleanup,
  type WorktreeRepo,
  type WorktreesSnapshot,
} from '../platform'
import { useMutation } from './useMutation'
import { useQuery } from './useQuery'

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
}

// Confirmation is the destructive action waiting for the user's yes.
export type Confirmation =
  | { kind: 'remove'; repoPath: string; worktree: Worktree }
  | { kind: 'force'; repoPath: string; worktree: Worktree }
  | { kind: 'all'; count: number }
  | { kind: 'finished'; count: number }

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
  // notice says what the latest single change did, or ''.
  notice: string
  // bulk is the outcome of the latest bulk action, or null.
  bulk: Bulk | null
  // confirmation is the destructive action waiting for the user's yes, or null.
  confirmation: Confirmation | null
  // cleanCount and finishedCount are how many worktrees each bulk action would take.
  cleanCount: number
  finishedCount: number
  // refresh lists again now.
  refresh: () => void
  // unlock, cleanUp, and prune make one change at once; askRemove, askForceRemove,
  // askRemoveAllClean, and askCleanUpFinished wait for confirm, or cancel.
  askRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  askForceRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  unlock: (repo: WorktreeRepo, worktree: Worktree) => void
  cleanUp: (repo: WorktreeRepo, worktree: Worktree) => void
  prune: (repo: WorktreeRepo) => void
  askRemoveAllClean: () => void
  askCleanUpFinished: () => void
  confirm: () => void
  cancel: () => void
  // clearHistory forgets the recently cleaned-up worktrees.
  clearHistory: () => void
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
  notice: string
  bulk: Bulk | null
  confirmation: Confirmation | null
  // refreshing is a listing asked for here, by Refresh or after a change, which holds the
  // actions while it runs, as the first listing does. A listing after a background sweep
  // (reloadKey) runs quietly.
  refreshing: boolean
}

type Action =
  | { type: 'reset' }
  | { type: 'changed'; notice: string }
  | { type: 'failed'; failure: Failure }
  | { type: 'bulkDone'; bulk: Bulk }
  | { type: 'ask'; confirmation: Confirmation }
  | { type: 'cancel' }
  | { type: 'refreshing' }
  | { type: 'refreshed' }

const initialState: State = { failure: null, notice: '', bulk: null, confirmation: null, refreshing: false }

// reducer applies an action to the state. Every change, bulk action, and refresh starts
// with reset, so only the outcome of the latest one shows.
function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'reset':
      return { ...state, failure: null, notice: '', bulk: null }
    case 'changed':
      return { ...state, notice: action.notice }
    case 'failed':
      return { ...state, failure: action.failure }
    case 'bulkDone':
      return { ...state, bulk: action.bulk }
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

// removedNotice says what a single removal did, with what became of the worktree's branch:
// verb is "Removed" or "Force removed".
function removedNotice(verb: string, wt: Worktree, res: RemoveResult): string {
  if (res.deletedBranch !== '') {
    return `${verb} ${wt.displayPath} and its branch ${res.deletedBranch}.`
  }

  return res.reason === '' ? `${verb} ${wt.displayPath}.` : `${verb} ${wt.displayPath}; ${res.reason}.`
}

// cleanedNotice says what a single clean-up did.
function cleanedNotice(wt: Worktree, res: RemoveResult): string {
  if (res.deletedBranch !== '') {
    return `Cleaned up ${wt.displayPath} and deleted its branch ${res.deletedBranch}.`
  }

  return res.reason === '' ? `Cleaned up ${wt.displayPath}.` : `Cleaned up ${wt.displayPath}; ${res.reason}.`
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
  const changer = useMutation((change: () => Promise<string>) => change())
  const bulker = useMutation((action: () => Promise<RemoveResult[]>) => action())
  const clearer = useMutation(() => clearCleanups())
  const historyRef = useRef<HTMLElement>(null)
  // focused records that focusHistory has scrolled since it was set, so later listings
  // stay put.
  const focused = useRef(false)
  const snapshot = listing.data?.snapshot ?? null
  const history = cleanups.data ?? null
  const loading = state.refreshing || (listing.data === undefined && listing.error === '')
  const busy = changer.pending || bulker.pending

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
    clearer.reset()
  }

  const clearHistory = async () => {
    resetMessages()

    if ((await clearer.run()).ok) {
      cleanups.set(() => [])
    }
  }

  // run performs one change, then lists again. The change returns the notice to show,
  // and a failure is kept with where it happened.
  const run = async (scope: Failure['scope'], path: string, change: () => Promise<string>) => {
    resetMessages()

    const result = await changer.run(change)

    if (result.ok) {
      dispatch({ type: 'changed', notice: result.data })
      void reload()
    } else {
      dispatch({ type: 'failed', failure: { scope, path, message: result.error } })
    }
  }

  const remove = (repoPath: string, wt: Worktree) => {
    void run('worktree', wt.path, async () => removedNotice('Removed', wt, await removeWorktree(repoPath, wt.path)))
  }

  const forceRemove = (repoPath: string, wt: Worktree) => {
    void run('worktree', wt.path, async () =>
      removedNotice('Force removed', wt, await forceRemoveWorktree(repoPath, wt.path)),
    )
  }

  // runBulk performs a bulk action and reports each worktree's outcome.
  const runBulk = async (action: () => Promise<RemoveResult[]>, noun: string, verb: string) => {
    resetMessages()

    const result = await bulker.run(action)

    if (result.ok) {
      dispatch({ type: 'bulkDone', bulk: { results: result.data, noun, verb } })
      void reload()
    }
  }

  const cleanCount = snapshot?.cleanCount ?? 0
  const finishedCount = snapshot?.finishedCount ?? 0

  return {
    snapshot,
    repos: snapshot?.repos ?? [],
    now: listing.data?.at ?? opened,
    history,
    historyRef,
    loading,
    disabled: busy || loading,
    loadError: listing.error,
    error: bulker.error !== '' ? bulker.error : listing.error,
    clearError: clearer.error,
    errors: {
      worktree: (path) => failureAt(state.failure, 'worktree', path),
      repo: (path) => failureAt(state.failure, 'repo', path),
    },
    notice: state.notice,
    bulk: state.bulk,
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
    unlock: (repo, wt) => {
      void run('worktree', wt.path, async () => {
        await unlockWorktree(repo.path, wt.path)

        return `Unlocked ${wt.displayPath}.`
      })
    },
    cleanUp: (repo, wt) => {
      void run('worktree', wt.path, async () => cleanedNotice(wt, await cleanUpWorktree(repo.path, wt.path)))
    },
    prune: (repo) => {
      void run('repo', repo.path, async () => {
        await pruneWorktrees(repo.path)

        return `Pruned the stale entries of ${repo.displayPath}.`
      })
    },
    askRemoveAllClean: () => {
      dispatch({ type: 'ask', confirmation: { kind: 'all', count: cleanCount } })
    },
    askCleanUpFinished: () => {
      dispatch({ type: 'ask', confirmation: { kind: 'finished', count: finishedCount } })
    },
    confirm: () => {
      const { confirmation } = state

      if (confirmation === null) {
        return
      }

      dispatch({ type: 'cancel' })

      switch (confirmation.kind) {
        case 'all':
          void runBulk(removeAllCleanWorktrees, 'clean worktree', 'Removed')
          break
        case 'finished':
          void runBulk(cleanUpFinishedWorktrees, 'finished worktree', 'Cleaned up')
          break
        case 'remove':
          remove(confirmation.repoPath, confirmation.worktree)
          break
        default:
          forceRemove(confirmation.repoPath, confirmation.worktree)
      }
    },
    cancel: () => {
      dispatch({ type: 'cancel' })
    },
    clearHistory: () => {
      void clearHistory()
    },
  }
}
