import { useId, type ReactNode } from 'react'

import { buttonClass, fieldClass } from '../controls'
import { XIcon } from '../icons'
import { ErrorText, Group, joinIds } from './rows'

export interface StringListEditorProps {
  // label names the list for assistive tech.
  label: string
  items: readonly string[]
  // empty is what shows in place of the list while it has no items.
  empty: ReactNode
  // removeLabel names an item's remove button, such as "Remove deploy/".
  removeLabel: (item: string) => string
  onRemove: (item: string) => void
  // extra sits after an item, such as a marker that it is one of the defaults.
  extra?: (item: string) => ReactNode
  // inputLabel names the field a new item is typed in; it is read, not shown.
  inputLabel: string
  placeholder: string
  // submitLabel names the button that adds the draft.
  submitLabel: string
  draft: string
  onDraftChange: (draft: string) => void
  // onAdd adds the draft; the field cannot be submitted while the draft is blank.
  onAdd: () => void
  // error is why the draft was refused, shown under the field, or ''.
  error: string
  // describedBy is the id of more text about the field, such as a note on its syntax.
  describedBy?: string
  // busy holds every control while a change is being stored.
  busy: boolean
  // actions sit before the submit button, such as one that picks a folder.
  actions?: ReactNode
  // below sits under the field, such as what adding the draft would add.
  below?: ReactNode
}

// StringListEditor is a card listing strings, each with a button that removes it, above a
// field that adds another. It only draws the list: the section keeps the draft, checks
// it, stores each change, and says why one was refused.
export function StringListEditor({
  label,
  items,
  empty,
  removeLabel,
  onRemove,
  extra,
  inputLabel,
  placeholder,
  submitLabel,
  draft,
  onDraftChange,
  onAdd,
  error,
  describedBy = '',
  busy,
  actions,
  below,
}: StringListEditorProps) {
  const id = useId()

  return (
    <Group>
      {items.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">{empty}</p>
      ) : (
        <ul aria-label={label} className="divide-y divide-border">
          {items.map((item) => (
            <li key={item} className="flex items-center gap-3 px-4 py-2.5">
              <span className="min-w-0 truncate font-mono text-[0.8125rem]">{item}</span>
              {extra?.(item)}
              <button
                type="button"
                disabled={busy}
                aria-label={removeLabel(item)}
                title={removeLabel(item)}
                onClick={() => {
                  onRemove(item)
                }}
                className="ml-auto flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-danger disabled:opacity-50"
              >
                <XIcon className="size-3.5" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          onAdd()
        }}
        className="flex items-start gap-2 px-4 py-3"
      >
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <label htmlFor={`${id}-add`} className="sr-only">
            {inputLabel}
          </label>
          <input
            id={`${id}-add`}
            value={draft}
            placeholder={placeholder}
            spellCheck={false}
            autoComplete="off"
            aria-invalid={error !== ''}
            aria-describedby={joinIds(error !== '' && `${id}-error`, describedBy !== '' && describedBy)}
            onChange={(e) => {
              onDraftChange(e.target.value)
            }}
            className={`${fieldClass} w-full font-mono text-[0.8125rem]`}
          />
          <ErrorText id={`${id}-error`} error={error} />
        </div>
        {actions}
        <button type="submit" disabled={busy || draft.trim() === ''} className={buttonClass}>
          {submitLabel}
        </button>
      </form>
      {below}
    </Group>
  )
}
