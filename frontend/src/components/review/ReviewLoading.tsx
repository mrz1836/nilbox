import type { ReactNode } from 'react'

import { capitalize, formatAgo } from '../../lib/time'
import type { ListMark } from '../../lib/useReview'
import type { ItemKind } from '../../platform'
import { GitHubReading } from '../GitHubReading'
import { IssueIcon, PullRequestIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'

export interface ReadAgeProps {
  // mark is how the sheet marks what it read (listMark), fetchedAt when it read it, and what
  // names what it read, as in "checks".
  mark: ListMark
  fetchedAt: string
  what: string
}

// ReadAge says, quietly, that what the sheet read of a pull request is older than what it
// shows around it: being read from GitHub again, as in "Reading checks… · read 14m ago", or as
// read then, as in "Checks as read 14m ago". It says nothing of what is current.
export function ReadAge({ mark, fetchedAt, what }: ReadAgeProps) {
  if (mark === 'current') {
    return null
  }

  const age = <RelativeTime iso={fetchedAt} show={formatAgo} />

  return (
    <p className="text-xs text-muted">
      {mark === 'reading' ? (
        <>
          Reading {what}… · read {age}
        </>
      ) : (
        <>
          {capitalize(what)} as read {age}
        </>
      )}
    </p>
  )
}

// sweep staggers a placeholder's highlight by its place, so the sweep travels down them.
function sweep(i: number): { animationDelay: string } {
  return { animationDelay: `${String(i * 90)}ms` }
}

// Reading is how the banner speaks of an item of one kind: its icon, what it calls it
// while nothing names it, and the parts its first read from GitHub brings.
interface Reading {
  icon: ReactNode
  noun: string
  coming: readonly string[]
}

const readings: Readonly<Record<ItemKind, Reading>> = {
  pr: {
    icon: <PullRequestIcon className="size-4 text-accent" />,
    noun: 'the pull request',
    coming: ['Description', 'Changed files', 'Checks', 'Comments'],
  },
  issue: {
    icon: <IssueIcon className="size-4 text-accent" />,
    noun: 'the issue',
    coming: ['Description', 'Comments', 'Linked pull requests', 'Projects'],
  },
}

export interface ReadingBannerProps {
  // target names the item, as in "acme-org/api#212", or is '' when nothing names it yet.
  target: string
  // kind is what the item is: a pull request, by default, or an issue.
  kind?: ItemKind
}

// ReadingBanner says the sheet is reading an item from GitHub for the first time, and what
// that read brings (see GitHubReading).
export function ReadingBanner({ target, kind = 'pr' }: ReadingBannerProps) {
  const { icon, noun, coming } = readings[kind]

  return (
    <GitHubReading
      icon={icon}
      heading={<>Reading {target === '' ? noun : <span className="font-mono">{target}</span>} from GitHub…</>}
      coming={coming}
    />
  )
}

// fileRows are the widths of the placeholder file names, varied so they read as a list.
const fileRows = ['w-24', 'w-32', 'w-20', 'w-28', 'w-16', 'w-36', 'w-12'] as const

// FileListSkeleton stands in for the file list until the files come: a count, then rows
// shaped like files, each with its dot, its name, and its counts.
export function FileListSkeleton() {
  return (
    <div aria-hidden="true" className="text-xs">
      <span className="skeleton mb-2 block h-3 w-14" />
      <ul className="space-y-2.5 pt-1">
        {fileRows.map((width, i) => (
          <li key={width} className="flex items-center gap-1.5 px-1">
            <span className="skeleton size-1.5 shrink-0 rounded-full" style={sweep(i)} />
            <span className={`skeleton h-2.5 ${width}`} style={sweep(i)} />
            <span className="skeleton ml-auto h-2.5 w-5 shrink-0" style={sweep(i)} />
            <span className="skeleton h-2.5 w-5 shrink-0" style={sweep(i)} />
          </li>
        ))}
      </ul>
    </div>
  )
}

// descriptionLines are the widths of the placeholder lines of the description.
const descriptionLines = ['w-11/12', 'w-full', 'w-4/5', 'w-2/3'] as const

// DescriptionSkeleton stands in for the description until it comes: a few lines of text.
export function DescriptionSkeleton() {
  return (
    <div aria-hidden="true" className="space-y-2 py-0.5">
      {descriptionLines.map((width, i) => (
        <span key={width} className={`skeleton block h-3 ${width}`} style={sweep(i)} />
      ))}
    </div>
  )
}

// Tone is a placeholder diff line's kind: context, an addition, or a deletion.
type Tone = 'context' | 'add' | 'del'

// toneRow tints a placeholder diff line as the diff tints its kind.
const toneRow: Record<Tone, string> = { context: '', add: 'bg-success/10', del: 'bg-danger/10' }

// diffLines are the placeholder lines of a diff: their widths and kinds, a hunk's worth.
const diffLines: readonly { id: string; width: string; tone: Tone }[] = [
  { id: 'l1', width: 'w-1/3', tone: 'context' },
  { id: 'l2', width: 'w-1/2', tone: 'context' },
  { id: 'l3', width: 'w-2/5', tone: 'del' },
  { id: 'l4', width: 'w-3/5', tone: 'add' },
  { id: 'l5', width: 'w-1/2', tone: 'add' },
  { id: 'l6', width: 'w-2/3', tone: 'add' },
  { id: 'l7', width: 'w-1/4', tone: 'context' },
  { id: 'l8', width: 'w-2/5', tone: 'context' },
]

export interface DiffLinesSkeletonProps {
  // lines is how many placeholder lines to show, at most a hunk's worth.
  lines?: number
}

// DiffLinesSkeleton stands in for a diff's lines while they load: each with its line
// number and its text, the additions and the deletion tinted as the diff tints them.
export function DiffLinesSkeleton({ lines = diffLines.length }: DiffLinesSkeletonProps) {
  return (
    <div aria-hidden="true" className="py-1">
      {diffLines.slice(0, lines).map((line, i) => (
        <div key={line.id} className={`flex items-center gap-3 px-3 py-1 ${toneRow[line.tone]}`}>
          <span className="skeleton h-2.5 w-6 shrink-0 opacity-60" style={sweep(i)} />
          <span className={`skeleton h-2.5 ${line.width}`} style={sweep(i)} />
        </div>
      ))}
    </div>
  )
}

// diffCards are the placeholder files of the diff: the width of each one's path and how
// many lines it shows.
const diffCards = [
  { id: 'first', path: 'w-48', lines: 8 },
  { id: 'second', path: 'w-36', lines: 5 },
] as const

// DiffSkeleton stands in for the diff until the files come: two files, each a header with
// its path and counts over a hunk of lines.
export function DiffSkeleton() {
  return (
    <div aria-hidden="true" className="space-y-4">
      {diffCards.map((card, c) => (
        <div key={card.id} className="overflow-hidden rounded-md border border-border">
          <div className="flex items-center gap-2 border-b border-border bg-raised/50 px-3 py-2">
            <span className={`skeleton h-3 ${card.path}`} style={sweep(c * 4)} />
            <span className="skeleton ml-auto h-3 w-12" style={sweep(c * 4)} />
          </div>
          <DiffLinesSkeleton lines={card.lines} />
        </div>
      ))}
    </div>
  )
}

// summaryRows are the widths of the placeholder rows of the summary: checks and reviews.
const summaryRows = ['w-40', 'w-32', 'w-44'] as const

// SummarySkeleton stands in for the summary until the checks and reviews come: the
// change's chips, then a row for each of a few checks and reviews.
export function SummarySkeleton() {
  return (
    <div aria-hidden="true" className="space-y-3">
      <div className="flex gap-1.5">
        <span className="skeleton h-5 w-16 rounded-full" />
        <span className="skeleton h-5 w-20 rounded-full" style={sweep(1)} />
      </div>
      <ul className="space-y-2.5">
        {summaryRows.map((width, i) => (
          <li key={width} className="flex items-center gap-2">
            <span className="skeleton size-3.5 shrink-0 rounded-full" style={sweep(i + 2)} />
            <span className={`skeleton h-3 ${width}`} style={sweep(i + 2)} />
          </li>
        ))}
      </ul>
    </div>
  )
}
