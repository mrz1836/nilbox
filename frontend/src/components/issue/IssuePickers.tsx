import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

import { useCloseWhenHidden, useModalDialog } from '../../lib/useModalDialog'
import { useQuery } from '../../lib/useQuery'
import { issueAssignable, issueLabels, type Issue, type IssueCandidate } from '../../platform'
import { buttonClass, fieldClass, primaryButtonClass, quietButtonClass } from '../controls'
import { XIcon } from '../icons'
import { LabelChip } from '../ItemSignals'

// IssueEdit is a change to an issue's labels or assignees, as the pickers ask for it.
export interface IssueEdit {
  addLabels?: string[]
  removeLabels?: string[]
  addAssignees?: string[]
  removeAssignees?: string[]
}

// OnEdit makes an edit on GitHub, and resolves with why it couldn't, or '' once it did.
export type OnEdit = (edit: IssueEdit) => Promise<string>

// Choice is one row of a picker: its key (a label's name, a person's login), what it shows,
// quieter words after it, and why it can't be changed here, or ''.
interface Choice {
  key: string
  label: ReactNode
  hint: string
  locked: string
}

// Wanted is whether the user wants each row they changed on the issue, by its key folded,
// with the name it holds. It says what to have, not what to flip, so it stays right however
// the issue changes under it, as when the sheet reads it again after an edit that went part
// of the way.
type Wanted = ReadonlyMap<string, { name: string; on: boolean }>

// fold is a key as GitHub compares it: names and logins ignore case.
function fold(key: string): string {
  return key.toLowerCase()
}

// changes are what wanted asks of an issue that has the keys in now: the names to add and
// the names to take off.
function changes(wanted: Wanted, now: ReadonlySet<string>): { add: string[]; remove: string[] } {
  const want = [...wanted.values()]

  return {
    add: want.filter((w) => w.on && !now.has(fold(w.name))).map((w) => w.name),
    remove: want.filter((w) => !w.on && now.has(fold(w.name))).map((w) => w.name),
  }
}

interface PickerProps {
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
  // current are the keys the issue has now.
  current: readonly string[]
  // onApply sends what to add and take off, and resolves with why it couldn't, or ''.
  onApply: (add: string[], remove: string[]) => Promise<string>
}

