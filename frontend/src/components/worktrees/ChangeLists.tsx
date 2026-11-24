import { useState } from 'react'

import { formatCount } from '../../lib/format'
import { shortCommit } from '../../lib/git'
import { formatAge, formatAgo, formatFull, unixISO } from '../../lib/time'
import { asReadNow, changesOf, notOnGitHub, readable, type DetailsRead } from '../../lib/useWorktreeDetails'
import {
  copiedOf,
  fileStatus,
  itemRef,
  prOfCopies,
  unpushedKnown,
  unpushedUnknown,
  type Copied,
} from '../../lib/worktreeParts'
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
// path, the folder muted. Force remove's confirmation lists the files it discards with it.
export function FileLine({ file }: { file: WorktreeFile }) {
  const status = fileStatus(file)

  return (
    <li className="flex min-h-4.75 min-w-0 items-center gap-1.5 whitespace-nowrap">
      <span
        title={status.title}
        className={`w-2.75 shrink-0 cursor-help text-center font-mono text-caption font-semibold ${letterTones[status.letter] ?? 'text-muted'}`}
      >
        {status.letter}
      </span>
      <span
        title={status.staged ? 'Staged' : undefined}
        className={`size-1.25 shrink-0 rounded-full ${status.staged ? 'bg-success' : 'bg-transparent'}`}
      />
      <span title={file.path} className="min-w-0 truncate font-mono text-meta">
        <span className="text-muted">{status.dir}</span>
        {status.name}
      </span>
    </li>
  )
}

// fileKey is a changed file's key in a list: its status letters and its path, which a NUL
// keeps apart, since git can list one path twice, as a file removed from the index and kept
// untracked (D and ??).
export function fileKey(file: WorktreeFile): string {
  return `${file.code}\u0000${file.path}`
}

// RestInTerminal says where the rest of a list are when it holds only the first listed of
// count: in Terminal, through command.
export function RestInTerminal({ listed, count, command }: { listed: number; count: number; command: string }) {
  return (
    <>
      {`Only the first ${formatCount(listed)} are listed here; ${formatCount(count - listed)} more are in Terminal (`}
      <code className="font-mono">{command}</code>)
    </>
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
          <FileLine key={fileKey(file)} file={file} />
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
              <RestInTerminal listed={files.length} count={count} command="git status" />
            </span>
          )}
        </div>
      )}
    </>
  )
}

