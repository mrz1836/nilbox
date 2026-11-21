import { Fragment, type ReactNode } from 'react'

import { counted, formatCount } from '../../lib/format'
import {
  bumpHint,
  bumpKind,
  cloneNotes,
  commitChecksText,
  readiness,
  readinessCounts,
  readinessWords,
  releasable,
  shownNotes,
  waveAt,
  type CloneNote,
  type Readiness,
} from '../../lib/release'
import { formatAgo } from '../../lib/time'
import type { CommitChecks, ReleaseCandidate } from '../../platform'
import { edgeBar } from '../card/CardParts'
import { AlertIcon, ArrowDownIcon, CheckIcon, PinIcon, RingIcon, WorktreeIcon, XIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { StatusDot } from '../StatusDot'

// The Release page's rows: one line per repository, in columns its whole group shares.
// The list is the grid and each row a subgrid of it, so the columns line up down the
// group, and the headline takes the room the rest leave. In a list under 56rem the
// headline and the clone's notes move to a second line, under the repository, and in one
// under 36rem the version and the age join them there. The list's width is what counts,
// not the window's: the list is the container the rows ask.

// releaseListClass is a group's list: the container its rows' layout follows, and the grid
// of their nine columns: the readiness edge, the checks, the repository, the version, the
// count, the headline, the clone's notes, the age, and the action. The edge's column is
// 0.4375rem, not the edge's own 0.1875rem: a subgrid adds its padding to the items in its
// outer columns as margin, so the row's 0.25rem of left padding takes the rest, leaving the
// edge its width, inset from the row's rounded corner.
export const releaseListClass =
  '@container grid grid-cols-[0.4375rem_auto_fit-content(22rem)_auto_auto_minmax(0,1fr)_fit-content(12rem)_auto_auto] gap-x-3'

// rowButtonClass is a row's Release… or Unmute: quiet muted words until the row is hovered
// or holds the focus, when it is drawn as a button, so a column of them stays calm and
// each is still found where it always is.
const rowButtonClass =
  'inline-flex items-center rounded-md border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap text-muted transition-colors hover:bg-raised group-hover/row:border-border group-hover/row:bg-surface group-hover/row:text-text group-focus-within/row:border-border group-focus-within/row:bg-surface group-focus-within/row:text-text focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-50'

// rowClass is a row: a subgrid of the list's columns, every cell placed in it by its own
// classes, and in a narrow list a grid of its own, five columns wide.
const rowClass =
  'group/row relative col-span-full grid grid-cols-subgrid items-center rounded-md py-1 pr-2 pl-1 hover:bg-raised/60 focus-within:bg-raised/60 @max-4xl:py-1.5 @max-xl:grid-cols-[0.1875rem_auto_minmax(0,1fr)_auto_auto] @max-xl:gap-x-2'

// metaClass holds the version, the headline, the clone's notes, and the age: no box of its
// own while they sit in the row's columns, and in a narrow list the second line they share.
const metaClass =
  'contents @max-xl:col-[3/-1] @max-xl:row-start-2 @max-xl:flex @max-xl:min-w-0 @max-xl:items-center @max-xl:gap-2'

// headlineClass is the headline's cell, which a muted row gives its folder.
const headlineClass =
  'col-start-6 row-start-1 flex min-w-0 items-center gap-1.5 text-xs text-muted @max-4xl:col-[3/7] @max-4xl:row-start-2 @max-xl:flex-1'

// actionClass is the cell of a row's Release… or Unmute, at its end.
const actionClass = 'col-start-9 row-start-1 justify-self-end @max-xl:col-start-5'

// readinessBar colors a row's edge by its readiness, as the inbox's cards color theirs: red
// for failing checks, amber for a clone that blocks, a pulse while checks run, green when
// ready, and muted without checks, which never reads as ready. Each class is spelled out
// in full so Tailwind finds it.
const readinessBar: Readonly<Record<Readiness, string>> = {
  failing: edgeBar.blocked,
  blocked: edgeBar.urgent,
  running: 'bg-muted/60 motion-safe:animate-pulse',
  ready: edgeBar.ready,
  none: edgeBar.waiting,
}

// readinessDot colors a readiness's dot in a group's summary as readinessBar colors the
// edges below it, so the summary reads as their legend.
const readinessDot: Readonly<Record<Readiness, string>> = {
  failing: 'bg-danger',
  blocked: 'bg-warning',
  running: 'bg-muted/60',
  ready: 'bg-success',
  none: 'bg-muted/30',
}

// running marks checks still running with the dot a working session has, a ring swelling
// out of it in the warning colour, in an icon's box.
const running = (
  <span data-checks-running className="inline-flex size-3.5 shrink-0 items-center justify-center">
    <StatusDot color="bg-warning" ping />
  </span>
)

// checksIcons mark the checks on a commit: passed, running, failing, or none at all.
const checksIcons: Readonly<Record<CommitChecks, ReactNode>> = {
  SUCCESS: <CheckIcon className="size-3.5 shrink-0 text-success" />,
  PENDING: running,
  EXPECTED: running,
  FAILURE: <XIcon className="size-3.5 shrink-0 text-danger" />,
  ERROR: <XIcon className="size-3.5 shrink-0 text-danger" />,
  '': <RingIcon className="size-3.5 shrink-0 text-muted" />,
}

// bumpChips word and color the bump a row's commits suggest: breaking for a major one,
// feat for a minor one.
const bumpChips: Readonly<Record<'major' | 'minor', { text: string; className: string }>> = {
  major: { text: 'breaking', className: 'border-caution/40 bg-caution/10 text-caution' },
  minor: { text: 'feat', className: 'border-info/40 bg-info/10 text-info' },
}

// noWave is the repositories sharing a headline when the row is given none.
const noWave: readonly string[] = []

// repoParts splits "owner/name" after its first slash, the owner keeping the slash; a
// repository without one is all name.
function repoParts(repo: string): { owner: string; name: string } {
  const cut = repo.indexOf('/') + 1

  return { owner: repo.slice(0, cut), name: repo.slice(cut) }
}

// RepoCell is a row's repository: a muted owner and the name in medium weight, cut short
// with the whole on hover, and the pin after it when a rule pins it, so the names still
// line up.
function RepoCell({ repo, pinnedWith }: { repo: string; pinnedWith: string }) {
  const { owner, name } = repoParts(repo)

  return (
    <span className="col-start-3 row-start-1 flex min-w-0 items-center gap-1.5 text-sm">
      <span className="truncate" title={repo}>
        <span className="text-muted">{owner}</span>
        <span className="font-medium">{name}</span>
      </span>
      {pinnedWith !== '' && (
        <span
          role="img"
          aria-label={`Pinned with ${pinnedWith}`}
          title={`Pinned with ${pinnedWith}: unpin it in Settings → Repositories`}
          className="inline-flex shrink-0 text-accent"
        >
          <PinIcon filled className="size-3.5" />
        </span>
      )}
    </span>
  )
}

// ChecksCell is the checks on a row's newest commit, as an icon, its words in its tooltip
// and for a screen reader.
function ChecksCell({ checks }: { checks: CommitChecks }) {
  const words = commitChecksText(checks)

  return (
    <span title={words} className="col-start-2 row-start-1 inline-flex">
      {checksIcons[checks]}
      <span className="sr-only">{words}</span>
    </span>
  )
}

// CountCell is how many commits came since an unreleased row's version, +? when GitHub
// couldn't compare them, and the bump the newest of them suggest, as a chip.
function CountCell({ candidate }: { candidate: ReleaseCandidate }) {
  const { ahead, version, defaultBranch } = candidate
  const headlines = candidate.commits.map((c) => c.headline)
  const kind = bumpKind(headlines)
  const branch = defaultBranch === '' ? 'the default branch' : defaultBranch

  return (
    <span className="col-start-5 row-start-1 flex items-center gap-1.5 text-xs whitespace-nowrap tabular-nums @max-xl:col-start-4 @max-xl:justify-self-end">
      {ahead > 0 ? (
        <span title={`${counted(ahead, 'commit', 'commits')} since ${version}`} className="font-medium">
          {`+${formatCount(ahead)}`}
        </span>
      ) : (
        <span
          title={`GitHub couldn’t compare ${version} with ${branch}, so how many commits came since isn’t known`}
          className="text-muted"
        >
          +?
        </span>
      )}
      {kind !== '' && (
        <span
          title={bumpHint(headlines)}
          className={`rounded-full border px-1.5 text-caption leading-4 font-semibold ${bumpChips[kind].className}`}
        >
          {bumpChips[kind].text}
        </span>
      )}
    </span>
  )
}

// WaveBadge marks a headline the newest commit of waveAt or more listed repositories
// share, as ×N, naming them in its tooltip.
function WaveBadge({ wave }: { wave: readonly string[] }) {
  const n = formatCount(wave.length)

  return (
    <span
      title={`${String(wave.length)} of the listed repositories have this as their newest commit: ${wave.join(', ')}`}
      className="shrink-0 rounded-full border border-border bg-raised px-1.5 text-caption leading-4 font-semibold tabular-nums"
    >
      <span aria-hidden="true">{`×${n}`}</span>
      <span className="sr-only">{`, the newest commit of ${n} listed repositories`}</span>
    </span>
  )
}

// NoteMark is one note about a row's clone, its sentence in its tooltip: a blocker in
// amber, cut short; the commits it lacks, with an arrow down; or the branch it's on, read
// as "on" it and cut short.
function NoteMark({ note }: { note: CloneNote }) {
  if (note.blocks) {
    return (
      <span title={note.title} className="inline-flex min-w-0 items-center gap-1 font-medium text-warning">
        <AlertIcon className="size-3.5 shrink-0" />
        <span className="truncate">{note.text}</span>
      </span>
    )
  }

  if (note.kind === 'behind') {
    return (
      <span title={note.title} className="inline-flex shrink-0 items-center gap-1 text-muted">
        <ArrowDownIcon className="size-3.5" />
        {note.text}
      </span>
    )
  }

  return (
    <span title={note.title} className="inline-flex min-w-12 items-center gap-1 text-muted">
      <WorktreeIcon className="size-3.5 shrink-0" />
      <span className="sr-only">on </span>
      <span className="max-w-36 truncate">{note.text}</span>
    </span>
  )
}

// NotesCell is what matters about a row's clone (shownNotes): its blockers alone while it
// has any, else the commits it lacks, then its branch. A comma between them, for a screen
// reader, keeps them apart when read as text; the gap does on screen.
function NotesCell({ notes }: { notes: readonly CloneNote[] }) {
  return (
    <span className="col-start-7 row-start-1 flex min-w-0 items-center gap-2.5 overflow-hidden text-xs whitespace-nowrap @max-4xl:col-[7/9] @max-4xl:row-start-2 @max-4xl:justify-end @max-xl:max-w-1/2 @max-xl:shrink-0 @max-xl:justify-start">
      <span className="sr-only">Your clone: </span>
      {notes.map((n, i) => (
        <Fragment key={`${n.kind}:${n.text}`}>
          {i > 0 && <span className="sr-only">, </span>}
          <NoteMark note={n} />
        </Fragment>
      ))}
    </span>
  )
}

export interface CandidateRowProps {
  candidate: ReleaseCandidate
  // pinnedWith is the repository or org whose rule pins the repository, or '' when none
  // does.
  pinnedWith?: string
  // wave is the listed repositories whose newest commit has this row's headline, its own
  // among them; waveAt or more show as a ×N badge after it.
  wave?: readonly string[]
  onRelease: () => void
}

// selectingText reports a click that ends a drag across text, which selects the words
// rather than acting on the row they're in.
function selectingText(): boolean {
  return (window.getSelection()?.toString() ?? '') !== ''
}

// CandidateRow is one repository on one line: its readiness as an edge, the checks on its
// newest commit, its name and pin, its newest version, how many commits came since and
// the bump they suggest, that commit's headline (with ×N when a wave of repositories
// shares it), what matters about its clone, its age, and Release… where there is
// something to release. A repository that can't be released here says why in place of
// its headline, with no checks. A row with Release… opens its sheet from a click anywhere
// on it, as Release… does: a pointer's shortcut for the button, which the keyboard and a
// screen reader reach. Nothing covers the row's cells to catch the click, so each keeps
// its tooltip, and a drag that selects the row's words opens nothing, so they can still
// be copied.
export function CandidateRow({ candidate, pinnedWith = '', wave = noWave, onRelease }: CandidateRowProps) {
  const { group, repo, headline, headAt } = candidate
  const canRelease = releasable(group)
  const ready = readiness(candidate)
  const notes = shownNotes(cloneNotes(candidate.clone, candidate.defaultBranch))

  return (
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions -- the row's click is a pointer's shortcut for its Release… button, which the keyboard and a screen reader reach
    <li
      className={canRelease ? `${rowClass} cursor-pointer` : rowClass}
      onClick={
        canRelease
          ? () => {
              if (!selectingText()) {
                onRelease()
              }
            }
          : undefined
      }
    >
      <span
        aria-hidden="true"
        data-readiness={canRelease ? ready : undefined}
        className={`col-start-1 row-start-1 my-0.5 self-stretch rounded-full @max-4xl:row-end-3 ${canRelease ? readinessBar[ready] : 'bg-transparent'}`}
      />
      {group !== 'cantRelease' && <ChecksCell checks={candidate.checks} />}
      <RepoCell repo={repo} pinnedWith={pinnedWith} />
      {group === 'unreleased' && <CountCell candidate={candidate} />}
      <div className={metaClass}>
        <span className="col-start-4 row-start-1 font-mono text-xs whitespace-nowrap text-muted">
          {candidate.version === '' ? 'no version yet' : candidate.version}
        </span>
        <span className={headlineClass}>
          {group === 'cantRelease' ? (
            <span className="truncate text-text" title={candidate.reason}>
              {candidate.reason}
            </span>
          ) : (
            <>
              <span className="truncate" title={headline}>
                {headline}
              </span>
              {wave.length >= waveAt && <WaveBadge wave={wave} />}
            </>
          )}
        </span>
        {notes.length > 0 && <NotesCell notes={notes} />}
        {headAt !== '' && (
          <span className="col-start-8 row-start-1 text-right text-xs whitespace-nowrap text-muted tabular-nums">
            <RelativeTime iso={headAt} show={formatAgo} />
          </span>
        )}
      </div>
      {canRelease && (
        <span className={actionClass}>
          <button
            type="button"
            aria-label={`Release ${repo}…`}
            onClick={(e) => {
              // The row's own click would open the sheet a second time.
              e.stopPropagation()
              onRelease()
            }}
            className={rowButtonClass}
          >
            Release…
          </button>
        </span>
      )}
    </li>
  )
}

export interface MutedRowProps {
  candidate: ReleaseCandidate
  // target is the repository or org whose rule mutes it, or '' once it is unmuted and
  // read again.
  target: string
  busy: boolean
  onUnmute: () => void
}

// MutedRow is a muted repository on the same columns: its name, its clone's folder in the
// headline's place, and Unmute in Release…'s, which unmutes the repository or org that
// mutes it, so it is read again and listed in its own group. One unmuted already says it
// is being read again.
export function MutedRow({ candidate, target, busy, onUnmute }: MutedRowProps) {
  return (
    <li className={rowClass}>
      <RepoCell repo={candidate.repo} pinnedWith="" />
      <div className={metaClass}>
        <span className={headlineClass}>
          <span className="truncate font-mono" title={candidate.dir}>
            {candidate.dir}
          </span>
        </span>
      </div>
      <span className={actionClass}>
        {target === '' ? (
          <span className="text-xs whitespace-nowrap text-muted">Unmuted · reading it again…</span>
        ) : (
          <button
            type="button"
            disabled={busy}
            aria-label={`Unmute ${target}`}
            title={`Unmute ${target}: GitHub is asked about it again, and its items go back to the inbox’s lists`}
            onClick={onUnmute}
            className={rowButtonClass}
          >
            Unmute
          </button>
        )}
      </span>
    </li>
  )
}

// ReadinessSummary counts a group's rows by readiness, for its heading's note: what needs
// a look first, a dot of each edge's color before its count, and no readiness no row has.
// The counts sit on one line, and one that doesn't fit wraps onto a hidden second line,
// so it drops whole rather than being cut. A colon and commas, for a screen reader, keep
// the heading's name readable: "Unreleased changes · 3: 2 failing, 1 ready". The space
// after each is plain text beside it, not inside it: a name drops the spaces an element's
// own text starts or ends with, and the layout draws no space between its boxes.
export function ReadinessSummary({ rows }: { rows: readonly ReleaseCandidate[] }) {
  return (
    <>
      <span className="sr-only">:</span>{' '}
      <span className="flex h-4 min-w-0 flex-wrap items-center gap-x-3 gap-y-1 overflow-hidden">
        {readinessCounts(rows).map(({ readiness: r, count }, i) => (
          <Fragment key={r}>
            {i > 0 && (
              <>
                <span className="sr-only">,</span>{' '}
              </>
            )}
            <span className="inline-flex shrink-0 items-center gap-1">
              <span aria-hidden="true" className={`size-1.5 rounded-full ${readinessDot[r]}`} />
              {`${formatCount(count)} ${readinessWords[r]}`}
            </span>
          </Fragment>
        ))}
      </span>
    </>
  )
}
