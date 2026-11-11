import type { ReactNode } from 'react'

import { formatCount } from '../../lib/format'
import { ChevronDownIcon, ChevronRightIcon } from '../icons'

export interface GroupHeadingProps {
  // id is the heading's, which its section is labelled by.
  id: string
  title: string
  count: number
  // open reports the group's rows shown; disabled keeps it so, as while a search opens
  // every group.
  open: boolean
  disabled: boolean
  onToggle: () => void
  // note is quieter text after the count, such as what the group's repositories lack.
  note?: string
  // children sit at the heading's end, such as a button for the whole group.
  children?: ReactNode
}

// GroupHeading heads one of a Scripts page's groups of repositories: a button that opens
// or folds the group, with its title and how many it holds, and whatever acts on the whole
// group beside it.
export function GroupHeading({ id, title, count, open, disabled, onToggle, note, children }: GroupHeadingProps) {
  return (
    <div className="flex items-center gap-2">
      <h3 id={id} className="min-w-0 flex-1">
        <button
          type="button"
          aria-expanded={open}
          disabled={disabled}
          onClick={onToggle}
          className="flex w-full items-center gap-1.5 rounded-md px-1 py-1 text-left text-[0.6875rem] font-semibold tracking-wide text-muted uppercase hover:text-text disabled:hover:text-muted"
        >
          {open ? <ChevronDownIcon className="size-3.5" /> : <ChevronRightIcon className="size-3.5" />}
          {title} · {formatCount(count)}
          {note !== undefined && <span className="ml-2 truncate font-normal tracking-normal normal-case">{note}</span>}
        </button>
      </h3>
      {children}
    </div>
  )
}
