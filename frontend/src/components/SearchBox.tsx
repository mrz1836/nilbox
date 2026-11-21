import { useId, type Ref } from 'react'

import { SearchIcon } from './icons'

export interface SearchBoxProps {
  value: string
  onChange: (value: string) => void
  // label names the field for assistive tech, and placeholder fills it while empty: by
  // default, a search of the page's repositories.
  label?: string
  placeholder?: string
  // inputRef reaches the text field, for a shortcut to focus it.
  inputRef?: Ref<HTMLInputElement> | undefined
  // className adds to the field's box, such as how it grows in a row.
  className?: string
  // dense draws the box as a page's own row of controls holds it, the default; false
  // draws it a little taller, as a side panel's search is.
  dense?: boolean
}

// SearchBox is a plain search field, a page's or a panel's own search in place of the
// toolbar's filter (SearchField): over its repositories, or, labelled otherwise, over
// whatever it lists, such as magex's targets, prompts, or variables. Its label is for
// assistive tech alone; the placeholder says what it searches.
export function SearchBox({
  value,
  onChange,
  label = 'Search repositories',
  placeholder = 'Search repositories…',
  inputRef,
  className = '',
  dense = true,
}: SearchBoxProps) {
  const id = useId()
  const pad = dense ? 'py-1' : 'py-1.5'

  return (
    <>
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <div
        className={`${className} flex items-center gap-2 rounded-md border border-border bg-background px-2.5 ${pad} focus-within:border-accent/60`.trim()}
      >
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <input
          ref={inputRef}
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
