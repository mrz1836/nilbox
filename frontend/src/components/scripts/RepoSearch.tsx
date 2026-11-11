import { useId } from 'react'

import { SearchIcon } from '../icons'

export interface RepoSearchProps {
  value: string
  onChange: (value: string) => void
  // label names the field for assistive tech, and placeholder fills it while empty: by
  // default, a search of the page's repositories.
  label?: string
  placeholder?: string
}

// RepoSearch is a Scripts page's own search, in place of the toolbar's filter, which
// Scripts doesn't show: over its repositories, or, labelled otherwise, over what else the
// page lists, such as magex's targets.
export function RepoSearch({
  value,
  onChange,
  label = 'Search repositories',
  placeholder = 'Search repositories…',
}: RepoSearchProps) {
  const id = useId()

  return (
    <>
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1 focus-within:border-accent/60">
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <input
          id={id}
          type="search"
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            onChange(e.target.value)
          }}
          className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
        />
      </div>
    </>
  )
}
