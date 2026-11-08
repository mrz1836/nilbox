import type { ReactNode } from 'react'

import { counted, formatCount } from '../lib/format'
import { featuredFields, projectText, projectTooltip, subIssuesText } from '../lib/projects'
import {
  checksSignal,
  reviewsSignal,
  sizeOf,
  sizeText,
  toneText,
  type ChecksKind,
  type ReviewsKind,
  type Signal,
} from '../lib/signals'
import { openExternal, type InboxItem, type Label, type Project, type ProjectField, type SubIssues } from '../platform'
import { CheckIcon, CommentIcon, DotIcon, EyeIcon, ProjectIcon, RingIcon, XIcon } from './icons'

// shownLabels is how many labels a card shows before folding the rest into "+N".
const shownLabels = 3

// statusTint is how strongly a project status's color tints its pill: an alpha byte
// appended to its "#rrggbb", light enough that the ink on it keeps its contrast in both
// themes. fieldEdge is how strongly a field option's color draws its pill's edge.
const statusTint = '29'
const fieldEdge = 'b3'

const iconClass = 'size-3.5 shrink-0'

// columns is the width each value takes on every card, whatever it says, so the values
// line up from card to card. Each fits the longest value a card should meet, measured in
// the line's own font: a ch is one digit, since the line uses tabular figures. A longer
// value still shows in full; it pushes the rest of its line along instead of overlapping.
// Labels close the line and take what they need.
const columns = {
  checks: 'min-w-[8ch]', // "No checks", the longest; fractions up to "999/999"
  reviews: 'min-w-[4.5ch]', // up to "99/99"
  comments: 'min-w-[3ch]', // up to "999"
  size: 'min-w-[5ch]', // "XXL" with its border, in the badge's smaller font
  added: 'min-w-[6.5ch]', // up to "+99,999"
  removed: 'min-w-[6.5ch]', // up to "−99,999"
  files: 'min-w-[6.6ch]', // up to "999 files"
  commits: 'min-w-[10ch]', // up to "999 commits"
} as const

const checkIcons: Readonly<Record<ChecksKind, ReactNode>> = {
  passed: <CheckIcon className={iconClass} />,
  failed: <XIcon className={iconClass} />,
  pending: <DotIcon className={iconClass} />,
  none: <RingIcon className={iconClass} />,
}

const reviewIcons: Readonly<Record<ReviewsKind, ReactNode>> = {
  approved: <CheckIcon className={iconClass} />,
  changes: <XIcon className={iconClass} />,
  waiting: <EyeIcon className={iconClass} />,
}

interface SlotProps {
  // long says the signal in full: the tooltip, and what a screen reader reads.
  long: string
  className?: string
  children: ReactNode
}

// Slot is one signal: an icon and a short form, with the full sentence for a tooltip and
// for screen readers. Each value in it sits in its column (see columns).
function Slot({ long, className = '', children }: SlotProps) {
  return (
    <li title={long} className={`inline-flex shrink-0 items-center gap-1 ${className}`}>
      <span aria-hidden="true" className="inline-flex items-center gap-1">
        {children}
      </span>
      <span className="sr-only">{long}</span>
    </li>
  )
}

function SignalSlot<K extends string>({
  signal,
  icon,
  column,
}: {
  signal: Signal<K>
  icon: ReactNode
  column: string
}) {
  return (
    <Slot long={signal.long} className={toneText[signal.tone]}>
      {icon}
      <span className={column}>{signal.short}</span>
    </Slot>
  )
}

// Labels shows the first few labels as chips with their GitHub color, and the count of
// the rest, named in its tooltip.
function Labels({ labels }: { labels: Label[] }) {
  if (labels.length === 0) {
    return null
  }

  const rest = labels.slice(shownLabels)

  return (
    <li className="flex min-w-0 items-center gap-1 overflow-hidden">
      <span className="sr-only">Labels:</span>
      {labels.slice(0, shownLabels).map((label) => (
        <span
          key={label.name}
          className="inline-flex max-w-40 shrink-0 items-center gap-1 rounded-full border border-border px-1.5 py-px text-[11px] text-text"
        >
          <span
            aria-hidden="true"
            className={`size-2 shrink-0 rounded-full ${label.color === '' ? 'bg-muted' : ''}`}
            style={label.color === '' ? undefined : { backgroundColor: label.color }}
          />
          <span className="truncate">{label.name}</span>
        </span>
      ))}
      {rest.length > 0 && (
        <span title={rest.map((label) => label.name).join(', ')} className="shrink-0 text-[11px]">
          +{rest.length}
          <span className="sr-only"> more: {rest.map((label) => label.name).join(', ')}</span>
        </span>
      )}
    </li>
  )
}

// FieldPill is one of a project's featured fields, such as Priority High, outlined in the
// option's color, as a project board draws it.
function FieldPill({ field }: { field: ProjectField }) {
  return (
    <span
      className={`inline-flex max-w-24 shrink-0 items-center rounded-full border px-1.5 text-[11px] leading-4 font-medium text-text ${field.color === '' ? 'border-border' : ''}`}
      style={field.color === '' ? undefined : { borderColor: field.color + fieldEdge }}
    >
      <span className="truncate">{field.value}</span>
    </span>
  )
}

