import { useId, useRef, useState, type SubmitEvent } from 'react'

import { useModalDialog } from '../../lib/useModalDialog'
import { useMutation } from '../../lib/useMutation'
import { useQuery } from '../../lib/useQuery'
import {
  iconLabels,
  isChosen,
  readyToSave,
  repoChoices,
  runsInLabel,
  scopeLabels,
  toggleRepo,
} from '../../lib/userScripts'
import {
  listScriptRepoNames,
  saveUserScript,
  type RunsIn,
  type UserScript,
  type UserScriptIcon,
  type UserScriptScope,
} from '../../platform'
import { buttonClass, Field, fieldClass, primaryButtonClass, Segmented, type SegmentOption } from '../controls'
import { BoxIcon, ChecklistIcon, FlaskIcon, PlayIcon, SlidersIcon } from '../icons'
import { Spinner } from '../Spinner'

// iconClass sizes an icon in the editor's icon choice.
const iconClass = 'size-3.5'

const iconOptions: readonly SegmentOption<UserScriptIcon>[] = [
  { value: 'play', label: iconLabels.play, icon: <PlayIcon className={iconClass} /> },
  { value: 'test', label: iconLabels.test, icon: <FlaskIcon className={iconClass} /> },
  { value: 'lint', label: iconLabels.lint, icon: <ChecklistIcon className={iconClass} /> },
  { value: 'build', label: iconLabels.build, icon: <BoxIcon className={iconClass} /> },
  { value: 'configure', label: iconLabels.configure, icon: <SlidersIcon className={iconClass} /> },
]

const runsInOptions: readonly SegmentOption<RunsIn | ''>[] = (['', 'app', 'terminal'] as const).map((value) => ({
  value,
  label: runsInLabel(value),
}))

const scopes: readonly UserScriptScope[] = ['all', 'go', 'repos']

interface ChosenProps {
  chosen: readonly string[]
  onChange: (repos: string[]) => void
}

// Chosen is the editor's list of repositories to choose from, one checkbox each: the
// clones' repositories, read once it opens, and those already chosen, with a filter.
function Chosen({ chosen, onChange }: ChosenProps) {
  const id = useId()
  const [filter, setFilter] = useState('')
  const names = useQuery(() => listScriptRepoNames(), [])
  const wanted = filter.trim().toLowerCase()
  const choices = repoChoices(names.data ?? [], chosen).filter((n) => n.toLowerCase().includes(wanted))

  return (
    <div className="flex flex-col gap-1.5 rounded-md border border-border p-2">
      <label htmlFor={`${id}-filter`} className="sr-only">
        Filter the repositories
      </label>
      <input
        id={`${id}-filter`}
        type="search"
        value={filter}
        placeholder="Filter"
        spellCheck={false}
        onChange={(e) => {
          setFilter(e.target.value)
        }}
        className={`${fieldClass} py-1 text-[0.8125rem]`}
      />
      {names.data === undefined && names.error === '' && (
        <p role="status" className="flex items-center gap-2 px-1 text-xs text-muted">
          <Spinner />
          Reading your clones…
        </p>
      )}
      {names.error !== '' && (
        <p role="alert" className="px-1 text-xs text-danger">
          Couldn’t list your clones’ repositories: {names.error}
        </p>
      )}
      {names.data !== undefined && choices.length === 0 && (
        <p className="px-1 text-xs text-muted">
          {wanted === '' ? 'None of your clones is on GitHub.' : 'No repository matches.'}
        </p>
      )}
      {choices.length > 0 && (
        <ul aria-label="Repositories" className="flex max-h-40 flex-col gap-0.5 overflow-y-auto">
          {choices.map((name) => (
            <li key={name}>
              <label className="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 text-sm text-text hover:bg-raised">
                <input
                  type="checkbox"
                  checked={isChosen(chosen, name)}
                  onChange={(e) => {
                    onChange(toggleRepo(chosen, name, e.target.checked))
                  }}
                  className="checkbox"
                />
                {name}
              </label>
            </li>
          ))}
        </ul>
      )}
      <p className="px-1 text-xs text-muted">
        {chosen.length === 0 ? 'Choose at least one.' : `${String(chosen.length)} chosen`}
      </p>
    </div>
  )
}

export interface ScriptEditorProps {
  // script is the script to edit, or a new one, whose ID is 0.
  script: UserScript
  // variables name the variables every script of the user's runs with, as Go names them
  // (RepoScripts.variables).
  variables: readonly string[]
  // onSaved hears the script as saved; onCancel closes the editor, saving nothing.
  onSaved: (saved: UserScript) => void
  onCancel: () => void
}

