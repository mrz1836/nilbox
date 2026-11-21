import { useId, type ReactNode } from 'react'

import { formatCount } from '../../lib/format'
import { cloneSummary } from '../../lib/worktrees'
import type { WorktreeRepo } from '../../platform'
import { ErrorText } from '../controls'
import { WorktreeIcon } from '../icons'
import { GroupHeading } from '../scripts/GroupHeading'
import { Spinner } from '../Spinner'
import { cloneSectionClass, rowListClass, toneBg, toneText, verbButtonClass } from './layout'

// pruneTitle says what Prune does, and what it never touches.
const pruneTitle =
  'Clears git’s entries for worktrees whose folders are gone; touches no worktree that exists, and no branch'

// CloneName is a clone heading's name: its owner, read from a linked item, muted, and its
// own name.
function CloneName({ owner, name }: { owner: string; name: string }) {
  return (
    <>
      {owner !== '' && <span className="text-muted">{owner}/</span>}
      <span className="font-semibold">{name}</span>{' '}
    </>
  )
}

interface CloneNoteProps {
  repo: WorktreeRepo
  // shown is how many of the clone's worktrees the page shows, and filtered reports a
  // filter that narrows them.
  shown: number
  filtered: boolean
}

// CloneNote is the rest of a clone heading's line: the clone's path, its main checkout as a
// note, the error git gave reading it, and on the right its lanes counted, or, while a
// filter narrows it, how many of its worktrees show. The path gives way under 44rem, and the
// counts under 30rem. The spaces between the parts, which the line's gaps stand in for on
// screen, keep them apart in the heading's name.
function CloneNote({ repo, shown, filtered }: CloneNoteProps) {
  const { note, lanes, count } = cloneSummary(repo)

  return (
    <span className="flex min-w-0 items-center gap-2 text-xs whitespace-nowrap">
      <span title={repo.displayPath} className="min-w-8 shrink truncate font-mono text-meta @max-[44rem]:hidden">
        {repo.displayPath}
      </span>{' '}
      {note !== null && (
        <span
          title={note.title}
          className={`inline-flex shrink-0 cursor-help items-center gap-1.25 ${toneText[note.tone]}`}
        >
          <WorktreeIcon className="size-3" />
          <span className="sr-only">Main checkout: </span>
          {note.text}{' '}
        </span>
      )}
      {repo.error !== '' && (
        <span title={repo.error} className="min-w-0 truncate text-danger">
          {repo.error}{' '}
        </span>
      )}
      <span className="ml-auto inline-flex h-[1.45em] min-w-0 flex-wrap justify-end gap-x-3 gap-y-1 overflow-hidden @max-[30rem]:hidden">
        {filtered ? (
          <span>{`${formatCount(shown)} of ${formatCount(count)}`}</span>
        ) : (
          lanes.map((lane) => (
            <span key={lane.lane} className="inline-flex shrink-0 items-center gap-1.25">
              <span aria-hidden="true" className={`size-1.5 rounded-full ${toneBg[lane.tone]}`} />
              {`${formatCount(lane.n)} ${lane.words} `}
            </span>
          ))
        )}
      </span>
    </span>
  )
}

export interface CloneSectionProps {
  repo: WorktreeRepo
  // shown is how many of the clone's worktrees the page shows, its rows the children; while
  // a filter narrows them (filtered), the heading counts them, and the clone stays open.
  shown: number
  filtered: boolean
  // open reports the clone's rows shown, and onToggle folds or unfolds them.
  open: boolean
  onToggle: () => void
  // disabled holds Prune while a change, or a listing, runs; pruning reports this clone
  // being pruned.
  disabled: boolean
  pruning: boolean
  // error is why a prune of the clone failed, or ''.
  error: string
  onPrune: (repo: WorktreeRepo) => void
  // children are the clone's rows (WorktreeLine), in the order shown.
  children?: ReactNode
}

// CloneSection is one clone on the Worktrees page: a heading that sticks while its rows
// scroll under it, with its owner/name, path, and main checkout as a note, its lanes
// counted, and Prune when git holds entries of worktrees whose folders are gone; then why
// a prune failed; then its rows, a subgrid of the page's grid, so the columns line up from
// one clone to the next.
export function CloneSection({
  repo,
  shown,
  filtered,
  open,
  onToggle,
  disabled,
  pruning,
  error,
  onPrune,
  children,
}: CloneSectionProps) {
  const headingId = useId()
  const { owner, name } = cloneSummary(repo)
  const unfolded = open || filtered

  return (
    <section aria-label={repo.name} className={cloneSectionClass}>
      <GroupHeading
        id={headingId}
        name={<CloneName owner={owner} name={name} />}
        note={<CloneNote repo={repo} shown={shown} filtered={filtered} />}
        open={unfolded}
        disabled={filtered}
        onToggle={onToggle}
        className="col-span-full"
      >
        {repo.hasStale &&
          (pruning ? (
            <span className="inline-flex h-6 items-center gap-1.5 px-2 text-xs font-semibold whitespace-nowrap">
              <Spinner className="size-3" />
              Pruning…
            </span>
          ) : (
            <button
              type="button"
              disabled={disabled}
              title={pruneTitle}
              onClick={() => {
                onPrune(repo)
              }}
              className={verbButtonClass}
            >
              Prune
            </button>
          ))}
      </GroupHeading>
      <ErrorText error={error} className="col-span-full px-1" />
      {unfolded && shown > 0 && (
        <ul aria-label={`Worktrees of ${repo.name}`} className={rowListClass}>
          {children}
        </ul>
      )}
    </section>
  )
}
