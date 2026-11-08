import { useEffect, useEffectEvent, useId, useRef, useState, type Ref } from 'react'

import { formatAge, formatAgo } from '../lib/age'
import { formatFull } from '../lib/triage'
import { useMutation } from '../lib/useMutation'
import { useQuery } from '../lib/useQuery'
import {
  cleanUpFinishedWorktrees,
  cleanUpWorktree,
  clearCleanups,
  forceRemoveWorktree,
  listWorktrees,
  openExternal,
  pruneWorktrees,
  recentCleanups,
  removeAllCleanWorktrees,
  removeWorktree,
  unlockWorktree,
  type LinkedItem,
  type RemoveResult,
  type Worktree,
  type WorktreeCleanup,
  type WorktreeRepo,
  type WorktreesSnapshot,
  type WorktreeStage,
} from '../platform'
import { ConfirmDialog } from './ConfirmDialog'
import { quietButtonClass } from './controls'
import { CircleCheckIcon, IssueIcon, PullRequestIcon } from './icons'

export interface WorktreesViewProps {
  // clock returns the current time for the last-commit ages. Keep it stable across renders.
  clock?: () => Date
  // reloadKey lists again whenever it changes, such as after an automatic cleanup.
  reloadKey?: number
  // onSnapshot is told about each listing, so the window can count what needs a look.
  onSnapshot?: (snapshot: WorktreesSnapshot) => void
  // focusHistory scrolls to the recently cleaned-up list once it loads, as when a
  // cleanup toast's View opens the view.
  focusHistory?: boolean
}

// Confirmation is the destructive action waiting for the user's yes.
type Confirmation =
  | { kind: 'force'; repoPath: string; worktree: Worktree }
  | { kind: 'all'; count: number }
  | { kind: 'finished'; count: number }

// Bulk is the outcome of a bulk action, with the words to report it in.
interface Bulk {
  results: RemoveResult[]
  // noun names what the action worked on, such as "clean worktree".
  noun: string
  // verb is what was done to them, such as "Removed".
  verb: string
}

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

const buttonBase = 'rounded-md border px-3 py-1 text-sm font-medium disabled:opacity-60'
const plainButton = `${buttonBase} border-border bg-surface hover:bg-raised`

// stageLabels name each stage the backend reports, with its color.
const stageLabels: Readonly<Partial<Record<WorktreeStage, { text: string; className: string }>>> = {
  'in-use': { text: 'in use', className: 'text-info' },
  finished: { text: 'ready to clean up', className: 'text-success' },
  'needs-review': { text: 'needs a look', className: 'text-warning' },
}