// ProjectLink names the project an issue is in, with its status there as a pill in the
// status's color and its featured fields (see featuredFields) as outlined pills, and
// opens the project on GitHub. Its tooltip lists every field.
function ProjectLink({ project }: { project: Project }) {
  const where = projectText(project)
  const featured = featuredFields(project)
  const spoken = featured.map((f) => `, ${f.name} ${f.value}`).join('')

  return (
    <button
      type="button"
      title={projectTooltip(project)}
      aria-label={`Project ${where}${spoken}. Open it on GitHub`}
      onClick={() => {
        openExternal(project.url)
      }}
      className="group/project inline-flex min-w-0 items-center gap-1.5 rounded-sm text-muted transition-colors hover:text-text"
    >
      <ProjectIcon className={iconClass} />
      <span className="max-w-40 min-w-0 truncate group-hover/project:underline">{project.title}</span>
      {project.status !== '' && (
        <span
          className={`inline-flex max-w-36 shrink-0 items-center gap-1 rounded-full px-1.5 py-px text-[11px] font-medium text-text ${project.statusColor === '' ? 'bg-raised' : ''}`}
          style={project.statusColor === '' ? undefined : { backgroundColor: project.statusColor + statusTint }}
        >
          <span
            aria-hidden="true"
            className={`size-1.5 shrink-0 rounded-full ${project.statusColor === '' ? 'bg-muted' : ''}`}
            style={project.statusColor === '' ? undefined : { backgroundColor: project.statusColor }}
          />
          <span className="truncate">{project.status}</span>
        </span>
      )}
      {featured.map((field) => (
        <FieldPill key={field.name} field={field} />
      ))}
    </button>
  )
}

// SubIssueProgress shows how many of an issue's sub-issues are done, as a count and a
// small bar, green once all are.
function SubIssueProgress({ subIssues }: { subIssues: SubIssues }) {
  const { total, completed } = subIssues

  if (total === 0) {
    return null
  }

  const done = completed === total

  return (
    <Slot long={`Sub-issues: ${subIssuesText(subIssues)}`} className={done ? 'text-success' : ''}>
      <span className="relative h-1.5 w-10 overflow-hidden rounded-full bg-border">
        <span
          className="absolute inset-y-0 left-0 rounded-full bg-success"
          style={{ width: `${String((completed / total) * 100)}%` }}
        />
      </span>
      <span>
        {formatCount(completed)}/{formatCount(total)}
      </span>
    </Slot>
  )
}

// Projects shows the first project an issue is in, and the count of the rest, named in
// its tooltip with their statuses. An issue is rarely in more than one.
function Projects({ projects }: { projects: Project[] }) {
  const [first, ...rest] = projects

  if (first === undefined) {
    return null
  }

  const others = rest.map(projectText)

  return (
    <li className="flex min-w-0 items-center gap-1">
      <span className="sr-only">Projects:</span>
      <ProjectLink project={first} />
      {rest.length > 0 && (
        <span title={others.join('\n')} className="shrink-0 text-[11px]">
          +{rest.length}
          <span className="sr-only"> more: {others.join(', ')}</span>
        </span>
      )}
    </li>
  )
}

export interface ItemSignalsProps {
  item: InboxItem
}

// ItemSignals is the card's line of facts about an item. For a pull request: its checks,
// its reviews, its comments, and its size (lines added and removed, files, commits);
// for an issue, its comments, how far along its sub-issues are, and the GitHub projects it
// is in, with its status and featured fields in each.
// Labels close the line. Every value before the projects and labels keeps a fixed width,
// so the values line up from card to card.
export function ItemSignals({ item }: ItemSignalsProps) {
  const details = item.details

  if (details === null) {
    return <p className="mt-1.5 text-xs text-muted">Details arrive with the next refresh.</p>
  }

  const isPR = item.kind === 'pr'
  const { additions, deletions, changedFiles, commits } = details
  const checks = checksSignal(details.checks)
  const reviews = reviewsSignal(details.reviews)

  return (
    <ul
      aria-label="Details"
      className="mt-1.5 flex min-w-0 items-center gap-x-3 overflow-hidden text-xs whitespace-nowrap text-muted tabular-nums"
    >
      {isPR && <SignalSlot signal={checks} icon={checkIcons[checks.kind]} column={columns.checks} />}
      {isPR && <SignalSlot signal={reviews} icon={reviewIcons[reviews.kind]} column={columns.reviews} />}
      <Slot long={counted(details.comments, 'comment', 'comments')}>
        <CommentIcon className={iconClass} />
        <span className={columns.comments}>{formatCount(details.comments)}</span>
      </Slot>
      {isPR && (
        <Slot long={sizeText(additions, deletions, changedFiles, commits)}>
          <span
            className={`${columns.size} rounded border border-border px-1 text-center text-[10px] leading-4 font-semibold text-text`}
          >
            {sizeOf(additions + deletions)}
          </span>
          <span className={`${columns.added} text-success`}>+{formatCount(additions)}</span>
          <span className={`${columns.removed} text-danger`}>−{formatCount(deletions)}</span>
          <span className={columns.files}>{counted(changedFiles, 'file', 'files')}</span>
          <span className={columns.commits}>{counted(commits, 'commit', 'commits')}</span>
        </Slot>
      )}
      {!isPR && <SubIssueProgress subIssues={details.subIssues} />}
      {!isPR && <Projects projects={details.projects} />}
      <Labels labels={details.labels} />
    </ul>
  )
}
