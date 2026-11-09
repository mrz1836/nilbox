import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type RefObject,
  type SyntheticEvent,
} from 'react'

import { counted, formatCount } from '../lib/format'
import { insertSuggestion, segments, type QuerySpec, type Suggester, type Suggestion } from '../lib/query'
import { SearchIcon, XIcon } from './icons'

export interface SearchFieldProps {
  value: string
  onChange: (value: string) => void
  // placeholder says what the field does, as in "Filter inbox", and names it.
  placeholder: string
  // spec is what the page's filter understands, whose keys the field tints as they are
  // typed; without it the field is text only.
  spec?: QuerySpec | undefined
  // matches is how many things the query matches, or null while it filters nothing.
  matches: number | null
  // suggest offers choices under the field; without it there are none.
  suggest?: Suggester | undefined
  // inputRef reaches the text field, for ⌘F to focus it.
  inputRef?: RefObject<HTMLInputElement | null> | undefined
}

// SearchField is the toolbar's filter, a combobox. Focused and empty, it lists what the
// page can be narrowed by, each with a count; while a key's value is typed, the values
// that fit. Plain words just filter, and ↓ opens the list. ↑ and ↓ choose, Enter or Tab
// inserts, and Escape closes the list, then clears the query, then leaves the field.
//
// It is a plain text field rather than a search one, whose own Escape and clear button
// would cut those steps short, and it sits in no form. Keys the page understands are
// tinted by a copy of the text drawn behind the field, kept scrolled with it.
export function SearchField({ value, onChange, placeholder, spec, matches, suggest, inputRef }: SearchFieldProps) {
  const listId = useId()
  const ownRef = useRef<HTMLInputElement>(null)
  const input = inputRef ?? ownRef
  const backdropRef = useRef<HTMLDivElement>(null)
  const [focused, setFocused] = useState(false)
  const [caret, setCaret] = useState(value.length)
  // forcedAt is the query ↓ opened the list at, and closedAt the one Escape closed it at;
  // typing on from either lets the list follow the query again.
  const [forcedAt, setForcedAt] = useState<string | null>(null)
  const [closedAt, setClosedAt] = useState<string | null>(null)
  const [active, setActive] = useState(-1)
  // listed is the list the choice is in, so a new list starts its choice afresh.
  const [listed, setListed] = useState('')
  // pendingCaret is where a choice puts the caret, once the query it made renders.
  const pendingCaret = useRef<number | null>(null)

  const suggestions = suggest !== undefined && focused ? suggest(value, caret) : null
  const options = suggestions?.options ?? []
  const open =
    suggestions !== null && options.length > 0 && closedAt !== value && (suggestions.auto || forcedAt === value)
  const listKey = open ? `${suggestions.label}\n${options.map((o) => o.id).join('\n')}` : ''

  // A new list starts at its first option when it offers values, or when ↓ opened it;
  // otherwise nothing is chosen until ↓.
  if (listKey !== listed) {
    setListed(listKey)
    setActive(open && (suggestions.pickFirst === true || forcedAt === value) ? 0 : -1)
  }

  const segs = spec === undefined ? [] : segments(value, spec)
  const tinted = segs.some((s) => s.key)

  // sync notes where the caret is, and keeps the tinted copy scrolled with the field.
  const sync = (e: SyntheticEvent<HTMLInputElement>) => {
    const field = e.currentTarget

    setCaret(field.selectionStart ?? field.value.length)

    if (backdropRef.current !== null) {
      backdropRef.current.scrollLeft = field.scrollLeft
    }
  }

  useLayoutEffect(() => {
    const field = input.current

    if (field !== null && pendingCaret.current !== null) {
      field.setSelectionRange(pendingCaret.current, pendingCaret.current)
      pendingCaret.current = null
    }

    if (field !== null && backdropRef.current !== null) {
      backdropRef.current.scrollLeft = field.scrollLeft
    }
  })

  const accept = (choice: Suggestion) => {
    const next = insertSuggestion(value, choice.edit)

    pendingCaret.current = next.caret
    setCaret(next.caret)
    setForcedAt(null)
    onChange(next.value)
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    switch (e.key) {
      case 'ArrowDown':
        if (open) {
          e.preventDefault()
          setActive((at) => (at + 1) % options.length)
        } else if (suggestions !== null && options.length > 0) {
          e.preventDefault()
          setForcedAt(value)
          setClosedAt(null)
          setActive(0)
        }

        break
      case 'ArrowUp':
        if (open) {
          e.preventDefault()
          setActive((at) => (at <= 0 ? options.length - 1 : at - 1))
        }

        break
      case 'Enter':
      case 'Tab': {
        const pick = open ? options[active] : undefined

        if (pick !== undefined) {
          e.preventDefault()
          accept(pick)
        }

        break
      }
      case 'Escape':
        // Each step is the field's alone, so nothing around it, such as the settings
        // page, closes too.
        e.preventDefault()

        if (open) {
          setClosedAt(value)
        } else if (value !== '') {
          onChange('')
        } else {
          input.current?.blur()
        }

        break
    }
  }

  const activeId = open && active >= 0 ? `${listId}-${String(active)}` : undefined
  const spoken = matches === null ? '' : matches === 0 ? 'No matches' : counted(matches, 'match', 'matches')

  return (
    <div className="relative w-64 min-w-36 shrink">
      <div
        className={`@container flex items-center gap-2 rounded-md border px-2.5 py-1 transition-colors ${matches === null ? 'border-border bg-background focus-within:border-accent/60' : 'border-accent/70 bg-accent/5'}`}
      >
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <div className="relative flex min-w-0 flex-1">
          {tinted && (
            <div
              ref={backdropRef}
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 overflow-hidden text-sm whitespace-pre text-text"
            >
              {segs.map((s, i) => (
                // The runs are rebuilt from the text on every change, so their order is
                // their identity.
                <span key={i} className={s.key ? 'text-accent' : undefined}>
                  {s.text}
                </span>
              ))}
            </div>
          )}
          <input
            ref={input}
            type="text"
            role="combobox"
            value={value}
            placeholder={placeholder}
            aria-label={placeholder}
            aria-expanded={open}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={activeId}
            autoComplete="off"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            onChange={(e) => {
              onChange(e.target.value)
              setCaret(e.target.selectionStart ?? e.target.value.length)
            }}
            onFocus={() => {
              setFocused(true)
            }}
            onBlur={() => {
              setFocused(false)
              setForcedAt(null)
              setClosedAt(null)
            }}
            onKeyDown={onKeyDown}
            onKeyUp={sync}
            onSelect={sync}
            onClick={sync}
            onScroll={sync}
            className={`relative min-w-0 flex-1 bg-transparent text-sm outline-none select-text selection:bg-accent/25 placeholder:text-muted/70 ${tinted ? 'text-transparent caret-text' : 'text-text'}`}
          />
        </div>
        {matches !== null && (
          <span aria-hidden="true" className="shrink-0 text-xs whitespace-nowrap text-muted tabular-nums">
            <span className="@[13rem]:hidden">{formatCount(matches)}</span>
            <span className="hidden @[13rem]:inline">{spoken}</span>
          </span>
        )}
        {value === '' && !focused && (
          <kbd aria-hidden="true" className="shrink-0 font-sans text-xs text-muted">
            ⌘F
          </kbd>
        )}
        {value !== '' && (
          <button
            type="button"
            aria-label="Clear the search"
            title="Clear"
            onClick={() => {
              onChange('')
              input.current?.focus()
            }}
            className="-mr-1 shrink-0 rounded-sm p-0.5 text-muted transition-colors hover:bg-raised hover:text-text"
          >
            <XIcon className="size-3.5" />
          </button>
        )}
        <span role="status" className="sr-only">
          {spoken}
        </span>
      </div>
      {open && (
        <div className="absolute top-full right-0 z-40 mt-1 w-80 rounded-lg border border-border bg-surface p-1 text-sm shadow-xl shadow-black/20">
          <div id={listId} role="listbox" aria-label={suggestions.label} className="max-h-72 overflow-y-auto">
            {options.map((option, i) => (
              <div
                key={option.id}
                id={`${listId}-${String(i)}`}
                role="option"
                tabIndex={-1}
                aria-selected={i === active}
                onMouseDown={(e) => {
                  // Keep the caret in the field while choosing.
                  e.preventDefault()
                  accept(option)
                }}
                onMouseEnter={() => {
                  setActive(i)
                }}
                className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 aria-selected:bg-raised"
              >
                <span className="min-w-0 truncate">{option.label}</span>
                {option.detail !== '' && (
                  <span className="shrink-0 font-mono text-[0.75rem] text-muted">{option.detail}</span>
                )}
                <span className="ml-auto shrink-0 pl-2 text-xs text-muted tabular-nums">{option.count}</span>
              </div>
            ))}
          </div>
          {suggestions.hint !== '' && (
            <p className="mt-1 border-t border-border px-2 pt-1.5 pb-1 text-xs text-muted">{suggestions.hint}</p>
          )}
        </div>
      )}
    </div>
  )
}
