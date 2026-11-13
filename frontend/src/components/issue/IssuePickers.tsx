import { useId, useState, type KeyboardEvent, type ReactNode } from 'react'

import { usePopover, type Popover } from '../../lib/usePopover'
import { useQuery } from '../../lib/useQuery'
import { issueAssignable, issueLabels, type Issue, type IssueCandidate } from '../../platform'
import { fieldClass, primaryButtonClass, quietButtonClass } from '../controls'
import { LabelChip } from '../ItemSignals'
import { PopoverPanel } from '../PopoverPanel'

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
  popover: Popover<HTMLButtonElement, HTMLDivElement>
  // applying reports an edit under way, which holds the panel open until it ends.
  applying: boolean
  onApplying: (applying: boolean) => void
  // title names the picker, as in "Labels", and buttonLabel its button, as in "Edit labels".
  title: string
  buttonLabel: string
  placeholder: string
  query: string
  onQuery: (query: string) => void
  choices: readonly Choice[]
  // note is said under the choices: that they load, why they couldn't, or that none match.
  note: string
  // current are the keys the issue has now.
  current: readonly string[]
  // onApply sends what to add and take off, and resolves with why it couldn't, or ''.
  onApply: (add: string[], remove: string[]) => Promise<string>
}

// Picker is an Edit button that opens a panel of choices to check and uncheck, with a field
// to filter or search them, and Apply (or ⌘↩), which sends only what differs from the issue,
// holds the panel open while GitHub answers, and closes once GitHub made it all, or says
// why it couldn't.
function Picker({ popover, title, buttonLabel, ...props }: PickerProps) {
  const id = useId()
  const { open, toggle, triggerRef } = popover

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={buttonLabel}
        title={buttonLabel}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        className={quietButtonClass}
      >
        Edit
      </button>
      <PopoverPanel
        state={popover}
        align="right"
        id={id}
        role="dialog"
        aria-labelledby={`${id}-title`}
        className="z-30 flex w-72 flex-col shadow-black/20"
      >
        <h3 id={`${id}-title`} className="px-3 pt-3 text-sm font-semibold">
          {title}
        </h3>
        <PickerBody
          {...props}
          onDone={() => {
            popover.close(true)
          }}
        />
      </PopoverPanel>
    </div>
  )
}

// PickerBody is an open picker's field, choices, and buttons. It lives while the panel is
// open, so each opening starts with nothing changed.
function PickerBody({
  applying,
  onApplying,
  placeholder,
  query,
  onQuery,
  choices,
  note,
  current,
  onApply,
  onDone,
}: Omit<PickerProps, 'popover' | 'title' | 'buttonLabel'> & { onDone: () => void }) {
  const [wanted, setWanted] = useState<Wanted>(new Map())
  const [error, setError] = useState('')
  const now = new Set(current.map(fold))
  const checked = (key: string) => wanted.get(fold(key))?.on ?? now.has(fold(key))
  const { add, remove } = changes(wanted, now)
  const ready = add.length + remove.length > 0 && !applying

  const flip = (key: string) => {
    setWanted((was) => new Map(was).set(fold(key), { name: key, on: !checked(key) }))
  }

  const apply = async () => {
    onApplying(true)

    const failed = await onApply(add, remove)

    onApplying(false)

    if (failed === '') {
      onDone()
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

  return (
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions -- the panel's own ⌘↩, for the field and the checkboxes inside it
    <div onKeyDown={onKeyDown} className="flex min-h-0 flex-col gap-2 p-3">
      <input
        type="search"
        aria-label={placeholder}
        placeholder={placeholder}
        value={query}
        onChange={(e) => {
          onQuery(e.target.value)
        }}
        className={`${fieldClass} w-full`}
      />
      <ul className="max-h-64 space-y-0.5 overflow-y-auto">
        {choices.map((c) => (
          <li key={c.key}>
            <label
              title={c.locked === '' ? undefined : c.locked}
              className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-sm hover:bg-raised has-disabled:opacity-60"
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
              <span className="flex min-w-0 items-center gap-1.5">{c.label}</span>
              {/* A space keeps the name and the hint apart when read aloud; the flex layout drops it. */}
              {c.hint !== '' && (
                <>
                  {' '}
                  <span className="min-w-0 truncate text-xs text-muted">{c.hint}</span>
                </>
              )}
            </label>
          </li>
        ))}
      </ul>
      {note !== '' && <p className="text-xs text-muted">{note}</p>}
      {error !== '' && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <button type="button" disabled={applying} onClick={onDone} className={quietButtonClass}>
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
      </div>
    </div>
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

// usePicker is a picker's open state, which holds while an edit is under way.
function usePicker() {
  const [applying, setApplying] = useState(false)
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({ focus: 'panel', hold: applying })

  return { popover, applying, onApplying: setApplying }
}

// LabelPicker edits the issue's labels: the repository's labels, filtered by the field,
// each checked while the issue has it. The labels are read as it opens, which GitHub
// answers for one point, kept ten minutes; a repository with more than a hundred shows the
// first hundred in name order, and says so.
export function LabelPicker({ issue, onEdit }: { issue: Issue; onEdit: OnEdit }) {
  const picker = usePicker()
  const [query, setQuery] = useState('')
  const labels = useQuery(() => issueLabels(issue.id), [issue.id], { enabled: picker.popover.open })
  const q = query.trim().toLowerCase()
  const held = labels.data?.labels ?? []
  const shown = held.filter((l) => l.name.toLowerCase().includes(q))
  const loading = loadNote(labels.pending && labels.data === undefined, labels.error)

  return (
    <Picker
      {...picker}
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
  const picker = usePicker()
  const [query, setQuery] = useState('')
  const found = useQuery(() => issueAssignable(issue.id, query.trim()), [issue.id, query.trim()], {
    enabled: picker.popover.open,
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
      {...picker}
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
