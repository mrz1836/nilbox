import type { ReactNode } from 'react'

import { effortChoices, modelChoices } from '../lib/launch'
import {
  describedBy,
  Field,
  fieldClass,
  fromTriState,
  Segmented,
  triState,
  type SegmentOption,
  type TriState,
} from './controls'

// LaunchOverrides are the launch settings a prompt or a set of launch defaults can set
// over the layers below it: each left empty (or null) inherits.
export interface LaunchOverrides {
  model: string
  effort: string
  planMode: boolean | null
  worktree: boolean | null
  extraFlags: string
}

// Inherited names what each setting falls back to when left on Inherit, such as
// "Opus (org)" or "the profile’s".
export interface Inherited {
  model: string
  effort: string
  planMode: string
  worktree: string
}

export interface LaunchOverrideFieldsProps {
  // id prefixes the controls' ids; give each form its own (useId).
  id: string
  values: LaunchOverrides
  // onChange receives the one setting that changed.
  onChange: (patch: Partial<LaunchOverrides>) => void
  // errors maps a setting to the reason it was refused, by field name.
  errors: Readonly<Record<string, string | undefined>>
  // inherited, when known, is shown on every setting left on Inherit.
  inherited?: Inherited
  // modelHint is help under the model field, if any; flagsHint is help under the flags.
  modelHint?: ReactNode
  flagsHint: ReactNode
}

const triOptions: readonly SegmentOption<TriState>[] = [
  { value: 'inherit', label: 'Inherit' },
  { value: 'on', label: 'On' },
  { value: 'off', label: 'Off' },
]

// inheritLabel is what an empty setting says: Inherit, with what it inherits when known.
function inheritLabel(inherited: string | undefined): string {
  return inherited === undefined ? 'Inherit' : `Inherit · ${inherited}`
}

interface TriFieldProps {
  label: string
  value: boolean | null
  onChange: (value: boolean | null) => void
  inherited: string | undefined
}

// TriField is an on, off, or inherit setting, saying what it inherits when known.
function TriField({ label, value, onChange, inherited }: TriFieldProps) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted">{label}</span>
      <Segmented
        label={label}
        size="sm"
        value={triState(value)}
        options={triOptions}
        onChange={(v) => {
          onChange(fromTriState(v))
        }}
      />
      {inherited !== undefined && <span className="text-xs text-muted">Inherits {inherited}</span>}
    </div>
  )
}

// LaunchOverrideFields are the model, effort, plan mode, worktree, and extra flags a
// prompt or a set of launch defaults sets for its launches. They render as cells of the
// form's two-column grid, the flags spanning both columns.
export function LaunchOverrideFields({
  id,
  values,
  onChange,
  errors,
  inherited,
  modelHint,
  flagsHint,
}: LaunchOverrideFieldsProps) {
  const failed = (field: keyof LaunchOverrides) => (errors[field] ?? '') !== ''

  return (
    <>
      <Field label="Model" controlId={`${id}-model`} error={errors.model} hint={modelHint}>
        <input
          id={`${id}-model`}
          list={`${id}-models`}
          value={values.model}
          placeholder={inheritLabel(inherited?.model)}
          spellCheck={false}
          aria-invalid={failed('model')}
          aria-describedby={describedBy(`${id}-model`, errors.model, modelHint !== undefined)}
          onChange={(e) => {
            onChange({ model: e.target.value })
          }}
          className={`${fieldClass} w-full`}
        />
        <datalist id={`${id}-models`}>
          {modelChoices.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </datalist>
      </Field>
      <Field label="Effort" controlId={`${id}-effort`} error={errors.effort}>
        <select
          id={`${id}-effort`}
          value={values.effort}
          aria-describedby={describedBy(`${id}-effort`, errors.effort, false)}
          onChange={(e) => {
            onChange({ effort: e.target.value })
          }}
          className={`${fieldClass} w-full`}
        >
          <option value="">{inheritLabel(inherited?.effort)}</option>
          {effortChoices.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      </Field>
      <TriField
        label="Plan mode"
        value={values.planMode}
        onChange={(planMode) => {
          onChange({ planMode })
        }}
        inherited={inherited?.planMode}
      />
      <TriField
        label="Start in a worktree"
        value={values.worktree}
        onChange={(worktree) => {
          onChange({ worktree })
        }}
        inherited={inherited?.worktree}
      />
      <div className="col-span-2">
        <Field label="Extra flags" controlId={`${id}-flags`} error={errors.extraFlags} hint={flagsHint}>
          <input
            id={`${id}-flags`}
            value={values.extraFlags}
            placeholder="None"
            spellCheck={false}
            aria-invalid={failed('extraFlags')}
            aria-describedby={describedBy(`${id}-flags`, errors.extraFlags, true)}
            onChange={(e) => {
              onChange({ extraFlags: e.target.value })
            }}
            className={`${fieldClass} w-full font-mono text-[13px]`}
          />
        </Field>
      </div>
    </>
  )
}
