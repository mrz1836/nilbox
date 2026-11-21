import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

import { useCloseWhenHidden, useModalDialog } from '../lib/useModalDialog'
import {
  buttonClass,
  captionClass,
  fieldClass,
  primaryButtonClass,
  quietButtonClass,
  sheetDialogClass,
} from './controls'
import { XIcon } from './icons'

// The sheets' pickers: a dialog of choices to check and uncheck, with a field that filters
// them, which sends only what differs from the item. The issue sheet's labels and assignees
// and the review sheet's reviewers are each one.

// Choice is one row of a picker: its key (a label's name, a person's login), what it shows,
// quieter words under it (hint) and at its right (aside), a picture before it (icon), the
// heading of the group it is in (section, consecutive rows sharing one), and why it can't be
// changed here, or ''.
export interface Choice {
  key: string
  label: ReactNode
  hint: string
  locked: string
  aside?: string
  icon?: ReactNode
  section?: string
}

// Wanted is whether the user wants each row they changed on the item, by its key folded,
// with the name it holds. It says what to have, not what to flip, so it stays right however
// the item changes under it, as when the sheet reads it again after an edit that went part
// of the way.
type Wanted = ReadonlyMap<string, { name: string; on: boolean }>

// fold is a key as GitHub compares it: names and logins ignore case.
export function fold(key: string): string {
  return key.toLowerCase()
}

// changes are what wanted asks of an item that has the keys in now: the names to add and the
// names to take off.
function changes(wanted: Wanted, now: ReadonlySet<string>): { add: string[]; remove: string[] } {
  const want = [...wanted.values()]

  return {
    add: want.filter((w) => w.on && !now.has(fold(w.name))).map((w) => w.name),
    remove: want.filter((w) => !w.on && now.has(fold(w.name))).map((w) => w.name),
  }
}

export interface PickerProps {
  open: boolean
  onOpen: (open: boolean) => void
  // title names the picker, as in "Labels", and buttonLabel its button, as in "Edit labels".
  title: string
  buttonLabel: string
  placeholder: string
  query: string
  onQuery: (query: string) => void
  choices: readonly Choice[]
  // note says that the choices load, why they couldn't, or that none match, in the list's
  // place while it is empty, else beside the buttons.
  note: string
  // busy says what the picker is doing while it shows what it has, as in "Learning who
  // works on acme-org/api…", over a gliding bar, or ''.
  busy?: string
  // current are the keys the item has now.
  current: readonly string[]
  // applyLabel names the button for what the user changed, as in "Request 2 · Remove 1";
  // Apply without it.
  applyLabel?: (add: readonly string[], remove: readonly string[]) => string
  // onApply sends what to add and take off, and resolves with why it couldn't, or ''.
  onApply: (add: string[], remove: string[]) => Promise<string>
}

// Picker is an Edit button that opens a dialog of choices to check and uncheck, with a
// field to filter or search them, and Apply (or ⌘↩), which sends only what differs from
// the item, holds the dialog open while GitHub answers, and closes once GitHub made it all,
// or says why it couldn't.
export function Picker({ open, onOpen, buttonLabel, ...props }: PickerProps) {
  const id = useId()

  return (
    <>
      <button
        type="button"
        aria-label={buttonLabel}
        title={buttonLabel}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={(e) => {
          // The desktop's WebKit focuses no button on a click. Focusing this one first
          // makes it where the dialog gives focus back as it closes.
          e.currentTarget.focus()
          onOpen(true)
        }}
        className={quietButtonClass}
      >
        Edit
      </button>
      {open && (
        <PickerDialog
          {...props}
          id={id}
          onClose={() => {
            onOpen(false)
          }}
        />
      )}
    </>
  )
}

// Group is the rows of one section, in order.
interface Group {
  section: string
  rows: { choice: Choice; at: number }[]
}

// groups splits choices into their sections, in order, each row with its place in the
// whole list, which the keyboard moves through.
function groups(choices: readonly Choice[]): Group[] {
  const out: Group[] = []

  choices.forEach((choice, at) => {
    const section = choice.section ?? ''
    const last = out.at(-1)

    if (last?.section === section) {
      last.rows.push({ choice, at })
    } else {
      out.push({ section, rows: [{ choice, at }] })
    }
  })

  return out
}

// Active is the row the keyboard is on, for the query it was moved under: a new query puts
// it on the first row that matches, and no query on none.
interface Active {
  query: string
  at: number
}

