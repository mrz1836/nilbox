import { useState } from 'react'

import { formatCount } from '../../lib/format'
import { shortCommit } from '../../lib/git'
import { formatAge, formatAgo, formatFull } from '../../lib/time'
import { changesOf, readable, type DetailsRead } from '../../lib/useWorktreeDetails'
import { fileStatus, itemRef, unpushedKnown, unpushedUnknown } from '../../lib/worktreeParts'
import type { Worktree, WorktreeCommit, WorktreeFile } from '../../platform'
import { quietButtonClass } from '../controls'
import { AlertIcon, CheckIcon, RingIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { Spinner } from '../Spinner'
import { CopyButton } from './CopyButton'
import { columnHeadingClass, columnNoneClass, drawerColumnClass, skeletonClass } from './layout'

// The drawer's first two columns: the files a worktree has changed, and the commits on it
// that aren't on GitHub, with its branch. Each shows at once what the listing has, and the
// rest once Details is read.

// firstFiles is how many files a drawer lists before Show all, as many as the listing
// sends.
const firstFiles = 12

// waiting reports a worktree whose details are being read, or about to be: its drawer has
// just opened and asked.
export function waiting(wt: Worktree, read: DetailsRead | undefined): boolean {
  return readable(wt) && (read === undefined || read.state === 'reading')
}

// letterTones color each status letter: modified amber, added green, deleted red, renamed
// blue, untracked muted, and a conflict red, spelled out in full so Tailwind finds them.
const letterTones: Readonly<Record<string, string>> = {
  M: 'text-warning',
  A: 'text-success',
  D: 'text-danger',
  R: 'text-info',
  U: 'text-muted',
  C: 'text-danger',
}

// FileLine is one changed file: its status letter, a dot when any of it is staged, and its
// path, the folder muted.
function FileLine({ file }: { file: WorktreeFile }) {
  const status = fileStatus(file)

  return (
    <li className="flex min-h-4.75 min-w-0 items-center gap-1.5 whitespace-nowrap">
      <span
        title={status.title}
        className={`w-2.75 shrink-0 cursor-help text-center font-mono text-[0.6875rem] font-semibold ${letterTones[status.letter] ?? 'text-muted'}`}
      >
        {status.letter}
      </span>
      <span
        title={status.staged ? 'Staged' : undefined}
        className={`size-1.25 shrink-0 rounded-full ${status.staged ? 'bg-success' : 'bg-transparent'}`}
      />
      <span title={file.path} className="min-w-0 truncate font-mono text-[0.71875rem]">
        <span className="text-muted">{status.dir}</span>
        {status.name}
      </span>
    </li>
  )
}

// listText is files as Copy list puts them on the clipboard: each one's status letters and
// its path, one a line.
function listText(files: readonly WorktreeFile[]): string {
  return files.map((f) => `${f.code} ${f.path}`).join('\n')
}

interface ColumnProps {
  worktree: Worktree
  // read is what the window has read of the worktree's details for this listing, or
  // undefined before it asks.
  read: DetailsRead | undefined
}

// FileList is a worktree's changed files: the first twelve at once, then, past twelve, the
// rest Details read (at most 200) on Show all. While Details reads them it says so, and when
// there are more than the drawer has, it says where the rest are.
function FileList({ worktree, read }: ColumnProps) {
  const [all, setAll] = useState(false)
  const { files, count, current } = changesOf(worktree, read)
  const reading = !current && count > files.length && waiting(worktree, read)
  const shown = all ? files : files.slice(0, firstFiles)
  const past = !reading && (all || files.length <= firstFiles) && count > files.length

  return (
    <>
      <ul className="flex flex-col text-xs">
        {shown.map((file) => (
          <FileLine key={file.path} file={file} />
        ))}
      </ul>
      {(reading || files.length > firstFiles || past) && (
        <div className="mt-1 flex flex-wrap items-center gap-0.5 text-xs text-muted">
          {reading && (
            <span role="status" className="inline-flex items-center gap-1.5 px-1.5">
              <Spinner className="size-2.25" />
              {`reading the other ${formatCount(count - files.length)}…`}
            </span>
          )}
          {files.length > firstFiles && (
            <button
              type="button"
              aria-expanded={all}
              onClick={() => {
                setAll(!all)
              }}
              className={quietButtonClass}
            >
              {all
                ? 'Show fewer'
                : files.length === count
                  ? `Show all ${formatCount(count)}`
                  : `Show the first ${formatCount(files.length)}`}
            </button>
          )}
          {past && (
            <span className="px-1.5">
              {`Only the first ${formatCount(files.length)} are listed here; ${formatCount(count - files.length)} more are in Terminal (`}
              <code className="font-mono">git status</code>)
            </span>
          )}
        </div>
      )}
    </>
  )
}

