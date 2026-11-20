import { useId, useState, type Ref } from 'react'

import { shortCommit } from '../../lib/git'
import { formatAgo, formatFull } from '../../lib/time'
import { itemRef, kindOf, restoreCommand } from '../../lib/worktreeParts'
import { openExternal, type LinkedItem, type WorktreeCleanup } from '../../platform'
import { quietButtonClass } from '../controls'
import { CheckIcon, ExternalIcon, IssueClosedIcon, MergeIcon, PullRequestIcon, ResetIcon } from '../icons'
import { GroupHeading } from '../scripts/GroupHeading'
import { Spinner } from '../Spinner'
import { CopyButton } from './CopyButton'
import {
  historyActionsClass,
  historyAgeClass,
  historyBranchClass,
  historyCommitClass,
  historyHowClass,
  historyIconClass,
  historyItemClass,
  historyListClass,
  historyRowClass,
  historyWhereClass,
  iconButtonClass,
} from './layout'

// historyPreview is how many cleanups the history shows before "Show more".
const historyPreview = 5

// DoneMark marks what a cleaned-up worktree was for, in the color of work that is done: a
// merged pull request, another pull request, an issue, or nothing.
function DoneMark({ item }: { item: LinkedItem | null }) {
  const icon = 'size-3.25'

  if (item === null) {
    return <CheckIcon className={icon} />
  }

  if (item.kind === 'issue') {
    return <IssueClosedIcon className={icon} />
  }

  return item.state === 'merged' ? <MergeIcon className={icon} /> : <PullRequestIcon className={icon} />
}

// howWords say who cleaned a worktree up: the app, after its pull request merged or its
// issue closed, or the user.
function howWords(cleanup: WorktreeCleanup): string {
  if (!cleanup.automatic) {
    return 'by you'
  }

  const { item } = cleanup

  return `automatic · after ${item === null ? 'it finished' : item.kind === 'pr' ? 'merge' : 'close'}`
}

// CleanupLine is one cleaned-up worktree: where it was, what it was for, who cleaned it up,
// the branch that went with it, the commit it was at, and when it went; then, while
// pointed at or focused, Copy restore command, which brings it back at that commit (on its
// branch again, when that went with it), and Open, which shows what it was for on GitHub.
function CleanupLine({ cleanup, now }: { cleanup: WorktreeCleanup; now: Date }) {
  const { item } = cleanup
  const command = restoreCommand(cleanup)
  // A pull request's head is on GitHub, which is why its worktree could go; when git has
  // pruned it from the clone since, fetching the pull request's head brings it back.
  const refetch =
    item?.kind === 'pr'
      ? ` If git has pruned it since, git fetch origin pull/${String(item.number)}/head brings it back first.`
      : ''

  return (
    <li className={historyRowClass}>
      <span title={item === null ? 'Cleaned up' : kindOf(item, false, now).words} className={historyIconClass}>
        <DoneMark item={item} />
      </span>
      <span title={cleanup.displayPath} className={historyWhereClass}>
        <span className="text-muted">{cleanup.repo} / </span>
        <span className="font-medium">{cleanup.worktree}</span>
      </span>
      <span className={historyItemClass}>
        {item !== null && (
          <>
            <span className="text-muted tabular-nums">#{String(item.number)}</span> {item.title}
          </>
        )}
      </span>
      <span className={historyHowClass}>{howWords(cleanup)}</span>
      <span className={historyBranchClass}>
        {cleanup.deletedBranch === '' ? (
          'its branch was kept'
        ) : (
          <>
            deleted <span className="font-medium text-text">{cleanup.deletedBranch}</span>
          </>
        )}
      </span>
      {cleanup.head !== '' && (
        <span title={`Was at ${cleanup.head}, which is on GitHub`} className={historyCommitClass}>
          {shortCommit(cleanup.head)}
        </span>
      )}
      <time dateTime={cleanup.cleanedAt} title={formatFull(cleanup.cleanedAt)} className={historyAgeClass}>
        {formatAgo(now, cleanup.cleanedAt)}
      </time>
      <span className={historyActionsClass}>
        {command !== '' && (
          <CopyButton
            text={command}
            what="restore command"
            ariaLabel={`Copy the command that restores ${cleanup.worktree}`}
            title={`Copy restore command: ${command}.${refetch}`}
            icon={<ResetIcon className="size-3.5" />}
            className={iconButtonClass}
          />
        )}
        {item !== null && (
          <button
            type="button"
            aria-label={`Open ${itemRef(item)} on GitHub`}
            title={`Open ${itemRef(item)} on GitHub`}
            onClick={() => {
              openExternal(item.url)
            }}
            className={iconButtonClass}
          >
            <ExternalIcon className="size-3.5" />
          </button>
        )}
      </span>
    </li>
  )
}

export interface CleanupHistoryProps {
  cleanups: readonly WorktreeCleanup[]
  now: Date
  disabled: boolean
  // clearing reports the list being cleared.
  clearing?: boolean | undefined
  error: string
  onClear: () => void
  sectionRef: Ref<HTMLElement>
}

// CleanupHistory lists the worktrees cleaned up most recently, by hand or automatically,
// so a cleanup whose toast went by unseen can still be found, and put back: under a
// heading that folds it, with Clear, which forgets the list and removes nothing else, one
// line each, in columns of its own. It shows the newest few, and the rest on request.
export function CleanupHistory({
  cleanups,
  now,
  disabled,
  clearing = false,
  error,
  onClear,
  sectionRef,
}: CleanupHistoryProps) {
  const headingId = useId()
  const [open, setOpen] = useState(true)
  const [expanded, setExpanded] = useState(false)
  const shown = expanded ? cleanups : cleanups.slice(0, historyPreview)
  const more = cleanups.length - historyPreview

  return (
    <section ref={sectionRef} aria-label="Recently cleaned up" className="flex scroll-mt-4 flex-col gap-0.5">
      <GroupHeading
        id={headingId}
        title="Recently cleaned up"
        count={cleanups.length}
        open={open}
        disabled={false}
        onToggle={() => {
          setOpen(!open)
        }}
        note="everything in them was on GitHub"
      >
        <button
          type="button"
          disabled={disabled || clearing}
          title="Forgets this list; removes nothing else"
          onClick={onClear}
          className={quietButtonClass}
        >
          {clearing ? (
            <>
              <Spinner className="size-3" />
              Clearing…
            </>
          ) : (
            'Clear'
          )}
        </button>
      </GroupHeading>
      {error !== '' && (
        <p role="alert" className="px-1 text-sm text-danger">
          {error}
        </p>
      )}
      {open && (
        <>
          <ul aria-label="Recently cleaned up worktrees" className={historyListClass}>
            {shown.map((c) => (
              <CleanupLine key={c.id} cleanup={c} now={now} />
            ))}
          </ul>
          {more > 0 && (
            <div className="pt-0.5 pl-1">
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
        </>
      )}
    </section>
  )
}
