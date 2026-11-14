import { useId, type ReactNode } from 'react'

// Shared styles and small controls for forms: text fields, buttons, and a segmented
// choice. Every control is a native element, so it works with the keyboard and screen
// readers as is.

export const fieldClass =
  'rounded-md border border-border bg-surface px-2.5 py-1.5 text-sm text-text placeholder:text-muted/70 disabled:opacity-60 aria-invalid:border-danger'

export const buttonClass =
  'inline-flex items-center justify-center gap-1.5 rounded-md border border-border bg-surface px-3 py-1.5 text-sm font-medium text-text transition-colors hover:bg-raised disabled:cursor-default disabled:opacity-50 disabled:hover:bg-surface'

// tintedButtonClass is the shape shared by the tinted buttons below: a wash of the tone
// with an edge of it, ink text, and a press that sinks a pixel. The text stays ink, not
// the tone, because tone text on a tone wash falls short of AA contrast in the light
// theme; an icon inside takes the tone instead. Disabled, the button turns neutral
// rather than fading, since a faded wash reads as mud on the dark theme. It sizes like
// buttonClass and fieldClass, so the three line up in a row.
const tintedButtonClass =
  'inline-flex items-center justify-center gap-1.5 rounded-md border px-3 py-1.5 text-sm font-medium text-text shadow-xs shadow-black/15 inset-shadow-[0_1px_0_rgb(255_255_255/0.06)] transition enabled:active:translate-y-px disabled:cursor-default disabled:border-border disabled:bg-raised disabled:text-muted disabled:shadow-none disabled:inset-shadow-none disabled:[&>svg]:text-muted'

// primaryButtonClass marks a view's main action.
export const primaryButtonClass = `${tintedButtonClass} border-accent/45 bg-accent/14 enabled:hover:border-accent/65 enabled:hover:bg-accent/22 [&>svg]:text-accent`

// heldButtonClass is a control that still works beside a tinted button that is held back,
// such as a split button's menu, so the two halves read as one: it looks like the tinted
// button disabled, neutral and flat, but takes a quiet hover. Its neutral classes repeat
// tintedButtonClass's disabled ones rather than share a list, since Tailwind generates only
// the class names it finds written out whole.
export const heldButtonClass =
  'inline-flex items-center justify-center gap-1.5 rounded-md border border-border bg-raised px-3 py-1.5 text-sm font-medium text-muted transition enabled:active:translate-y-px hover:text-text [&>svg]:text-muted hover:[&>svg]:text-text'

// dangerButtonClass confirms a destructive action.
export const dangerButtonClass = `${tintedButtonClass} border-danger/45 bg-danger/14 enabled:hover:border-danger/65 enabled:hover:bg-danger/22 [&>svg]:text-danger`

export const quietButtonClass =
  'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium text-muted transition-colors hover:bg-raised hover:text-text disabled:cursor-default disabled:opacity-50 disabled:hover:bg-transparent'

export const chipClass = 'rounded-full border border-border px-2 py-0.5 text-xs whitespace-nowrap text-muted'

// captionClass sets a small uppercase caption over a group, such as a page's section
// heading in the Scripts tab; add the padding that lines it up.
export const captionClass = 'text-[0.6875rem] font-semibold tracking-wide text-muted uppercase'

// switchClass draws a native checkbox as a switch; give it role="switch" so assistive
// tech announces on and off.
export const switchClass =
  'relative h-5 w-9 shrink-0 appearance-none rounded-full bg-border transition-colors checked:bg-accent before:absolute before:top-0.5 before:left-0.5 before:size-4 before:rounded-full before:bg-white before:shadow before:transition-transform checked:before:translate-x-4 disabled:opacity-60'

export interface SegmentOption<T extends string> {
  value: T
  label: string
  // title is the option's tooltip, if any.
  title?: string
  // icon sits before the label, if any; it is decorative, since the label names the option.
  icon?: ReactNode
}

export interface SegmentedProps<T extends string> {
  // label names the choice for assistive tech.
  label: string
  value: T
  options: readonly SegmentOption<T>[]
  onChange: (value: T) => void
  disabled?: boolean
  // size is "sm" for dense rows.
  size?: 'sm' | 'md'
  // describedBy lists the ids of the text that describes the choice, such as why the
  // last one was refused.
  describedBy?: string | undefined
}