// ScriptEditor makes or edits one of the user's own scripts: its name; the command, a
// line as they'd type it in a terminal, run with the repository's details in variables;
// the repositories it shows in, all, the Go ones, or those chosen from the clones'; where
// it runs; whether it asks before running; and its icon. It is a modal dialog like
// TaskInputsForm (useModalDialog): focus starts on the name, Escape cancels, and Enter
// saves. Render it only while it is open.
export function ScriptEditor({ script, variables, onSaved, onCancel }: ScriptEditorProps) {
  const id = useId()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const nameRef = useRef<HTMLInputElement>(null)
  const [draft, setDraft] = useState(script)
  const save = useMutation(saveUserScript)
  const cancel = useModalDialog(dialogRef, onCancel, () => nameRef.current)
  const set = (patch: Partial<UserScript>) => {
    setDraft({ ...draft, ...patch })
  }

  const submit = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()

    const result = await save.run(draft)

    if (result.ok) {
      onSaved(result.data)
    }
  }

  return (
    <dialog
      ref={dialogRef}
      aria-modal="true"
      aria-labelledby={`${id}-title`}
      onCancel={cancel}
      // The backdrop's color is spelled out, as ConfirmDialog's is.
      className="m-auto w-[calc(100%-2rem)] max-w-xl rounded-lg border border-border bg-surface p-4 text-text shadow-lg backdrop:bg-[rgb(0_0_0/0.4)]"
    >
      <form
        onSubmit={(e) => {
          void submit(e)
        }}
        className="flex flex-col gap-4"
      >
        <h2 id={`${id}-title`} className="text-base font-semibold">
          {script.id === 0 ? 'New script' : `Edit “${script.name}”`}
        </h2>
        <Field label="Name" controlId={`${id}-name`}>
          <input
            ref={nameRef}
            id={`${id}-name`}
            type="text"
            value={draft.name}
            placeholder="Fix all"
            onChange={(e) => {
              set({ name: e.target.value })
            }}
            className={fieldClass}
          />
        </Field>
        <Field
          label="Command"
          controlId={`${id}-command`}
          hint={`A shell command, as you’d type it in a terminal. It runs in the clone, with ${variables.join(', ')} set, so it never needs to quote a path.`}
        >
          <input
            id={`${id}-command`}
            type="text"
            value={draft.command}
            placeholder="magex format:fix && magex lint && magex test:race"
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            aria-describedby={`${id}-command-hint`}
            onChange={(e) => {
              set({ command: e.target.value })
            }}
            className={`${fieldClass} font-mono text-[0.8125rem]`}
          />
        </Field>
        <fieldset className="flex flex-col gap-1.5">
          <legend className="mb-1 text-xs font-medium text-muted">Repositories</legend>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            {scopes.map((scope) => (
              <label key={scope} className="flex cursor-pointer items-center gap-2 text-sm text-text">
                <input
                  type="radio"
                  name={`${id}-scope`}
                  value={scope}
                  checked={draft.scope === scope}
                  onChange={() => {
                    set({ scope })
                  }}
                  className="radio"
                />
                {scopeLabels[scope]}
              </label>
            ))}
          </div>
          {draft.scope === 'go' && <p className="text-xs text-muted">Clones with a go.mod at their root.</p>}
          {draft.scope === 'repos' && (
            <Chosen
              chosen={draft.repos}
              onChange={(repos) => {
                set({ repos })
              }}
            />
          )}
        </fieldset>
        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium text-muted">Runs in</span>
          <Segmented
            label="Runs in"
            value={draft.runsIn}
            options={runsInOptions}
            onChange={(runsIn) => {
              set({ runsIn })
            }}
          />
          <p className="text-xs text-muted">Terminal suits a command that asks you questions.</p>
        </div>
        <label htmlFor={`${id}-confirm`} className="flex cursor-pointer items-center gap-2 text-sm text-text">
          <input
            id={`${id}-confirm`}
            type="checkbox"
            checked={draft.confirm}
            onChange={(e) => {
              set({ confirm: e.target.checked })
            }}
            className="checkbox"
          />
          Ask before running
        </label>
        <div className="flex flex-col gap-1">
          <span className="text-xs font-medium text-muted">Icon</span>
          <Segmented
            label="Icon"
            size="sm"
            value={draft.icon}
            options={iconOptions}
            onChange={(icon) => {
              set({ icon })
            }}
          />
        </div>
        {save.error !== '' && (
          <p role="alert" className="text-sm text-danger">
            {save.error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onCancel} className={buttonClass}>
            Cancel
          </button>
          <button type="submit" disabled={!readyToSave(draft) || save.pending} className={primaryButtonClass}>
            {save.pending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </dialog>
  )
}