// Changes is the drawer's first column: what a worktree holds that isn't committed, with
// Copy list, which waits while the rest of the list is read.
export function Changes({ worktree, read }: ColumnProps) {
  const { files, count, current } = changesOf(worktree, read)
  const reading = !current && count > files.length && waiting(worktree, read)
  const listed = !worktree.prunable && worktree.error === '' && count > 0
  let body

  if (worktree.prunable) {
    body = <p className={columnNoneClass}>Its folder is gone.</p>
  } else if (worktree.error !== '') {
    body = (
      <p className={columnNoneClass}>
        <AlertIcon className="size-3.5" />
        Couldn’t be read.
      </p>
    )
  } else if (count === 0) {
    body = (
      <p className={columnNoneClass}>
        <CheckIcon className="size-3.5 text-success" />
        Nothing uncommitted.
      </p>
    )
  } else {
    body = <FileList worktree={worktree} read={read} />
  }

  return (
    <div className={drawerColumnClass}>
      <h4 className={columnHeadingClass}>
        Changes
        {count > 0 && <span className="tracking-normal text-text tabular-nums">{formatCount(count)}</span>}
        <span className="flex-1" />
        {listed && files.length > 0 && (
          <CopyButton
            text={listText(files)}
            what="list of files"
            label="Copy list"
            disabled={reading}
            title={reading ? 'The rest of the list is still being read' : undefined}
            className={`${quietButtonClass} tracking-normal normal-case`}
          />
        )}
      </h4>
      {body}
    </div>
  )
}

// CommitLine is one commit that isn't on GitHub: its short ID, its subject, and its age.
function CommitLine({ commit }: { commit: WorktreeCommit }) {
  const at = new Date(commit.unix * 1000).toISOString()

  return (
    <li className="flex min-h-4.75 min-w-0 items-baseline gap-2 whitespace-nowrap">
      <span title={commit.oid} className="shrink-0 font-mono text-[0.6875rem] text-muted">
        {shortCommit(commit.oid)}
      </span>
      <span title={commit.subject} className="min-w-0 flex-1 truncate">
        {commit.subject}
      </span>
      <span title={formatFull(at)} className="shrink-0 text-[0.71875rem] text-muted tabular-nums">
        <RelativeTime iso={at} show={formatAge} />
      </span>
    </li>
  )
}

// skeletonWidths are the widths of the lines that stand in for the commits being read.
const skeletonWidths = ['w-52', 'w-40', 'w-48', 'w-32']

// countedNow reports details that counted the commits not on GitHub.
function countedNow(read: DetailsRead | undefined): read is Extract<DetailsRead, { state: 'read' }> {
  return read?.state === 'read' && read.details.unpushed >= 0
}

// unpushedOf is how many commits on a worktree aren't on GitHub: as its details count them
// now, once read, else as the listing did.
function unpushedOf(wt: Worktree, read: DetailsRead | undefined): number {
  return countedNow(read) ? read.details.unpushed : unpushedKnown(wt)
}