// Picker is an Edit button that opens a dialog of choices to check and uncheck, with a
// field to filter or search them, and Apply (or ⌘↩), which sends only what differs from
// the issue, holds the dialog open while GitHub answers, and closes once GitHub made it
// all, or says why it couldn't.
function Picker({ open, onOpen, buttonLabel, ...props }: PickerProps) {
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

// PickerDialog is an open picker, in the middle of the window over the sheet, so the
// sheet's column never cuts it off: a native modal <dialog> (see useModalDialog), drawn in
// the page's body as LaunchDialog is, and closed with its page (useCloseWhenHidden). Focus
// starts in the field; Escape, the close button, Cancel, or a click on the backdrop closes
// it, except while GitHub answers, so the outcome shows there. It lives while it is open,
// so each opening starts with nothing changed.
//
// Its list keeps one height, cut short only by a short window, so the dialog doesn't jump
// as the field filters it. As in LaunchDialog, no column inside it takes flex-1, which the
// desktop's WebKit counts as empty.
function PickerDialog({
  id,
  title,
  placeholder,
  query,
  onQuery,
  choices,
  note,
  current,
  onApply,
  onClose,
}: Omit<PickerProps, 'open' | 'onOpen' | 'buttonLabel'> & { id: string; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const fieldRef = useRef<HTMLInputElement>(null)
  const [wanted, setWanted] = useState<Wanted>(new Map())
  const [error, setError] = useState('')
  const [applying, setApplying] = useState(false)
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
      // The backdrop's color is spelled out, as ConfirmDialog's is, for older Safari.
      className="m-auto max-h-[calc(100%-3rem)] w-[calc(100%-2rem)] max-w-md overflow-hidden rounded-xl border border-border bg-surface p-0 text-text shadow-xl backdrop:bg-[rgb(0_0_0/0.4)] open:flex open:flex-col motion-safe:animate-pop-in"
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
            value={query}
            onChange={(e) => {
              onQuery(e.target.value)
            }}
            className={`${fieldClass} w-full`}
          />
        </div>
        <div className="h-88 min-h-24 overflow-y-auto px-2 pb-2">
          {choices.length === 0 ? (
            <p className="flex h-full items-center justify-center px-4 text-center text-sm text-muted">{note}</p>
          ) : (
            <ul className="space-y-px">
              {choices.map((c) => (
                <li key={c.key}>
                  <label
                    title={c.locked === '' ? undefined : c.locked}
                    className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-0.5 rounded-md px-2 py-1.5 text-sm hover:bg-raised has-disabled:opacity-60"
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
                    <span className="flex min-w-0 items-center">{c.label}</span>
                    {/* A space keeps the name and the hint apart when read aloud; the grid drops it. */}
                    {c.hint !== '' && (
                      <>
                        {' '}
                        <span className="col-start-2 line-clamp-2 text-xs text-muted">{c.hint}</span>
                      </>
                    )}
                  </label>
                </li>
              ))}
            </ul>
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
            {applying ? 'Applying…' : 'Apply'}
          </button>
        </footer>
      </div>
    </dialog>,
    document.body,
  )
}

// loadNote says why a picker has no choices to show: they load, or they couldn't.
function loadNote(pending: boolean, error: string): string {
  if (error !== '') {
    return `Couldn’t read them from GitHub: ${error}`
  }

  return pending ? 'Reading them from GitHub…' : ''
}

// labelsNote says what the label picker shows under its choices: that they load, why they
// couldn't, that it holds only the first of a repository's labels, which the filter searches,
// or that none matches.
function labelsNote(loading: string, held: number, total: number, shown: number): string {
  if (loading !== '') {
    return loading
  }

  if (total > held) {
    return shown === 0
      ? `None of the first ${String(held)} labels matches, of ${String(total)}.`
      : `The first ${String(held)} labels, of ${String(total)}.`
  }

  return shown === 0 ? 'No label matches.' : ''
}

// LabelPicker edits the issue's labels: the repository's labels, filtered by the field,
// each checked while the issue has it. The labels are read as it opens, which GitHub
// answers for one point, kept ten minutes; a repository with more than a hundred shows the
// first hundred in name order, and says so.
export function LabelPicker({ issue, onEdit }: { issue: Issue; onEdit: OnEdit }) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const labels = useQuery(() => issueLabels(issue.id), [issue.id], { enabled: open })
  const q = query.trim().toLowerCase()
  const held = labels.data?.labels ?? []
  const shown = held.filter((l) => l.name.toLowerCase().includes(q))
  const loading = loadNote(labels.pending && labels.data === undefined, labels.error)

  return (
    <Picker
      open={open}
      onOpen={setOpen}
      title="Labels"
      buttonLabel="Edit labels"
      placeholder="Filter labels"
      query={query}
      onQuery={setQuery}
      choices={shown.map((l) => ({
        key: l.name,
        label: <LabelChip label={l} />,
        hint: l.description,
        locked: '',
      }))}
      note={labelsNote(loading, held.length, labels.data?.total ?? 0, shown.length)}
      current={issue.labels.map((l) => l.name)}
      onApply={(add, remove) => onEdit({ addLabels: add, removeLabels: remove })}
    />
  )
}

// AssigneePicker edits the issue's assignees: who it is assigned to, and the people a
// search finds it can be, a search costing one point as the user stops typing, kept ten
// minutes. The user takes themselves off with Unassign me, never here.
export function AssigneePicker({ issue, onEdit }: { issue: Issue; onEdit: OnEdit }) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const found = useQuery(() => issueAssignable(issue.id, query.trim()), [issue.id, query.trim()], {
    enabled: open,
    delay: 300,
  })
  const assigned = issue.assignees.map((p) => ({ login: p.login, name: '', mine: p.mine }))
  const people: IssueCandidate[] = [
    ...assigned,
    ...(found.data ?? []).filter((p) => !assigned.some((a) => fold(a.login) === fold(p.login))),
  ]
  const loading = loadNote(found.pending && found.data === undefined, found.error)

  return (
    <Picker
      open={open}
      onOpen={setOpen}
      title="Assignees"
      buttonLabel="Edit assignees"
      placeholder="Search people"
      query={query}
      onQuery={setQuery}
      choices={people.map((p) => ({
        key: p.login,
        label: (
          <span className="truncate">
            {p.login}
            {p.mine && (
              <>
                {' '}
                <span className="text-muted">(you)</span>
              </>
            )}
          </span>
        ),
        hint: p.name,
        locked:
          p.mine && issue.assignees.some((a) => a.mine)
            ? 'Take yourself off with Unassign me, in the action menu.'
            : '',
      }))}
      note={loading === '' && people.length === 0 ? 'No one matches.' : loading}
      current={issue.assignees.map((p) => p.login)}
      onApply={(add, remove) => onEdit({ addAssignees: add, removeAssignees: remove })}
    />
  )
}
