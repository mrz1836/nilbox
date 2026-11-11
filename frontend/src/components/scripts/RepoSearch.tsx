import { useId } from 'react'

import { SearchIcon } from '../icons'

export interface RepoSearchProps {
  value: string
  onChange: (value: string) => void
}

// RepoSearch is a Scripts page's own search over its repositories, in place of the
// toolbar's filter, which Scripts doesn't show.
export function RepoSearch({ value, onChange }: RepoSearchProps) {
  const id = useId()

  return (
    <>
      <label htmlFor={id} className="sr-only">
        Search repositories
      </label>
      <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1 focus-within:border-accent/60">
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <input
          id={id}
          type="search"
          value={value}
          placeholder="Search repositories…"
          onChange={(e) => {
            onChange(e.target.value)
          }}
          className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
        />
      </div>
    </>
  )
}