// Segmented is a row of mutually exclusive options drawn as one control: radio buttons
// underneath, so arrow keys move between them.
export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
  disabled = false,
  size = 'md',
  describedBy,
}: SegmentedProps<T>) {
  const name = useId()
  const pad = size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-[0.8125rem]'

  return (
    <div
      role="radiogroup"
      aria-label={label}
      aria-describedby={describedBy}
      className="inline-flex shrink-0 rounded-md border border-border bg-background p-0.5"
    >
      {options.map((o) => (
        <label
          key={o.value}
          title={o.title}
          className={`relative flex items-center gap-1.5 rounded-[5px] ${pad} font-medium whitespace-nowrap text-muted transition-colors select-none hover:text-text has-checked:bg-raised has-checked:text-text has-checked:shadow-sm has-focus-visible:outline-2 has-focus-visible:outline-accent ${disabled ? 'opacity-60' : 'cursor-pointer'}`}
        >
          <input
            type="radio"
            name={name}
            value={o.value}
            checked={value === o.value}
            disabled={disabled}
            onChange={() => {
              onChange(o.value)
            }}
            className="sr-only"
          />
          {o.icon}
          {o.label}
        </label>
      ))}
    </div>
  )
}

export interface SwitchProps {
  id: string
  checked: boolean
  onChange: (on: boolean) => void
  // describedBy lists the ids of the text that describes the switch, if any.
  describedBy?: string | undefined
  disabled?: boolean
  // title is the switch's tooltip, if any.
  title?: string | undefined
  // className adds to the switch's own styles, such as a margin to line it up.
  className?: string
}

// Switch is a checkbox drawn as a switch, with role="switch" so assistive tech announces
// it as on or off rather than checked.
export function Switch({ id, checked, onChange, describedBy, disabled = false, title, className = '' }: SwitchProps) {
  return (
    <input
      id={id}
      type="checkbox"
      role="switch"
      aria-describedby={describedBy}
      checked={checked}
      disabled={disabled}
      title={title}
      onChange={(e) => {
        onChange(e.target.checked)
      }}
      className={className === '' ? switchClass : `${switchClass} ${className}`}
    />
  )
}

// Inherit, on, or off: a launch setting that is either set or left to the layer above.
export type TriState = 'inherit' | 'on' | 'off'

// triState reads an optional setting as a TriState.
export function triState(v: boolean | null): TriState {
  if (v === null) {
    return 'inherit'
  }

  return v ? 'on' : 'off'
}

// fromTriState turns a TriState back into an optional setting.
export function fromTriState(v: TriState): boolean | null {
  if (v === 'inherit') {
    return null
  }

  return v === 'on'
}

export interface FieldProps {
  label: string
  // controlId is the id of the control the label names.
  controlId: string
  // hint is help shown under the control while there is no error.
  hint?: ReactNode
  error?: string | undefined
  // aside sits at the end of the label line, such as a character count.
  aside?: ReactNode
  children: ReactNode
}

// Field lays out a labeled control with its hint or error below it. The hint and error
// carry ids the control can point aria-describedby at: `${controlId}-hint` and
// `${controlId}-error`.
export function Field({ label, controlId, hint, error = '', aside, children }: FieldProps) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex items-baseline justify-between gap-2">
        <label htmlFor={controlId} className="text-xs font-medium text-muted">
          {label}
        </label>
        {aside}
      </div>
      {children}
      {error !== '' ? (
        <p id={`${controlId}-error`} role="alert" className="text-xs text-danger">
          {error}
        </p>
      ) : (
        hint !== undefined && (
          <p id={`${controlId}-hint`} className="text-xs text-muted">
            {hint}
          </p>
        )
      )}
    </div>
  )
}

// describedBy returns the ids a Field's control should point aria-describedby at.
export function describedBy(controlId: string, error: string | undefined, hasHint: boolean): string | undefined {
  if (error !== undefined && error !== '') {
    return `${controlId}-error`
  }

  return hasHint ? `${controlId}-hint` : undefined
}
