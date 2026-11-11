import { useId, useRef, useState, type SubmitEvent } from 'react'

import { firstChoice } from '../../lib/repoScripts'
import { useModalDialog } from '../../lib/useModalDialog'
import type { ScriptEntry, ScriptInput } from '../../platform'
import { buttonClass, fieldClass, primaryButtonClass } from '../controls'

// radioLimit is the most choices a pickString shows as radio buttons; one with more is a
// menu.
const radioLimit = 6

// inputLabel names an input as the form asks for it: its description, or else its ID.
function inputLabel(input: ScriptInput): string {
  return input.description === '' ? input.id : input.description
}

// Answer is what the form holds for one input: a promptString's text, and a pickString's
// choice by its place in the list, since two choices may fill in the same value.
interface Answer {
  input: ScriptInput
  text: string
  choice: number
}

// firstAnswer starts an input's field: its text the default, its choice the default's.
function firstAnswer(input: ScriptInput): Answer {
  return { input, text: input.default, choice: firstChoice(input) }
}

// valuesOf are the values the answers fill in, by input ID: a choice's value, or the text.
// A pickString with no choices keeps its default.
function valuesOf(answers: readonly Answer[]): Record<string, string> {
  return Object.fromEntries(
    answers.map(({ input, text, choice }) => [
      input.id,
      input.kind === 'pickString' ? (input.options[choice]?.value ?? input.default) : text,
    ]),
  )
}

interface InputFieldProps {
  input: ScriptInput
  id: string
  // text is a promptString's text, and choice a pickString's, by its place.
  text: string
  choice: number
  onText: (text: string) => void
  onChoice: (choice: number) => void
}

// InputField is one value a task asks for: a choice, as radio buttons, or a menu when it
// has many, each showing the value it fills in beside its label (Option.shown); or a line
// of text.
function InputField({ input, id, text, choice, onText, onChoice }: InputFieldProps) {
  const label = inputLabel(input)

  if (input.kind === 'promptString') {
    return (
      <div className="flex flex-col gap-1">
        <label htmlFor={id} className="text-sm font-medium text-text">
          {label}
        </label>
        <input
          id={id}
          type="text"
          value={text}
          spellCheck={false}
          onChange={(e) => {
            onText(e.target.value)
          }}
          className={`${fieldClass} font-mono text-[0.8125rem]`}
        />
      </div>
    )
  }

  if (input.options.length > radioLimit) {
    return (
      <div className="flex flex-col gap-1">
        <label htmlFor={id} className="text-sm font-medium text-text">
          {label}
        </label>
        <select
          id={id}
          value={String(choice)}
          onChange={(e) => {
            onChoice(Number(e.target.value))
          }}
          className={fieldClass}
        >
          {input.options.map((option, i) => (
            // Choices are told apart by place: two may share a value.
            <option key={i} value={String(i)}>
              {option.shown}
            </option>
          ))}
        </select>
      </div>
    )
  }

  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className="mb-1 text-sm font-medium text-text">{label}</legend>
      {input.options.map((option, i) => (
        // Choices are told apart by place: two may share a value.
        <label key={i} className="flex cursor-pointer items-center gap-2 text-sm text-text">
          <input
            type="radio"
            name={id}
            value={String(i)}
            checked={choice === i}
            onChange={() => {
              onChoice(i)
            }}
            className="radio"
          />
          {option.shown}
        </label>
      ))}
    </fieldset>
  )
}

export interface TaskInputsFormProps {
  // entry is the task, whose inputs the form asks for.
  entry: ScriptEntry
  // onRun runs it with the values filled in, by input ID; onCancel runs nothing.
  onRun: (values: Record<string, string>) => void
  onCancel: () => void
}

// TaskInputsForm asks for the values a task needs before it runs, VS Code's pickString
// and promptString, each filled in with its default to start. It is a modal dialog like
// ConfirmDialog (useModalDialog): focus starts on the first field, Escape cancels, and
// Enter runs. Render it only while it is open.
export function TaskInputsForm({ entry, onRun, onCancel }: TaskInputsFormProps) {
  const id = useId()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const [answers, setAnswers] = useState(() => entry.inputs.map(firstAnswer))
  const cancel = useModalDialog(
    dialogRef,
    onCancel,
    () => dialogRef.current?.querySelector<HTMLElement>('input:checked, input[type="text"], select') ?? null,
  )

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault()
    onRun(valuesOf(answers))
  }

  return (
    <dialog
      ref={dialogRef}
      aria-modal="true"
      aria-labelledby={`${id}-title`}
      onCancel={cancel}
      // The backdrop's color is spelled out, as ConfirmDialog's is.
      className="m-auto w-[calc(100%-2rem)] max-w-md rounded-lg border border-border bg-surface p-4 text-text shadow-lg backdrop:bg-[rgb(0_0_0/0.4)]"
    >
      <form onSubmit={submit} className="flex flex-col gap-4">
        <div>
          <h2 id={`${id}-title`} className="text-base font-semibold">
            Run “{entry.name}”
          </h2>
          <p className="mt-1 text-sm text-muted">
            This task asks for {entry.inputs.length === 1 ? 'a value' : 'values'}.
          </p>
        </div>
        {answers.map((answer, i) => (
          <InputField
            key={answer.input.id}
            input={answer.input}
            // The field's id is by position: an input's own ID may hold a space.
            id={`${id}-${String(i)}`}
            text={answer.text}
            choice={answer.choice}
            onText={(text) => {
              setAnswers(answers.with(i, { ...answer, text }))
            }}
            onChoice={(choice) => {
              setAnswers(answers.with(i, { ...answer, choice }))
            }}
          />
        ))}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onCancel} className={buttonClass}>
            Cancel
          </button>
          <button type="submit" className={primaryButtonClass}>
            Run
          </button>
        </div>
      </form>
    </dialog>
  )
}
