import { projectText, featuredFields, projectTooltip } from '../lib/projects'
import { openExternal, type Label, type Project, type ProjectField } from '../platform'
import { ProjectIcon } from './icons'

// The parts a card, a sheet, and a conversation share to show a label, an issue's
// project and its status there, and how far something is along.

// statusTint is how strongly a project status's color tints its pill: an alpha byte
// appended to its "#rrggbb", light enough that the ink on it keeps its contrast in both
// themes. fieldEdge is how strongly a field option's color draws its pill's edge.
const statusTint = '29'
const fieldEdge = 'b3'

const iconClass = 'size-3.5 shrink-0'

// FilterBy adds a filter to the inbox's query, or takes it away.
type FilterBy = (key: string, value: string) => void

// chipClass is a label chip's shape; a chip that filters is a button of the same shape.
const chipClass =
  'inline-flex max-w-40 shrink-0 items-center gap-1 rounded-full border border-border px-1.5 py-px text-caption text-text'

// LabelChip is one label with its GitHub color: a button that filters the inbox by it,
// when the card offers that, or else a plain chip.
export function LabelChip({ label, onFilterBy }: { label: Label; onFilterBy?: FilterBy | undefined }) {
  const children = (
    <>
      <span
        aria-hidden="true"
        className={`size-2 shrink-0 rounded-full ${label.color === '' ? 'bg-muted' : ''}`}
        style={label.color === '' ? undefined : { backgroundColor: label.color }}
      />
      <span className="truncate">{label.name}</span>
    </>
  )

  if (onFilterBy === undefined) {
    return <span className={chipClass}>{children}</span>
  }

  return (
    <button
      type="button"
      aria-label={`Filter by label ${label.name}`}
      title={`Filter by label ${label.name}`}
      onClick={() => {
        onFilterBy('label', label.name)
      }}
      className={`${chipClass} transition-colors hover:bg-raised`}
    >
      {children}
    </button>
  )
}

// FieldPill is a field's value, such as a project's Priority High, outlined in the
// option's color, as a project board draws it. On a card it is cut short; wide lets it
// run as long as its line.
export function FieldPill({ field, wide = false }: { field: ProjectField; wide?: boolean }) {
  return (
    <span
      className={`inline-flex ${wide ? 'max-w-full' : 'max-w-24'} shrink-0 items-center rounded-full border px-1.5 text-caption leading-4 font-medium text-text ${field.color === '' ? 'border-border' : ''}`}
      style={field.color === '' ? undefined : { borderColor: field.color + fieldEdge }}
    >
      <span className="truncate">{field.value}</span>
    </span>
  )
}

// StatusPill is an issue's status in a project, such as In progress, as a pill tinted
// with the status's color and a dot of it.
export function StatusPill({ project }: { project: Project }) {
  return (
    <span
      className={`inline-flex max-w-36 shrink-0 items-center gap-1 rounded-full px-1.5 py-px text-caption font-medium text-text ${project.statusColor === '' ? 'bg-raised' : ''}`}
      style={project.statusColor === '' ? undefined : { backgroundColor: project.statusColor + statusTint }}
    >
      <span
        aria-hidden="true"
        className={`size-1.5 shrink-0 rounded-full ${project.statusColor === '' ? 'bg-muted' : ''}`}
        style={project.statusColor === '' ? undefined : { backgroundColor: project.statusColor }}
      />
      <span className="truncate">{project.status}</span>
    </span>
  )
}

// ProjectLink names the project an issue is in, with its status there as a pill in the
// status's color (StatusPill) and its featured fields (see featuredFields) as outlined
// pills, and opens the project on GitHub. Its tooltip lists every field. Without
// featuring, it leaves the fields to whatever lists them beside it.
export function ProjectLink({ project, featuring = true }: { project: Project; featuring?: boolean }) {
  const where = projectText(project)
  const featured = featuring ? featuredFields(project) : []
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
      {project.status !== '' && <StatusPill project={project} />}
      {featured.map((field) => (
        <FieldPill key={field.name} field={field} />
      ))}
    </button>
  )
}

export interface ProgressBarProps {
  // done of total is how far the bar is filled.
  done: number
  total: number
  // className sizes the bar: by default it is 2.5rem wide.
  className?: string
}

// ProgressBar is a small bar filled as far as done goes of total, in the success color. It
// is decoration: the words beside it say how far.
export function ProgressBar({ done, total, className = 'w-10' }: ProgressBarProps) {
  return (
    <span aria-hidden="true" className={`relative h-1.5 shrink-0 overflow-hidden rounded-full bg-border ${className}`}>
      <span
        className="absolute inset-y-0 left-0 rounded-full bg-success"
        style={{ width: `${String(total > 0 ? Math.min(done / total, 1) * 100 : 0)}%` }}
      />
    </span>
  )
}
