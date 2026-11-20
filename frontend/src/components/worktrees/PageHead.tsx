import type { ReactNode } from 'react'

import { counted, formatCount } from '../../lib/format'
import { unreadWords, whereClones } from '../../lib/locations'
import { formatAgo } from '../../lib/time'
import type { Working } from '../../lib/useWorktrees'
import { facetQuery, facetsOf } from '../../lib/worktreeParts'
import { progressStep } from '../../lib/worktreeProgress'
import type { WorktreeRepo, WorktreesProgress, WorktreesSnapshot } from '../../platform'
import { quietButtonClass } from '../controls'
import { AlertIcon, ChevronDownIcon, InfoIcon, RefreshIcon, XIcon } from '../icons'
import { MenuButton } from '../Menu'
import { ProgressStatus } from '../ProgressStatus'
import { RelativeTime } from '../RelativeTime'
import { cleanUpButtonClass, toneBg, verbButtonClass } from './layout'
import { cleanupMenuItems } from './menus'

// sessionsUnknownWords say what a system that can't list its processes means for the page.
const sessionsUnknownWords =
  'This system can’t list the running processes, so no worktree shows a session, the automatic cleanup removes none, and no worktree can be force-removed.'

// chipClass is a chip: a pill with its state's dot, its count, and its label, which turns
// the accent while the toolbar's filter is it alone.
const chipClass =
  'inline-flex items-center gap-1.5 rounded-full border border-border bg-background py-0 pr-2.25 pl-2 text-xs leading-5 whitespace-nowrap text-muted transition-colors hover:bg-raised hover:text-text aria-pressed:border-accent/55 aria-pressed:bg-accent/10 aria-pressed:text-text'

// Banner is a note the listing carries, set under the title: amber for what it couldn't
// read (warn), and quiet for what this system can't tell.
function Banner({ tone, children }: { tone: 'warn' | 'quiet'; children: ReactNode }) {
  return (
    <p
      role="note"
      className={`flex items-start gap-2 rounded-lg border px-3 py-2 text-[0.8125rem] ${tone === 'warn' ? 'border-warning/45 bg-warning/9 text-text' : 'border-border bg-surface text-muted'}`}
    >
      {tone === 'warn' ? (
        <AlertIcon className="mt-0.5 size-3.5 shrink-0 text-warning" />
      ) : (
        <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
      )}
      <span className="min-w-0">{children}</span>
    </p>
  )
}

interface CleanupSplitProps {
  finishedCount: number
  cleanCount: number
  disabled: boolean
  onCleanUpFinished: () => void
  onRemoveAllClean: () => void
  onCleanUpMerged: () => void
}

// CleanupSplit is the page's cleanup control: Clean up N finished, in green, while any
// worktree is finished, with a ⌄ beside it for the other cleanups; with none finished, the
// ⌄ alone, as Clean up, whose menu says why.
function CleanupSplit({
  finishedCount,
  cleanCount,
  disabled,
  onCleanUpFinished,
  onRemoveAllClean,
  onCleanUpMerged,
}: CleanupSplitProps) {
  const items = () => cleanupMenuItems({ finishedCount, cleanCount, disabled, onRemoveAllClean, onCleanUpMerged })

  if (finishedCount === 0) {
    return (
      <MenuButton
        label="More cleanups"
        title="More cleanups"
        items={items}
        className={verbButtonClass}
        panelClassName="min-w-60 max-w-80"
      >
        Clean up
        <ChevronDownIcon className="size-3.5" />
      </MenuButton>
    )
  }

  return (
    <span className="inline-flex shrink-0">
      <button
        type="button"
        disabled={disabled}
        title={`Removes the ${counted(finishedCount, 'worktree', 'worktrees')} whose work is done and on GitHub; asks first, with a box for each`}
        onClick={onCleanUpFinished}
        className={`${cleanUpButtonClass} rounded-r-none`}
      >
        {`Clean up ${formatCount(finishedCount)} finished`}
      </button>
      <MenuButton
        label="More cleanups"
        title="More cleanups"
        items={items}
        className={`${cleanUpButtonClass} -ml-px rounded-l-none px-1.25`}
        panelClassName="min-w-60 max-w-80"
      >
        <ChevronDownIcon className="size-3.5" />
      </MenuButton>
    </span>
  )
}

// cloneTitle is the meta's tooltip: where the clones are, each location on a line of its
// own when there are several.
function cloneTitle(roots: readonly string[]): string {
  const where = `The clones ${whereClones(roots)}`

  return roots.length > 1 ? `${where}:\n${roots.join('\n')}` : where
}