// PickerDialog is an open picker, in the middle of the window over the sheet, so the
// sheet's column never cuts it off: a native modal <dialog> (see useModalDialog), drawn in
// the page's body as LaunchDialog is, and closed with its page (useCloseWhenHidden). Focus
// starts in the field, where ↑ and ↓ move through the rows, Enter checks or unchecks the one
// it is on, and ⌘↩ applies; Escape, the close button, Cancel, or a click on the backdrop
// closes it, except while GitHub answers, so the outcome shows there. It lives while it is
// open, so each opening starts with nothing changed.
//
// Its list keeps one height, cut short only by a short window, so the dialog doesn't jump
// as the field filters it. As in LaunchDialog, no column inside it takes flex-1, which the
// desktop's WebKit counts as empty.
export function PickerDialog({
  id,
  title,
  placeholder,
  query,
  onQuery,
  choices,
  note,
  busy = '',
  current,
  applyLabel,
  onApply,
  onClose,
}: Omit<PickerProps, 'open' | 'onOpen' | 'buttonLabel'> & { id: string; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const fieldRef = useRef<HTMLInputElement>(null)
  const [wanted, setWanted] = useState<Wanted>(new Map())
  const [error, setError] = useState('')
  const [applying, setApplying] = useState(false)
  const [active, setActive] = useState<Active>({ query, at: -1 })
  const cancel = useModalDialog(
    dialogRef,
    () => {
      if (!applying) {
        onClose()
      }
    },
    () => fieldRef.current,
    { backdrop: true },
  )

  useCloseWhenHidden(dialogRef, onClose)

  const now = new Set(current.map(fold))
  const checked = (key: string) => wanted.get(fold(key))?.on ?? now.has(fold(key))
  const { add, remove } = changes(wanted, now)
  const ready = add.length + remove.length > 0 && !applying
  const rowId = (at: number) => `${id}-row-${String(at)}`
  const listId = `${id}-list`
  const fresh = query.trim() === '' ? -1 : 0
  const at = Math.min(active.query === query ? active.at : fresh, choices.length - 1)
  const activeChoice = at >= 0 ? choices[at] : undefined

  const flip = (key: string) => {
    setWanted((was) => new Map(was).set(fold(key), { name: key, on: !checked(key) }))
  }

  const apply = async () => {
    setApplying(true)

    const failed = await onApply(add, remove)

    setApplying(false)

    if (failed === '') {
      onClose()
    } else {
      setError(failed)
    }
  }

  // moveTo puts the keyboard on the row next, scrolled into view.
  const moveTo = (next: number) => {
    setActive({ query, at: next })
    document.getElementById(rowId(next))?.scrollIntoView({ block: 'nearest' })
  }

  // The field's own keys: ↑ and ↓ move through the rows, and Enter checks or unchecks the
  // one the keyboard is on. ⌘↩ is the dialog's.
  const onFieldKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (choices.length === 0 || e.metaKey) {
      return
    }

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        moveTo(Math.min(at + 1, choices.length - 1))
        break
      case 'ArrowUp':
        e.preventDefault()
        moveTo(Math.max(at - 1, 0))
        break
      case 'Enter':
        e.preventDefault()

        if (activeChoice !== undefined && activeChoice.locked === '' && !applying) {
          flip(activeChoice.key)
        }

        break
      default:
    }
  }

  // ⌘↩ applies, as it runs the sheet's own button outside the picker.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Enter' && e.metaKey) {
      e.preventDefault()

      if (ready) {
        void apply()
      }
    }
  }

  return createPortal(
    <dialog
      ref={dialogRef}
      id={id}
      aria-modal="true"
      aria-labelledby={`${id}-title`}
      onCancel={cancel}
      className={`${sheetDialogClass} max-h-[calc(100%-3rem)] w-[calc(100%-2rem)] max-w-md overflow-hidden open:flex open:flex-col motion-safe:animate-pop-in`}
    >
      {/* The body takes focus itself, so a click on a checkbox or button, which the
          desktop's WebKit focuses no control for, leaves focus in the dialog. */}
      {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions -- the dialog's own ⌘↩, for the field and the checkboxes inside it */}
      <div tabIndex={-1} onKeyDown={onKeyDown} className="flex min-h-0 flex-col outline-none">
        <header className="flex shrink-0 items-center gap-3 border-b border-border py-2.5 pr-2.5 pl-4">
          <h2 id={`${id}-title`} className="min-w-0 flex-1 truncate text-base font-semibold">
            {title}
          </h2>
          <button
            type="button"
            aria-label="Close"
            title="Close (Esc)"
            disabled={applying}
            onClick={onClose}
            className={`${quietButtonClass} p-1`}
          >
            <XIcon className="size-4" />
          </button>
        </header>
        <div className="shrink-0 px-4 pt-3 pb-2">
          <input
            ref={fieldRef}
            type="search"
            aria-label={placeholder}
            placeholder={placeholder}
            aria-controls={listId}
            aria-activedescendant={activeChoice === undefined ? undefined : rowId(at)}
            value={query}
            onChange={(e) => {
              onQuery(e.target.value)
            }}
            onKeyDown={onFieldKeyDown}
            className={`${fieldClass} w-full`}
          />
        </div>
        {busy !== '' && (
          <div className="shrink-0 px-4 pb-2">
            <div aria-hidden="true" className="loading-bar rounded-full" />
            <p role="status" className="mt-1.5 text-xs text-muted">
              {busy}
            </p>
          </div>
        )}
        <div id={listId} className="h-88 min-h-24 overflow-y-auto px-2 pb-2">
          {choices.length === 0 ? (
            <p className="flex h-full items-center justify-center px-4 text-center text-sm text-muted">{note}</p>
          ) : (
            groups(choices).map((g) => (
              <PickerGroup key={`${g.section}-${String(g.rows[0]?.at ?? 0)}`} id={id} section={g.section}>
                {g.rows.map(({ choice: c, at: row }) => (
                  <li key={c.key}>
                    {/* The pointer puts the keyboard's place on the row it moves over, so one row
                        shows highlighted and ↑ and ↓ go on from it; the checkbox is the control. */}
                    {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- the pointer only moves the highlight */}
                    <label
                      id={rowId(row)}
                      title={c.locked === '' ? undefined : c.locked}
                      onMouseMove={() => {
                        if (row !== at) {
                          setActive({ query, at: row })
                        }
                      }}
                      className={`grid ${c.aside === undefined || c.aside === '' ? 'grid-cols-[auto_minmax(0,1fr)]' : 'grid-cols-[auto_minmax(0,1fr)_auto]'} items-center gap-x-3 gap-y-0.5 rounded-md px-2 py-1.5 text-sm has-disabled:opacity-disabled ${row === at ? 'bg-raised' : 'hover:bg-raised'}`}
                    >
                      <input
                        type="checkbox"
                        className="checkbox"
                        checked={checked(c.key)}
                        disabled={c.locked !== '' || applying}
                        onChange={() => {
                          flip(c.key)
                        }}
                      />
                      <span className="flex min-w-0 items-center gap-2">
                        {c.icon}
                        {c.label}
                      </span>
                      {/* Spaces keep the name and the words beside it apart when read aloud; the
                          grid drops them. */}
                      {c.aside !== undefined && c.aside !== '' && (
                        <>
                          {' '}
                          <span className="text-xs whitespace-nowrap text-muted">{c.aside}</span>
                        </>
                      )}
                      {c.hint !== '' && (
                        <>
                          {' '}
                          <span className="col-start-2 line-clamp-2 text-xs text-muted">{c.hint}</span>
                        </>
                      )}
                    </label>
                  </li>
                ))}
              </PickerGroup>
            ))
          )}
        </div>
        {error !== '' && (
          <p role="alert" className="shrink-0 border-t border-border px-4 py-2 text-xs text-danger">
            {error}
          </p>
        )}
        <footer className="flex shrink-0 items-center gap-2 border-t border-border px-4 py-3">
          <p className="min-w-0 flex-1 text-xs text-muted">{choices.length === 0 ? '' : note}</p>
          <button type="button" disabled={applying} onClick={onClose} className={buttonClass}>
            Cancel
          </button>
          <button
            type="button"
            disabled={!ready}
            onClick={() => {
              void apply()
            }}
            className={primaryButtonClass}
          >
            {applying ? 'Applying…' : (applyLabel?.(add, remove) ?? 'Apply')}
          </button>
        </footer>
      </div>
    </dialog>,
    document.body,
  )
}

// PickerGroup is one section of a picker's rows, under its heading; a section with no name
// is a plain list.
function PickerGroup({ id, section, children }: { id: string; section: string; children: ReactNode }) {
  const headingId = `${id}-section-${section.replaceAll(/\W+/g, '-')}`

  if (section === '') {
    return <ul className="space-y-px">{children}</ul>
  }

  return (
    <div role="group" aria-labelledby={headingId} className="pt-2 first:pt-0">
      <h3 id={headingId} className={`${captionClass} px-2 pb-1`}>
        {section}
      </h3>
      <ul className="space-y-px">{children}</ul>
    </div>
  )
}