// Commits are the commits a worktree holds that aren't on GitHub, once Details has read
// them, with a line standing in for each of the first four while it reads.
function Commits({ worktree, read }: ColumnProps) {
  const known = unpushedKnown(worktree)
  const { item } = worktree

  if (worktree.prunable || worktree.error !== '') {
    return <p className={columnNoneClass}>Couldn’t be read.</p>
  }

  // While Details reads, a count the listing couldn't make waits too, on one line, rather
  // than say it couldn't count them before Details has tried.
  if ((known > 0 || unpushedUnknown(worktree)) && waiting(worktree, read)) {
    return (
      <ul aria-busy="true" aria-label="Reading the commits" className="flex flex-col gap-1.5 py-0.5">
        {skeletonWidths.slice(0, Math.min(Math.max(known, 1), skeletonWidths.length)).map((width) => (
          <li key={width} className="flex items-center gap-2">
            <span className={`${skeletonClass} w-12`} />
            <span className={`${skeletonClass} ${width}`} />
          </li>
        ))}
      </ul>
    )
  }

  if (read?.state === 'read' && read.details.commits.length > 0) {
    return (
      <ul className="flex flex-col text-xs">
        {read.details.commits.map((commit) => (
          <CommitLine key={commit.oid} commit={commit} />
        ))}
      </ul>
    )
  }

  if (unpushedOf(worktree, read) > 0) {
    return (
      <p className={columnNoneClass}>
        <AlertIcon className="size-3.5 text-warning" />
        Couldn’t list them.
      </p>
    )
  }

  if (unpushedUnknown(worktree) && !countedNow(read)) {
    return (
      <p className={columnNoneClass}>
        <RingIcon className="size-3.5" />
        Couldn’t count them.
      </p>
    )
  }

  const head =
    item !== null && item.headOid !== '' && item.headOid === worktree.head ? ` (${itemRef(item)}’s head)` : ''

  return (
    <p className={columnNoneClass}>
      <CheckIcon className="size-3.5 text-success" />
      {`None: every commit is on GitHub${head}.`}
    </p>
  )
}

// Base is origin's default branch and how far a worktree's HEAD is behind it, once Details
// has read them.
function Base({ worktree, read }: ColumnProps) {
  if (waiting(worktree, read)) {
    return <span role="img" aria-label="Reading the base" className={`${skeletonClass} w-32`} />
  }

  if (read?.state !== 'read' || read.details.base === '') {
    return <span className="text-muted">not known</span>
  }

  const { base, behind } = read.details

  return (
    <>
      <code className="font-mono text-[0.71875rem]">{base}</code>
      {behind >= 0 && (
        <span className="text-muted">{` · ${behind === 0 ? 'up to date' : `${formatCount(behind)} behind`}`}</span>
      )}
    </>
  )
}

// BranchLines are a worktree's branch, which Copy puts on the clipboard, its head and its
// last commit's age, and its base.
function BranchLines({ worktree, read }: ColumnProps) {
  const detached = worktree.detached || worktree.branch === ''
  const last = worktree.lastCommitUnix > 0 ? new Date(worktree.lastCommitUnix * 1000).toISOString() : ''
  const line = 'flex min-w-0 items-baseline gap-2 whitespace-nowrap'
  const key = 'w-14 shrink-0 text-muted'

  return (
    <div className="mt-2.25 flex flex-col gap-0.75 border-t border-dashed border-border/70 pt-1.75 text-xs">
      <div className={line}>
        <span className={key}>Branch</span>
        <span className="min-w-0 truncate">
          {detached ? (
            <span className="text-muted">none: detached</span>
          ) : (
            <code className="font-mono text-[0.71875rem]">{worktree.branch}</code>
          )}
        </span>
        {!detached && (
          <CopyButton
            text={worktree.branch}
            what="branch name"
            ariaLabel="Copy the branch name"
            title="Copy the branch name"
          />
        )}
      </div>
      <div className={line}>
        <span className={key}>Head</span>
        <span className="min-w-0 truncate">
          <code title={worktree.head} className="font-mono text-[0.71875rem]">
            {shortCommit(worktree.head)}
          </code>
          {last !== '' && (
            <span className="text-muted">
              {' · '}
              <RelativeTime iso={last} show={formatAgo} />
            </span>
          )}
        </span>
      </div>
      <div className={line}>
        <span className={key}>Base</span>
        <span className="min-w-0 truncate">
          <Base worktree={worktree} read={read} />
        </span>
      </div>
    </div>
  )
}

// NotOnGitHub is the drawer's second column: the commits a worktree holds that aren't on
// GitHub, then its branch, head, and base, against origin's default branch.
export function NotOnGitHub({ worktree, read }: ColumnProps) {
  const n = worktree.prunable || worktree.error !== '' ? 0 : unpushedOf(worktree, read)

  return (
    <div className={drawerColumnClass}>
      <h4 className={columnHeadingClass}>
        Not on GitHub
        {n > 0 && <span className="tracking-normal text-text tabular-nums">{formatCount(n)}</span>}
      </h4>
      <Commits worktree={worktree} read={read} />
      {!worktree.prunable && <BranchLines worktree={worktree} read={read} />}
    </div>
  )
}
