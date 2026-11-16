import { useId, useState, type KeyboardEvent } from 'react'

import { XIcon } from '../../icons'

// TokenOption is a value a token field suggests: what it stores, and how it shows.
export interface TokenOption {
  value: string
  label: string
}

export interface TokenFieldProps {
  // id is the text field's id, which a label points at.
  id: string
  // label names the field for assistive tech, as in "Authors".
  label: string
  tokens: readonly string[]
  onChange: (tokens: string[]) => void
  // tokenLabel shows a token's value, as in "Dependabot" for app/dependabot.
  tokenLabel?: (value: string) => string
  // suggestions are what the field offers as the user types, or arrows down.
  suggestions: readonly TokenOption[]
  // toValue turns typed text into a token's value, as in "dependabot" into
  // app/dependabot; the text as typed, trimmed, by default.
  toValue?: (text: string) => string
  // choicesOnly takes a suggestion alone, never free text, for a field of choices.
  choicesOnly?: boolean
  placeholder?: string
  invalid?: boolean
  describedBy?: string | undefined
}

// maxShown caps the suggestions listed at once.
const maxShown = 8

// TokenField is a list of values as tokens, [Dependabot ×], with a text field that adds
// more: it suggests what the app has seen as the user types, and takes anything else typed
// too. Enter or a comma adds what is typed, or the suggestion picked with the arrows;
// Backspace in an empty field, or a token's ×, takes one away; Escape closes the
// suggestions.
export function TokenField({
  id,
  label,
  tokens,
  onChange,
  tokenLabel = (v) => v,
  suggestions,
  toValue = (t) => t.trim(),
  choicesOnly = false,
  placeholder = 'Add…',
  invalid = false,
  describedBy,
}: TokenFieldProps) {
  const listId = useId()
  const [text, setText] = useState('')
  const [active, setActive] = useState(-1)
  const [open, setOpen] = useState(false)
  const lower = text.trim().toLowerCase()
  const has = (value: string) => tokens.some((t) => t.toLowerCase() === value.toLowerCase())
  const options = suggestions
    .filter(
      (o) =>
        !has(o.value) &&
        (lower === '' || o.label.toLowerCase().includes(lower) || o.value.toLowerCase().includes(lower)),
    )
    .slice(0, maxShown)
  const shown = open && options.length > 0
  const activeId = shown && active >= 0 && active < options.length ? `${listId}-${String(active)}` : undefined

  const add = (value: string) => {
    if (value !== '' && !has(value)) {
      onChange([...tokens, value])
    }

    setText('')
    setActive(-1)
  }

  // commit adds the suggestion picked, else what is typed: in a field of choices, the
  // first suggestion that fits it.
  const commit = () => {
    const picked = shown && active >= 0 ? options[active] : undefined

    if (picked !== undefined) {
      add(picked.value)
    } else if (choicesOnly) {
      const first = options[0]

      if (first !== undefined && lower !== '') {
        add(first.value)
      }
    } else if (lower !== '') {
      add(toValue(text))
    }
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    switch (e.key) {
      case 'Enter':
      case ',':
        if (text.trim() !== '' || (shown && active >= 0)) {
          e.preventDefault()
          commit()
        }

        break
      case 'Backspace':
        if (text === '' && tokens.length > 0) {
          e.preventDefault()
          onChange(tokens.slice(0, -1))
        }

        break
      case 'ArrowDown':
        e.preventDefault()
        setOpen(true)
        setActive((i) => (options.length === 0 ? -1 : (i + 1) % options.length))

        break
      case 'ArrowUp':
        e.preventDefault()
        setOpen(true)
        setActive((i) => (options.length === 0 ? -1 : i <= 0 ? options.length - 1 : i - 1))

        break
      case 'Escape':
        if (shown) {
          e.preventDefault()
          e.stopPropagation()
          setOpen(false)
          setActive(-1)
        }

        break
      default:
        break
    }
  }

  return (
    <div className="relative min-w-0">
      <div
        className={`flex min-h-8 flex-wrap items-center gap-1 rounded-md border bg-surface px-1.5 py-1 focus-within:border-accent/60 ${invalid ? 'border-danger' : 'border-border'}`}
      >
        {tokens.map((t) => (
          <span
            key={t}
            className="inline-flex items-center gap-0.5 rounded-sm bg-raised py-0.5 pr-0.5 pl-1.5 text-xs text-text"
          >
            {tokenLabel(t)}
            <button
              type="button"
              aria-label={`Remove ${tokenLabel(t)}`}
              title="Remove"
              onClick={() => {
                onChange(tokens.filter((x) => x !== t))
              }}
              className="rounded-sm p-0.5 text-muted hover:bg-border hover:text-text"
            >
              <XIcon className="size-3" />
            </button>
          </span>
        ))}
        <input
          id={id}
          type="text"
          role="combobox"
          value={text}
          placeholder={tokens.length === 0 ? placeholder : ''}
          aria-label={label}
          aria-expanded={shown}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={activeId}
          aria-invalid={invalid}
          aria-describedby={describedBy}
          autoComplete="off"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          onChange={(e) => {
            const next = e.target.value

            if (next.endsWith(',')) {
              setText(next.slice(0, -1))

              return
            }

            setText(next)
            setOpen(true)
            setActive(-1)
          }}
          onFocus={() => {
            setOpen(true)
          }}
          onBlur={() => {
            setOpen(false)
            setActive(-1)
          }}
          onKeyDown={onKeyDown}
          className="min-w-24 flex-1 bg-transparent px-1 text-sm text-text outline-none placeholder:text-muted/70"
        />
      </div>
      {shown && (
        <div
          id={listId}
          role="listbox"
          aria-label={`${label} suggestions`}
          className="absolute top-full left-0 z-40 mt-1 max-h-64 w-64 overflow-y-auto rounded-lg border border-border bg-surface p-1 text-sm shadow-xl shadow-black/20"
        >
          {options.map((o, i) => (
            <div
              key={o.value}
              id={`${listId}-${String(i)}`}
              role="option"
              tabIndex={-1}
              aria-selected={i === active}
              onMouseDown={(e) => {
                // Keep the caret in the field while choosing.
                e.preventDefault()
                add(o.value)
              }}
              onMouseEnter={() => {
                setActive(i)
              }}
              className="cursor-pointer truncate rounded-md px-2 py-1 aria-selected:bg-raised"
            >
              {o.label}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
