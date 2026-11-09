import { useState, type ReactNode } from 'react'

import { agentName, anyEffortChoices, modelChoices } from '../lib/launch'
import type { AgentID, AgentInfo, PerAgent } from '../platform'
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
// over the layers below it: each left empty (or null, or with no entry for an agent)
// inherits. The model and the extra flags are each agent's own; the effort, plan mode,
// and worktree are shared by every agent.
export interface LaunchOverrides {
  models: PerAgent<string>
  effort: string
  planMode: boolean | null
  worktree: boolean | null
  extraFlags: PerAgent<string>
}

// Inherited names what each setting falls back to when left on Inherit, such as
// "Opus (org)" or "the profile’s"; the model's, per agent.
export interface Inherited {
  models: PerAgent<string>
  effort: string
  planMode: string
  worktree: string
}

// AgentKey is a real agent's ID, as per-agent settings are keyed.
type AgentKey = Exclude<AgentID, ''>

export interface LaunchOverrideFieldsProps {
  // id prefixes the controls' ids; give each form its own (useId).
  id: string
  // agents are the coding agents, whose models and flags the form sets one at a time.
  agents: readonly AgentInfo[]
  values: LaunchOverrides
  // onChange receives the one setting that changed.
  onChange: (patch: Partial<LaunchOverrides>) => void
  // errors maps a setting to the reason it was refused, by field name: "effort", or for
  // one agent's model or flags, "models.codex" and "extraFlags.codex".
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

// agentError is the reason one agent's model or flags were refused, or the field's own.
function agentError(errors: LaunchOverrideFieldsProps['errors'], field: string, agent: AgentKey): string | undefined {
  return errors[`${field}.${agent}`] ?? errors[field]
}

// LaunchOverrideFields are the model, effort, plan mode, worktree, and extra flags a
// prompt or a set of launch defaults sets for its launches. The model and the flags are
// set per agent, under a tab for each; the rest apply to every agent. They render as cells
// of the form's two-column grid, the per-agent fields spanning both columns.
export function LaunchOverrideFields({
  id,
  agents,
  values,
  onChange,
  errors,
  inherited,
  modelHint,
  flagsHint,
}: LaunchOverrideFieldsProps) {
  // tabbed are the agents with a tab each; the form shows one agent's fields at a time.
  const tabbed = agents.filter((a): a is AgentInfo & { id: AgentKey } => a.id !== '')
  const [tab, setTab] = useState<AgentKey>(tabbed[0]?.id ?? 'claude')
  const name = agentName(agents, tab)
  const modelError = agentError(errors, 'models', tab)
  const flagsError = agentError(errors, 'extraFlags', tab)
  // tabs marks an agent whose model or flags are set, or refused.
  const tabs: SegmentOption<AgentKey>[] = tabbed.map((a) => {
    const refused =
      agentError(errors, 'models', a.id) !== undefined || agentError(errors, 'extraFlags', a.id) !== undefined
    const set = Boolean(values.models[a.id]) || Boolean(values.extraFlags[a.id]?.trim())
    const label = refused ? `${a.name} !` : set ? `${a.name} •` : a.name

    return { value: a.id, label, title: `${a.name}'s model and flags` }
  })

  return (
    <>
      <div className="col-span-2 flex flex-col gap-3 rounded-md border border-border p-3">
        {tabbed.length > 1 && (
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium text-muted">Per agent</span>
            <Segmented label="Agent" size="sm" value={tab} options={tabs} onChange={setTab} />
          </div>
        )}
        <Field label={`${name} model`} controlId={`${id}-model`} error={modelError} hint={modelHint}>
          <input
            id={`${id}-model`}
            list={`${id}-models`}
            value={values.models[tab] ?? ''}
            placeholder={inheritLabel(inherited?.models[tab])}
            spellCheck={false}
            aria-invalid={modelError !== undefined}
            aria-describedby={describedBy(`${id}-model`, modelError, modelHint !== undefined)}
            onChange={(e) => {
              onChange({ models: { ...values.models, [tab]: e.target.value } })
            }}
            className={`${fieldClass} w-full`}
          />
          <datalist id={`${id}-models`}>
            {modelChoices(agents, tab).map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </datalist>
        </Field>
        <Field label={`${name} extra flags`} controlId={`${id}-flags`} error={flagsError} hint={flagsHint}>
          <input
            id={`${id}-flags`}
            value={values.extraFlags[tab] ?? ''}
            placeholder="None"
            spellCheck={false}
            aria-invalid={flagsError !== undefined}
            aria-describedby={describedBy(`${id}-flags`, flagsError, true)}
            onChange={(e) => {
              onChange({ extraFlags: { ...values.extraFlags, [tab]: e.target.value } })
            }}
            className={`${fieldClass} w-full font-mono text-[0.8125rem]`}
          />
        </Field>
      </div>
      <Field
        label="Effort"
        controlId={`${id}-effort`}
        error={errors.effort}
        hint="Shared by every agent; one an agent lacks uses its default."
      >
        <select
          id={`${id}-effort`}
          value={values.effort}
          aria-describedby={describedBy(`${id}-effort`, errors.effort, true)}
          onChange={(e) => {
            onChange({ effort: e.target.value })
          }}
          className={`${fieldClass} w-full`}
        >
          <option value="">{inheritLabel(inherited?.effort)}</option>
          {anyEffortChoices(agents).map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      </Field>
      <div />
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
    </>
  )
}
