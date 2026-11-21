import { useId, type ReactNode } from 'react'

import type { AutopilotCatalog, AutopilotPastRules, AutopilotProblem, AutopilotSuggestions } from '../../../platform'
import { captionClass, fieldClass, quietButtonClass } from '../../controls'
import { AlertIcon } from '../../icons'

// BuilderKit is what every block of the builder reads beside its part of the draft: the
// catalog it renders from, what the stored items suggest its pickers, the account signed
// in (whose name is your account), the problems and warnings the backend found, each
// where it is, and how the draft stands on merging past the rules.
export interface BuilderKit {
  catalog: AutopilotCatalog
  suggestions: AutopilotSuggestions
  viewer: string
  problems: readonly AutopilotProblem[]
  warnings: readonly AutopilotProblem[]
  pastRules?: PastRulesKit | undefined
}

// PastRulesKit is how a draft stands on merging past the rules, which Merge it's ticks say
// beside them: how the routine as saved stands ('' for a new one), whether Save asks the
// user to confirm the ticks (pastRulesToConfirm), and how to open Settings → Autopilot,
// where the switch for them is.
export interface PastRulesKit {
  saved: AutopilotPastRules
  confirm: boolean
  onSettings: (() => void) | undefined
}

export interface BlockProps {
  // title is the block's caption: WATCH, WHEN, IF, THEN, LIMITS.
  title: string
  // hint is a quiet line under the caption, such as what the block decides.
  hint?: string
  children: ReactNode
}

// Block lays out one part of a routine: its caption on the left, and the part itself,
// edited in place, on the right.
export function Block({ title, hint, children }: BlockProps) {
  const id = useId()

  return (
    <section aria-labelledby={id} className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-x-4 border-t border-border py-3">
      <div className="flex flex-col gap-0.5 pt-1.5">
        <h3 id={id} className={captionClass}>
          {title}
        </h3>
        {hint !== undefined && <p className="text-caption text-muted">{hint}</p>}
      </div>
      <div className="flex min-w-0 flex-col gap-2">{children}</div>
    </section>
  )
}

export interface NotesProps {
  problems: readonly AutopilotProblem[]
  warnings?: readonly AutopilotProblem[]
  // extra are warnings in words alone, such as a search's own from a test.
  extra?: readonly string[]
  id?: string
}

// sentenceCase writes a message the backend words in lower case as a sentence.
function sentenceCase(text: string): string {
  const t = text.trim()
  const first = t.charAt(0).toUpperCase() + t.slice(1)

  return /[.!?:]$/.test(first) ? first : `${first}.`
}

// Notes lists what is wrong where it sits, which keeps the routine from being saved, and
// what may make it do less than meant, which doesn't.
export function Notes({ problems, warnings = [], extra = [], id }: NotesProps) {
  if (problems.length === 0 && warnings.length === 0 && extra.length === 0) {
    return null
  }

  return (
    <ul id={id} className="flex flex-col gap-0.5">
      {problems.map((p) => (
        <li key={`p:${p.field}:${p.message}`} className="flex items-start gap-1.5 text-xs text-danger">
          <AlertIcon className="mt-0.5 size-3 shrink-0" />
          <span>{sentenceCase(p.message)}</span>
        </li>
      ))}
      {[...warnings.map((w) => w.message), ...extra].map((text) => (
        <li key={`w:${text}`} className="flex items-start gap-1.5 text-xs text-warning">
          <AlertIcon className="mt-0.5 size-3 shrink-0" />
          <span>{sentenceCase(text)}</span>
        </li>
      ))}
    </ul>
  )
}

export interface ResetProps {
  // label says what is put back, for assistive tech: "Reset the time limit".
  label: string
  onReset: () => void
}

// Reset puts a value set here back to the one it inherits, or its default.
export function Reset({ label, onReset }: ResetProps) {
  return (
    <button
      type="button"
      aria-label={label}
      title="Put back the default"
      onClick={onReset}
      className={quietButtonClass}
    >
      Reset
    </button>
  )
}

export interface NumberFieldProps {
  id: string
  label: string
  // value is the number set here, or null while it takes its default, shown as the
  // placeholder.
  value: number | null | undefined
  fallback: number
  min: number
  max: number
  unit: string
  onChange: (value: number | null) => void
}

// NumberField is a whole number set here, or left to its default: its unit after it, and
// Reset while it is set.
export function NumberField({ id, label, value, fallback, min, max, unit, onChange }: NumberFieldProps) {
  const set = value !== null && value !== undefined

  return (
    <span className="inline-flex items-center gap-1.5 text-sm">
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <input
        id={id}
        type="number"
        inputMode="numeric"
        min={min}
        max={max}
        value={set ? value : ''}
        placeholder={String(fallback)}
        onChange={(e) => {
          const n = e.target.valueAsNumber

          onChange(e.target.value === '' || Number.isNaN(n) ? null : Math.trunc(n))
        }}
        className={`${fieldClass} w-16 py-1 text-right tabular-nums`}
      />
      <span className="text-muted">{unit}</span>
      {set ? (
        <Reset
          label={`Reset ${label.toLowerCase()}`}
          onReset={() => {
            onChange(null)
          }}
        />
      ) : (
        <span className="text-xs text-muted">default</span>
      )}
    </span>
  )
}
