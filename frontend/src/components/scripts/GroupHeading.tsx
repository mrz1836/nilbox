import type { ReactNode } from 'react'

import { formatCount } from '../../lib/format'
import { ChevronDownIcon, ChevronRightIcon } from '../icons'

// HeadingLine is what a heading's line says: its title and how many the group holds, in
// capitals, or, as a Worktrees clone heads its rows, a name, a node in normal case, such
// as the clone's owner/name.
type HeadingLine = { title: string; count: number; name?: never } | { name: ReactNode; title?: never; count?: never }

export type GroupHeadingProps = HeadingLine & {
  // id is the heading's, which its section is labelled by.
  id: string
  // open reports the group's rows shown; disabled keeps it so, as while a search opens
  // every group.
  open: boolean
  disabled: boolean
  onToggle: () => void
  // note is quieter text after the line, such as what the group's repositories lack: a
  // string, cut short when the heading runs out of room, or a node that lays itself out in
  // the room left, such as the Release page's readiness summary.
  note?: ReactNode
  // className adds to the heading's own classes, as a grid places it.
  className?: string
  // children sit at the heading's end, such as a button for the whole group.
  children?: ReactNode
}

// GroupHeading heads one of a Scripts page's groups, or a clone on the Worktrees page: a
// button that opens or folds the group, with its title and how many it holds (or its name),
// over a rule that sets the group apart from the one before, and whatever acts on the whole
// group beside it. It is sticky inside the page's scroll container, so it stays at the top,
// over the page's background, while its group's rows scroll under it.
export function GroupHeading(props: GroupHeadingProps) {
  const { id, open, disabled, onToggle, note, className = '', children } = props

  return (
    <div
      className={`sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-background pt-1 pb-1 ${className}`.trim()}
    >
      <h3 id={id} className="min-w-0 flex-1">
        <button
          type="button"
          aria-expanded={open}
          disabled={disabled}
          onClick={onToggle}
          className="flex w-full items-center gap-1.5 rounded-md px-1 py-1 text-left text-xs font-semibold tracking-wide text-text uppercase transition-colors hover:bg-raised disabled:hover:bg-transparent"
        >
          {open ? (
            <ChevronDownIcon className="size-3.5 shrink-0 text-muted" />
          ) : (
            <ChevronRightIcon className="size-3.5 shrink-0 text-muted" />
          )}
          {props.title === undefined ? (
            <span className="shrink-0 text-body font-normal tracking-normal normal-case">{props.name}</span>
          ) : (
            <span className="truncate">
              {props.title} · {formatCount(props.count)}
            </span>
          )}
          <Note note={note} />
        </button>
      </h3>
      {children}
    </div>
  )
}

// Note is a heading's note after its line: a string cut short to the room it has, any
// other node given the rest of the heading's width to lay itself out in, and nothing
// without one.
function Note({ note }: { note: ReactNode }) {
  if (note === undefined) {
    return null
  }

  if (typeof note === 'string') {
    return <span className="ml-2 truncate font-normal tracking-normal text-muted normal-case">{note}</span>
  }

  return <span className="ml-2 min-w-0 flex-1 font-normal tracking-normal text-muted normal-case">{note}</span>
}