export interface PageHeadProps {
  // snapshot is the latest listing, or null until the first one arrives; repos are its
  // clones, every one of them whatever the filter.
  snapshot: WorktreesSnapshot | null
  repos: readonly WorktreeRepo[]
  // now is when the listing arrived, which the meta says it was checked.
  now: Date
  // checking reports a listing under way, and progress how far it has got; loadError is
  // why the latest failed, or '', and holds the first listing's progress back.
  checking: boolean
  progress: WorktreesProgress | null
  loadError: string
  // doing is the change under way while it runs, or null.
  doing: Working | null
  // disabled holds every change while one, or a listing it was asked for, runs.
  disabled: boolean
  // finishedCount and cleanCount are how many worktrees Clean up finished and Remove clean
  // would take.
  finishedCount: number
  cleanCount: number
  // query is the toolbar's filter, which a chip writes through onQuery; without onQuery,
  // there are no chips.
  query: string
  onQuery?: ((query: string) => void) | undefined
  onRefresh: () => void
  onCleanUpFinished: () => void
  onRemoveAllClean: () => void
  onCleanUpMerged: () => void
  // children are what the latest change did, or why it failed, under the banners.
  children?: ReactNode
}

// PageHead is the head of the Worktrees page, which stays put while the clones scroll under
// it: the title, how many worktrees in how many clones and when they were checked (or, in
// its place, the progress of the listing or the change under way, so the rows never move),
// Check again, and the cleanup control, once there's a listing (or a failed one) to clean up
// from; then a chip for each state any worktree is in, which filters by it; then the notes
// the listing carries, and what the latest change did.
export function PageHead({
  snapshot,
  repos,
  now,
  checking,
  progress,
  loadError,
  doing,
  disabled,
  finishedCount,
  cleanCount,
  query,
  onQuery,
  onRefresh,
  onCleanUpFinished,
  onRemoveAllClean,
  onCleanUpMerged,
  children,
}: PageHeadProps) {
  const linked = repos.reduce((n, repo) => n + repo.worktrees.filter((wt) => !wt.main).length, 0)
  const chips = onQuery === undefined ? [] : facetsOf(repos, query)
  let meta: ReactNode = null

  if (doing !== null) {
    meta = <ProgressStatus step={{ text: doing.text, share: null }} />
  } else if (checking || (snapshot === null && loadError === '')) {
    meta = <ProgressStatus step={progressStep(progress, snapshot === null ? '' : whereClones(snapshot.roots))} />
  } else if (snapshot !== null) {
    meta = (
      <span title={cloneTitle(snapshot.roots)} className="truncate">
        {`${counted(linked, 'worktree', 'worktrees')} in ${counted(repos.length, 'clone', 'clones')} · checked `}
        <RelativeTime iso={now.toISOString()} show={formatAgo} />
      </span>
    )
  }

  return (
    <div className="flex flex-col gap-2 px-4 pt-3.5 pb-2.5">
      <div className="flex min-w-0 items-center gap-2 text-xs whitespace-nowrap text-muted">
        <h2 className="text-sm font-semibold text-text">Worktrees</h2>
        <span className="flex min-w-0 items-center overflow-hidden">{meta}</span>
        <button
          type="button"
          disabled={disabled}
          title={disabled ? (doing !== null ? 'A change is under way' : 'A listing is under way') : undefined}
          onClick={onRefresh}
          className={`shrink-0 ${quietButtonClass}`}
        >
          <RefreshIcon className="size-3" />
          Check again
        </button>
        <span className="flex-1" />
        {(snapshot !== null || loadError !== '') && (
          <CleanupSplit
            finishedCount={finishedCount}
            cleanCount={cleanCount}
            disabled={disabled}
            onCleanUpFinished={onCleanUpFinished}
            onRemoveAllClean={onRemoveAllClean}
            onCleanUpMerged={onCleanUpMerged}
          />
        )}
      </div>
      {onQuery !== undefined && chips.length > 0 && (
        <div role="group" aria-label="Filter by state" className="flex min-h-5.5 flex-wrap items-center gap-1.5">
          {chips.map((chip) => (
            <button
              key={chip.key}
              type="button"
              aria-pressed={chip.on}
              title={chip.title}
              onClick={() => {
                onQuery(facetQuery(query, chip.key))
              }}
              className={chipClass}
            >
              <span aria-hidden="true" className={`size-1.75 shrink-0 rounded-full ${toneBg[chip.tone]}`} />
              <b className="font-semibold text-text tabular-nums">{formatCount(chip.n)}</b>
              {` ${chip.label}`}
              {chip.on && <XIcon className="size-3" />}
            </button>
          ))}
        </div>
      )}
      {snapshot !== null && snapshot.linkError !== '' && (
        <Banner tone="warn">
          {snapshot.linkError} Worktrees are shown without their pull requests and issues, so none counts as finished.
        </Banner>
      )}
      {snapshot?.sessionsUnknown === true && <Banner tone="quiet">{sessionsUnknownWords}</Banner>}
      {snapshot !== null && snapshot.unread.length > 0 && <Banner tone="warn">{unreadWords(snapshot.unread)}</Banner>}
      {children}
    </div>
  )
}