// Changes is the drawer's first column: what a worktree holds that isn't committed, with
// Copy list, which waits while the rest of the list is read. When every one is a file
// missing from its folder, it says so first: its last commit has each one.
export function Changes({ worktree, read }: ColumnProps) {
  const { files, count, missing, current } = changesOf(worktree, read)
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
    body = (
      <>
        {missing === count && (
          <p className="mb-1 text-xs text-muted">
            {count === 1
              ? 'It’s missing from the folder, and its last commit has it, so removing the worktree loses nothing.'
              : 'Each is missing from the folder, and its last commit has it, so removing the worktree loses none.'}
          </p>
        )}
        <FileList worktree={worktree} read={read} />
      </>
    )
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

// CopyMark says how a commit not on GitHub by its commit ID compares with the pushed branch
// its worktree's commits were compared with: = and its copy's short ID for an identical
// copy there, which has its change on GitHub, ≈ and its copy's for an edited one, whose
// change differs, so this commit's isn't; nothing for one with no copy there.
function CopyMark({ commit }: { commit: WorktreeCommit }) {
  if (commit.copy === '') {
    return null
  }

  const identical = commit.copy === 'identical'

  return (
    <span
      title={
        identical
          ? `An identical copy of it is on GitHub: ${commit.copyOid}`
          : `Only an edited copy of it is on GitHub, ${commit.copyOid}: the same author, time, and subject, with another change`
      }
      className={`shrink-0 cursor-help text-meta ${identical ? 'text-muted' : 'text-caution'}`}
    >
      {identical ? '= copied as ' : '≈ edited copy '}
      <code className="font-mono text-caption">{shortCommit(commit.copyOid)}</code>
    </span>
  )
}

// CommitLine is one commit that isn't on GitHub: its short ID, its subject, its copy on the
// pushed branch its worktree's commits were compared with, if it has one (CopyMark), and its
// age. Force remove's confirmation lists the commits it keeps, or loses, with it.
export function CommitLine({ commit }: { commit: WorktreeCommit }) {
  const at = unixISO(commit.unix)

  return (
    <li className="flex min-h-4.75 min-w-0 items-baseline gap-2 whitespace-nowrap">
      <span title={commit.oid} className="shrink-0 font-mono text-caption text-muted">
        {shortCommit(commit.oid)}
      </span>
      <span title={commit.subject} className="min-w-0 flex-1 truncate">
        {commit.subject}
      </span>
      <CopyMark commit={commit} />
      <span title={formatFull(at)} className="shrink-0 text-meta text-muted tabular-nums">
        <RelativeTime iso={at} show={formatAge} />
      </span>
    </li>
  )
}

// skeletonWidths are the widths of the lines that stand in for the commits being read.
const skeletonWidths = ['w-52', 'w-40', 'w-48', 'w-32']

// ReadingCommits stands a line in for each of the first n commits being read, four at most,
// and one at least.
function ReadingCommits({ n }: { n: number }) {
  return (
    <ul aria-busy="true" aria-label="Reading the commits" className="flex flex-col gap-1.5 py-0.5">
      {skeletonWidths.slice(0, Math.min(Math.max(n, 1), skeletonWidths.length)).map((width) => (
        <li key={width} className="flex items-center gap-2">
          <span className={`${skeletonClass} w-12`} />
          <span className={`${skeletonClass} ${width}`} />
        </li>
      ))}
    </ul>
  )
}

// CommitList lists commits that a worktree has, of n, Details listing at most 200, and
// where the rest are.
function CommitList({ commits, n }: { commits: readonly WorktreeCommit[]; n: number }) {
  return (
    <>
      <ul className="flex flex-col text-xs">
        {commits.map((commit) => (
          <CommitLine key={commit.oid} commit={commit} />
        ))}
      </ul>
      {n > commits.length && (
        <p className="mt-1 px-1.5 text-xs text-muted">
          <RestInTerminal listed={commits.length} count={n} command="git log" />
        </p>
      )}
    </>
  )
}

// countedNow reports details that counted the commits not on GitHub.
function countedNow(read: DetailsRead | undefined): boolean {
  return read?.state === 'read' && read.details.unpushed >= 0
}

// Commits are the commits a worktree holds that aren't on GitHub (notOnGitHub), once
// Details has read them, with a line standing in for each of the first four while it reads,
// and where the rest are past the first 200. Commits at its pull request's head are on
// GitHub, however many no remote-tracking branch holds.
function Commits({ worktree, read }: ColumnProps) {
  const known = unpushedKnown(worktree)
  const n = notOnGitHub(worktree, read)
  const { item } = worktree

  if (worktree.prunable || worktree.error !== '') {
    return <p className={columnNoneClass}>Couldn’t be read.</p>
  }

  // While Details reads, a count the listing couldn't make waits too, on one line, rather
  // than say it couldn't count them before Details has tried.
  if ((known > 0 || unpushedUnknown(worktree)) && waiting(worktree, read)) {
    return <ReadingCommits n={known} />
  }

  if (n > 0 && read?.state === 'read' && read.details.commits.length > 0) {
    return <CommitList commits={read.details.commits} n={n} />
  }

  if (n > 0) {
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
      <code className="font-mono text-meta">{base}</code>
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
  const last = worktree.lastCommitUnix > 0 ? unixISO(worktree.lastCommitUnix) : ''
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
            <code className="font-mono text-meta">{worktree.branch}</code>
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
          <code title={worktree.head} className="font-mono text-meta">
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

// refClass sets a pushed branch's name in code in a column's heading, in its own case.
const refClass = 'font-mono font-medium tracking-normal normal-case'

// CopiedCommits are the commits of a worktree that aren't on GitHub by their commit IDs
// but are as identical copies (copied): where the copies are, then each commit once Details
// has read them, marked with its copy (CommitLine), with a line standing in for each of the
// first four while it reads. Once the branch with the copies is gone, or no longer holds
// them all, the app's record of them vouches for them instead, and the commits are listed
// unmarked: Details compares them with the branch as it is now, which says nothing of the
// pull request they went into.
function CopiedCommits({ worktree, read, copied }: ColumnProps & { copied: Copied }) {
  const listed = read?.state === 'read' ? read.details.commits : []
  const commits = copied.recorded ? listed.map((c) => ({ ...c, copy: '' as const, copyOid: '' })) : listed

  return (
    <>
      <p className="mb-1 text-xs text-muted">
        {copied.recorded ? (
          <>
            <code className="font-mono text-meta">{copied.ref}</code>
            {` ${copied.gone ? 'is gone' : 'no longer holds them all'}, but the app recorded each one’s identical copy there, and ${prOfCopies(copied)} has them.`}
          </>
        ) : (
          <>
            {'Each has an identical copy on '}
            <code className="font-mono text-meta">{copied.ref}</code>, under another commit ID.
          </>
        )}
      </p>
      {waiting(worktree, read) ? (
        <ReadingCommits n={copied.n} />
      ) : (
        commits.length > 0 && <CommitList commits={commits} n={copied.n} />
      )}
    </>
  )
}

// NotOnGitHub is the drawer's second column: the commits a worktree holds that aren't on
// GitHub, then its branch, head, and base, against origin's default branch. When those
// commits are on GitHub as identical copies after all (copiedOf), counted as its details
// last counted them (asReadNow), it is headed with the pull request they went into, or the
// branch they're on, and lists them with their copies.
export function NotOnGitHub({ worktree, read }: ColumnProps) {
  const n = worktree.prunable || worktree.error !== '' ? 0 : notOnGitHub(worktree, read)
  const copied = copiedOf(asReadNow(worktree, read))

  return (
    <div className={drawerColumnClass}>
      {copied === null ? (
        <h4 className={columnHeadingClass}>
          Not on GitHub
          {n > 0 && <span className="tracking-normal text-text tabular-nums">{formatCount(n)}</span>}
        </h4>
      ) : (
        <h4 className={columnHeadingClass}>
          Copied to {copied.pr > 0 ? prOfCopies(copied) : <code className={refClass}>{copied.ref}</code>}
        </h4>
      )}
      {copied === null ? (
        <Commits worktree={worktree} read={read} />
      ) : (
        <CopiedCommits worktree={worktree} read={read} copied={copied} />
      )}
      {!worktree.prunable && <BranchLines worktree={worktree} read={read} />}
    </div>
  )
}