interface WorktreeRowProps {
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

function WorktreeRow({ worktree, now, disabled, error, ...actions }: WorktreeRowProps) {
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

interface RepoSectionProps {
  repo: WorktreeRepo
  now: Date
  disabled: boolean
  errors: Record<string, string>
  onPrune: (repo: WorktreeRepo) => void
  onRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onForceRemove: (repo: WorktreeRepo, worktree: Worktree) => void
  onUnlock: (repo: WorktreeRepo, worktree: Worktree) => void
  onCleanUp: (repo: WorktreeRepo, worktree: Worktree) => void
}

function RepoSection({
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
              error={errors[rowKey(wt.path)] ?? ''}
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

// BulkResults reports the outcome of a bulk action, listing every worktree that was not
// removed, or whose branch was kept, with the reason.
function BulkResults({ bulk }: { bulk: Bulk }) {
  const { results, noun, verb } = bulk

  if (results.length === 0) {
    return (
      <p role="status" className="text-sm text-muted">
        There were no {noun}s to remove.
      </p>
    )
  }

  const removed = results.filter((r) => r.removed).length
  const notes = results.filter((r) => r.reason !== '')

  return (
    <div role="status" className="flex flex-col gap-1 text-sm">
      <p className={removed === results.length ? 'text-success' : 'text-warning'}>
        {verb} {String(removed)} of {plural(results.length, noun)}.
      </p>
      {notes.length > 0 && (
        <ul className="list-disc pl-5 text-xs text-muted">
          {notes.map((r) => (
            <li key={r.path}>
              {r.displayPath}: {r.reason}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// historyPreview is how many cleanups the history shows before "Show more".
const historyPreview = 5

// itemNoun names an item's kind in a sentence.
function itemNoun(item: LinkedItem): string {
  return item.kind === 'pr' ? 'pull request' : 'issue'
}

// CleanupSummary says how a worktree was cleaned up, what went with it, and the commit it
// was at, which is on GitHub.
function CleanupSummary({ cleanup }: { cleanup: WorktreeCleanup }) {
  const who = cleanup.automatic ? 'Cleaned up automatically' : 'Cleaned up by you'
  const after = cleanup.item === null ? '' : ` after the ${itemNoun(cleanup.item)} ${cleanup.item.state}`

  return (
    <p className="text-xs text-muted">
      {who}
      {after}
      {' · '}
      {cleanup.deletedBranch === '' ? (
        'its branch was kept'
      ) : (
        <>
          deleted <span className="font-medium text-text">{cleanup.deletedBranch}</span>
        </>
      )}
      {cleanup.head !== '' && (
        <>
          {' · was at '}
          <code title={cleanup.head} className="font-mono">
            {cleanup.head.slice(0, 7)}
          </code>
        </>
      )}
    </p>
  )
}

// CleanupRow is one cleaned-up worktree: where it was, what it was for, and when it went.
function CleanupRow({ cleanup, now }: { cleanup: WorktreeCleanup; now: Date }) {
  const { item } = cleanup
  // KindIcon marks what the worktree was for, in the color of work that is done.
  const KindIcon = item === null ? CircleCheckIcon : item.kind === 'pr' ? PullRequestIcon : IssueIcon

  return (
    <li className="flex items-start gap-2.5 border-b border-border px-3 py-2.5 last:border-b-0">
      <KindIcon className="mt-0.5 size-4 shrink-0 text-success" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="flex min-w-0 items-baseline gap-2 text-sm">
          <span className="shrink-0 font-medium">{cleanup.repo}</span>
          <span className="shrink-0 text-muted" title={cleanup.displayPath}>
            {cleanup.worktree}
          </span>
          {item !== null && (
            <span className="min-w-0 truncate text-muted">
              <button
                type="button"
                title={`${item.owner}/${item.name}#${String(item.number)}`}
                onClick={() => {
                  openExternal(item.url)
                }}
                className="font-medium text-text hover:underline"
              >
                {item.kind === 'pr' ? 'PR' : 'Issue'} #{item.number}
              </button>{' '}
              {item.title}
            </span>
          )}
        </p>
        <CleanupSummary cleanup={cleanup} />
      </div>
      <time
        dateTime={cleanup.cleanedAt}
        title={formatFull(cleanup.cleanedAt)}
        className="shrink-0 pt-0.5 text-xs text-muted"
      >
        {formatAgo(now, cleanup.cleanedAt)}
      </time>
    </li>
  )
}

interface CleanupHistoryProps {
  cleanups: readonly WorktreeCleanup[]
  now: Date
  disabled: boolean
  error: string
  onClear: () => void
  sectionRef: Ref<HTMLElement>
}

// CleanupHistory lists the worktrees cleaned up most recently, by hand or automatically,
// so a cleanup whose toast went by unseen can still be found. It shows the newest few, and
// the rest on request.
function CleanupHistory({ cleanups, now, disabled, error, onClear, sectionRef }: CleanupHistoryProps) {
  const headingId = useId()
  const [expanded, setExpanded] = useState(false)
  const shown = expanded ? cleanups : cleanups.slice(0, historyPreview)
  const more = cleanups.length - historyPreview

  return (
    <section
      ref={sectionRef}
      aria-labelledby={headingId}
      className="scroll-mt-4 rounded-lg border border-border bg-surface"
    >
      <header className="flex items-center gap-3 border-b border-border px-3 py-2">
        <h2 id={headingId} className="text-sm font-semibold">
          Recently cleaned up
        </h2>
        <span className="text-xs text-muted">Everything in them was on GitHub</span>
        <button type="button" disabled={disabled} onClick={onClear} className={`ml-auto ${quietButtonClass}`}>
          Clear
        </button>
      </header>
      {error !== '' && (
        <p role="alert" className="px-3 py-2 text-sm text-danger">
          {error}
        </p>
      )}
      <ul>
        {shown.map((c) => (
          <CleanupRow key={c.id} cleanup={c} now={now} />
        ))}
      </ul>
      {more > 0 && (
        <div className="border-t border-border px-1.5 py-1">
          <button
            type="button"
            aria-expanded={expanded}
            onClick={() => {
              setExpanded(!expanded)
            }}
            className={quietButtonClass}
          >
            {expanded ? 'Show fewer' : `Show ${String(more)} more`}
          </button>
        </div>
      )}
    </section>
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

  if (confirmation.kind === 'finished') {
    return (
      <ConfirmDialog
        title="Clean up finished worktrees?"
        confirmLabel={`Clean up ${plural(confirmation.count, 'worktree')}`}
        tone="primary"
        onConfirm={onConfirm}
        onCancel={onCancel}
      >
        This removes {plural(confirmation.count, 'worktree')} whose pull request merged or issue closed. Each has no
        uncommitted changes and every commit on GitHub, and each is checked again first. A worktree&apos;s own
        worktree-… branch is deleted with it.
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

// cleanedNotice says what a single clean-up did.
function cleanedNotice(wt: Worktree, res: RemoveResult): string {
  if (res.deletedBranch !== '') {
    return `Cleaned up ${wt.displayPath} and deleted its branch ${res.deletedBranch}.`
  }

  return res.reason === '' ? `Cleaned up ${wt.displayPath}.` : `Cleaned up ${wt.displayPath}; ${res.reason}.`
}

// WorktreesView lists the git worktrees of every clone under the clone root and removes
// them safely. It loads when it opens, when its Refresh button is clicked, and when
// reloadKey changes, never on a timer of its own: a listing runs git in every clone.
// Removal follows what the backend allows, and the destructive actions (force remove and
// the bulk actions) ask first.
export function WorktreesView({
  clock = systemClock,
  reloadKey = 0,
  onSnapshot,
  focusHistory = false,
}: WorktreesViewProps) {
  // listing is the worktrees as git reports them, with when the listing arrived, which
  // the ages are measured against.
  const listing = useQuery(() => listWorktrees().then((snapshot) => ({ snapshot, at: clock() })), [reloadKey])
  // cleanups are the recently cleaned-up worktrees.
  const cleanups = useQuery(() => recentCleanups(), [reloadKey], {
    log: 'could not read the recently cleaned-up worktrees',
  })
  const [opened] = useState(clock)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [notice, setNotice] = useState('')
  const [bulk, setBulk] = useState<Bulk | null>(null)
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null)
  const changer = useMutation((change: () => Promise<string>) => change())
  const bulker = useMutation((action: () => Promise<RemoveResult[]>) => action())
  const clearer = useMutation(() => clearCleanups())
  const historyRef = useRef<HTMLElement>(null)
  // focused records that focusHistory has scrolled once, so later listings stay put.
  const focused = useRef(false)
  const snapshot = listing.data?.snapshot ?? null
  const now = listing.data?.at ?? opened
  // history is the recently cleaned-up worktrees, or null until they have loaded.
  const history = cleanups.data ?? null
  // refreshing is a listing asked for here, by Refresh or after a change, which holds the
  // actions while it runs, as the first listing does. A listing after a background sweep
  // (reloadKey) runs quietly.
  const [refreshing, setRefreshing] = useState(false)
  const loading = refreshing || (listing.data === undefined && listing.error === '')
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
    if (focusHistory && !focused.current && history !== null && history.length > 0) {
      focused.current = true
      historyRef.current?.scrollIntoView({ block: 'start' })
    }
  }, [focusHistory, history])

  const reload = async () => {
    setRefreshing(true)
    void cleanups.refetch()
    await listing.refetch()
    setRefreshing(false)
  }

  const resetMessages = () => {
    setErrors({})
    setNotice('')
    setBulk(null)
    bulker.reset()
    clearer.reset()
  }

  const clearHistory = async () => {
    resetMessages()

    if ((await clearer.run()).ok) {
      cleanups.set(() => [])
    }
  }

  // run performs one change, then lists again so the view shows what git now reports. The
  // change returns the notice to show, and a failure is shown inline under key.
  const run = async (key: string, change: () => Promise<string>) => {
    resetMessages()

    const result = await changer.run(change)

    if (result.ok) {
      setNotice(result.data)
      void reload()
    } else {
      setErrors({ [key]: result.error })
    }
  }

  const remove = (repo: WorktreeRepo, wt: Worktree) => {
    void run(rowKey(wt.path), async () => {
      await removeWorktree(repo.path, wt.path)

      return `Removed ${wt.displayPath}.`
    })
  }

  const forceRemove = (repoPath: string, wt: Worktree) => {
    void run(rowKey(wt.path), async () => {
      await forceRemoveWorktree(repoPath, wt.path)

      return `Force removed ${wt.displayPath}.`
    })
  }

  const unlock = (repo: WorktreeRepo, wt: Worktree) => {
    void run(rowKey(wt.path), async () => {
      await unlockWorktree(repo.path, wt.path)

      return `Unlocked ${wt.displayPath}.`
    })
  }

  const cleanUp = (repo: WorktreeRepo, wt: Worktree) => {
    void run(rowKey(wt.path), async () => cleanedNotice(wt, await cleanUpWorktree(repo.path, wt.path)))
  }

  const prune = (repo: WorktreeRepo) => {
    void run(repoKey(repo.path), async () => {
      await pruneWorktrees(repo.path)

      return `Pruned the stale entries of ${repo.displayPath}.`
    })
  }

  // runBulk performs a bulk action and reports each worktree's outcome.
  const runBulk = async (action: () => Promise<RemoveResult[]>, noun: string, verb: string) => {
    resetMessages()

    const result = await bulker.run(action)

    if (result.ok) {
      setBulk({ results: result.data, noun, verb })
      void reload()
    }
  }

  const confirm = () => {
    if (confirmation === null) {
      return
    }

    setConfirmation(null)

    switch (confirmation.kind) {
      case 'all':
        void runBulk(removeAllCleanWorktrees, 'clean worktree', 'Removed')
        break
      case 'finished':
        void runBulk(cleanUpFinishedWorktrees, 'finished worktree', 'Cleaned up')
        break
      default:
        forceRemove(confirmation.repoPath, confirmation.worktree)
    }
  }

  const disabled = busy || loading
  const repos = snapshot?.repos ?? []
  const cleanCount = snapshot?.cleanCount ?? 0
  const finishedCount = snapshot?.finishedCount ?? 0
  const loadError = listing.error
  const error = bulker.error !== '' ? bulker.error : loadError

  return (
    <section aria-label="Worktrees" className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            resetMessages()
            void reload()
          }}
          className={plainButton}
        >
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
        {snapshot !== null && <span className="text-muted">Worktrees of the clones under {snapshot.root}</span>}
        <span className="ml-auto flex items-center gap-2">
          <button
            type="button"
            disabled={disabled || finishedCount === 0}
            onClick={() => {
              setConfirmation({ kind: 'finished', count: finishedCount })
            }}
            className={plainButton}
          >
            Clean up finished ({String(finishedCount)})
          </button>
          <button
            type="button"
            disabled={disabled || cleanCount === 0}
            onClick={() => {
              setConfirmation({ kind: 'all', count: cleanCount })
            }}
            className={plainButton}
          >
            Remove all clean ({String(cleanCount)})
          </button>
        </span>
      </div>
      {error !== '' && (
        <p role="alert" className="rounded-md border border-danger px-3 py-2 text-sm text-danger">
          {error}
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
          onPrune={prune}
          onRemove={remove}
          onForceRemove={(r, wt) => {
            setConfirmation({ kind: 'force', repoPath: r.path, worktree: wt })
          }}
          onUnlock={unlock}
          onCleanUp={cleanUp}
        />
      ))}
      {history !== null && history.length > 0 && (
        <CleanupHistory
          cleanups={history}
          now={now}
          disabled={disabled}
          error={clearer.error}
          onClear={() => {
            void clearHistory()
          }}
          sectionRef={historyRef}
        />
      )}
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
