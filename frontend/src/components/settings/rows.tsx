import type { ReactNode } from 'react'

import { Switch } from '../controls'
import { headingMatchClass, rowMatchClass, useSetting } from './match'

// The building blocks every settings section lays itself out with: cards of rows, the
// heading over a part of a section, the note under one, and the text of a refusal.

// joinIds joins the ids a control points aria-describedby at, leaving out those not shown
// (false), or returns undefined when none are left.
export function joinIds(...ids: (string | false)[]): string | undefined {
  const shown = ids.filter((id) => id !== false)

  return shown.length === 0 ? undefined : shown.join(' ')
}

// rowDescribedBy lists what describes a SettingRow's control: the row's help, then its
// error while it shows one, then any ids the section adds.
export function rowDescribedBy(controlId: string, error: string, ...more: (string | false)[]): string | undefined {
  return joinIds(`${controlId}-help`, error !== '' && `${controlId}-error`, ...more)
}

interface ErrorTextProps {
  // id is what a control's aria-describedby points at, so the error is read with it.
  id?: string | undefined
  // error is why a change was refused; nothing shows while it is ''.
  error: string
  // className places the text, such as a margin.
  className?: string
}

// ErrorText is why a change was refused, announced as soon as it shows.
export function ErrorText({ id, error, className = '' }: ErrorTextProps) {
  if (error === '') {
    return null
  }

  return (
    <p id={id} role="alert" className={`${className} text-xs text-danger`.trim()}>
      {error}
    </p>
  )
}

// Group is a card of setting rows.
export function Group({ children }: { children: ReactNode }) {
  return <div className="divide-y divide-border rounded-lg border border-border bg-surface">{children}</div>
}

// Note is the small print under a part of a section.
export function Note({ id, children }: { id?: string; children: ReactNode }) {
  return (
    <p id={id} className="text-xs leading-relaxed text-muted">
      {children}
    </p>
  )
}

interface SectionIntroProps {
  title: string
  // id is the heading's, for a region's aria-labelledby.
  id?: string
  // children is the help under the heading, if any.
  children?: ReactNode
  // action sits at the end of the heading's line, such as a button that restores the
  // defaults.
  action?: ReactNode
}

// SectionIntro is the heading over a part of a section, with its help below it and an
// action at its end, if any. "Find a setting" finds it by its title, and colors the
// heading while it matches.
export function SectionIntro({ title, id, children, action }: SectionIntroProps) {
  const [marks, match] = useSetting(title)
  const headingClass = `text-sm font-semibold ${headingMatchClass(match)}`.trim()

  // The marks go on the intro's outermost element: the heading alone, the heading with
  // its help, or either beside the action.
  if (children === undefined && action === undefined) {
    return (
      <h3 {...marks} id={id} className={headingClass}>
        {title}
      </h3>
    )
  }

  const heading = (
    <h3 id={id} className={headingClass}>
      {title}
    </h3>
  )
  const help = children === undefined ? null : <p className="mt-0.5 text-xs text-muted">{children}</p>

  if (action === undefined) {
    return (
      <div {...marks}>
        {heading}
        {help}
      </div>
    )
  }

  return (
    <div {...marks} className="flex items-end justify-between gap-4">
      {help === null ? (
        heading
      ) : (
        <div>
          {heading}
          {help}
        </div>
      )}
      {action}
    </div>
  )
}

interface SettingRowProps {
  label: string
  // controlId is the id of the control the label names. The description's id is
  // `${controlId}-help` and the error's `${controlId}-error`, which the control points
  // aria-describedby at (rowDescribedBy).
  controlId: string
  description: ReactNode
  error?: string
  children: ReactNode
}

// SettingRow lays out one setting: its name and help on the left, the control on the right.
// "Find a setting" finds it by its label, and tints it while it matches.
export function SettingRow({ label, controlId, description, error = '', children }: SettingRowProps) {
  const [marks] = useSetting(label)

  return (
    <div {...marks} className={`flex items-center justify-between gap-8 px-4 py-3.5 ${rowMatchClass}`}>
      <div className="min-w-0">
        <label htmlFor={controlId} className="text-sm font-medium">
          {label}
        </label>
        <p id={`${controlId}-help`} className="mt-0.5 text-xs text-muted">
          {description}
        </p>
        <ErrorText id={`${controlId}-error`} error={error} className="mt-1" />
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  )
}

interface SwitchRowProps {
  label: string
  // controlId is the switch's id; see SettingRow.
  controlId: string
  description: ReactNode
  error?: string
  checked: boolean
  onChange: (on: boolean) => void
  // describedBy lists more ids that describe the switch, such as an error the section
  // shows below its rows.
  describedBy?: string | false
}

// SwitchRow is a setting turned on and off with a switch.
export function SwitchRow({ checked, onChange, describedBy = false, error = '', ...row }: SwitchRowProps) {
  return (
    <SettingRow {...row} error={error}>
      <Switch
        id={row.controlId}
        checked={checked}
        describedBy={rowDescribedBy(row.controlId, error, describedBy)}
        onChange={onChange}
      />
    </SettingRow>
  )
}

interface ChoiceRowProps {
  label: string
  description: ReactNode
  // error is why the last choice was refused, and errorId its id, which the control
  // points aria-describedby at.
  error?: string
  errorId?: string
  children: ReactNode
}

// ChoiceRow lays out one setting chosen with a segmented control: its name and help on
// the left, the choices on the right. The control carries its own accessible name.
// "Find a setting" finds it by its label, and tints it while it matches.
export function ChoiceRow({ label, description, error = '', errorId, children }: ChoiceRowProps) {
  const [marks] = useSetting(label)

  return (
    <div {...marks} className={`flex items-center justify-between gap-8 px-4 py-3.5 ${rowMatchClass}`}>
      <div className="min-w-0">
        <p className="text-sm font-medium">{label}</p>
        <p className="mt-0.5 text-xs text-muted">{description}</p>
        <ErrorText id={errorId} error={error} className="mt-1" />
      </div>
      {children}
    </div>
  )
}
