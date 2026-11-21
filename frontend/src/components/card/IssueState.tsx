import { formatCount } from '../../lib/format'
import { featuredFields, projectText, subIssuesText } from '../../lib/projects'
import type { InboxItem, SubIssues } from '../../platform'
import { FieldPill, StatusPill } from '../ItemSignals'
import { gradeClass } from './SizeGlyph'

// Ring is a small ring filled as far as an issue's sub-issues are done.
function Ring({ subIssues }: { subIssues: SubIssues }) {
  const { total, completed } = subIssues
  // The ring's circumference is 2πr for r = 5.5 on its 16-unit grid.
  const around = 2 * Math.PI * 5.5
  const filled = Math.min(completed / total, 1) * around

  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" className="size-3.25 -rotate-90">
      <circle cx="8" cy="8" r="5.5" fill="none" strokeWidth="2.5" className="stroke-border" />
      <circle
        cx="8"
        cy="8"
        r="5.5"
        fill="none"
        strokeWidth="2.5"
        strokeDasharray={`${String(filled)} ${String(around)}`}
        className="stroke-success"
      />
    </svg>
  )
}

// SubIssueRing is how far an issue's sub-issues are, as a small ring filled that far and
// "{done}/{total}", in the success color once all are done.
function SubIssueRing({ subIssues }: { subIssues: SubIssues }) {
  const { total, completed } = subIssues
  const done = completed === total

  return (
    <span data-sub-issues className={`inline-flex items-center gap-1 ${done ? 'text-success' : 'text-muted'}`}>
      <Ring subIssues={subIssues} />
      <span aria-hidden="true">
        {formatCount(completed)}/{formatCount(total)}
      </span>
      <span className="sr-only">Sub-issues: {subIssuesText(subIssues)}</span>
    </span>
  )
}

// IssueState is where an issue stands, in a pull request's track's place: its status in
// the first project it is in, as a pill tinted with the status's color, that project's
// first featured field (featuredFields) as an outlined pill, and its sub-issues as a ring
// with how many are done. An issue with none of these shows nothing.
export function IssueState({ item }: { item: InboxItem }) {
  const d = item.details
  const project = d?.projects[0]
  const status = project !== undefined && project.status !== ''
  const field = project === undefined ? undefined : featuredFields(project)[0]
  const subIssues = d === null || d.subIssues.total === 0 ? null : d.subIssues

  if (!status && field === undefined && subIssues === null) {
    return null
  }

  return (
    <div className="flex flex-col items-end gap-1.25 text-caption">
      {status && <StatusPill project={project} />}
      {(field !== undefined || subIssues !== null) && (
        <div className="flex items-center gap-1.75">
          {field !== undefined && (
            <span className="inline-flex">
              <span className="sr-only">{field.name} </span>
              <FieldPill field={field} />
            </span>
          )}
          {subIssues !== null && <SubIssueRing subIssues={subIssues} />}
        </div>
      )}
    </div>
  )
}

// IssueTrack is where an issue stands in a compact card's track column: its sub-issues as
// a ring filled as far as they are done, or else a dot in the color of its status in its
// first project; its tooltip, and what a screen reader reads, say both. An issue with
// neither shows nothing.
export function IssueTrack({ item }: { item: InboxItem }) {
  const d = item.details
  const first = d?.projects[0]
  // project is the issue's first project, while the issue has a status there.
  const project = first !== undefined && first.status !== '' ? first : undefined
  const subIssues = d === null || d.subIssues.total === 0 ? null : d.subIssues
  const words = [project === undefined ? '' : projectText(project), subIssues === null ? '' : subIssuesText(subIssues)]
    .filter((part) => part !== '')
    .join(' · ')
  const mark =
    subIssues !== null ? (
      <Ring subIssues={subIssues} />
    ) : project === undefined ? null : (
      <span
        data-status-dot
        className={`block size-2.25 rounded-full ${project.statusColor === '' ? 'bg-muted' : ''}`}
        style={project.statusColor === '' ? undefined : { backgroundColor: project.statusColor }}
      />
    )

  if (mark === null) {
    return null
  }

  return (
    <span role="img" aria-label={words} title={words} className="relative z-10 inline-flex items-center justify-end">
      {mark}
    </span>
  )
}

// IssueGrade is an issue's first featured field (featuredFields), such as its priority, in
// a compact card's size column, named in its tooltip; nothing when its first project has
// none.
export function IssueGrade({ item }: { item: InboxItem }) {
  const project = item.details?.projects[0]
  const field = project === undefined ? undefined : featuredFields(project)[0]

  if (field === undefined) {
    return null
  }

  return (
    <span title={`${field.name}: ${field.value}`} className={`relative z-10 ${gradeClass}`}>
      <span className="sr-only">{field.name} </span>
      {field.value}
    </span>
  )
}
