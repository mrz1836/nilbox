import { useId, type ComponentPropsWithoutRef, type ReactNode } from 'react'

// Shared styles and small controls for forms: text fields, buttons, a segmented choice,
// and a select. Every control is a native element, so it works with the keyboard and screen
// readers as is.

export const fieldClass =
  'rounded-md border border-border bg-surface px-2.5 py-1.5 text-sm text-text placeholder:text-muted/70 disabled:opacity-disabled aria-invalid:border-danger'

export const buttonClass =
  'inline-flex items-center justify-center gap-1.5 rounded-md border border-border bg-surface px-3 py-1.5 text-sm font-medium text-text transition-colors hover:bg-raised disabled:cursor-default disabled:opacity-disabled disabled:hover:bg-surface'

// tintedButtonClass is the shape shared by the tinted buttons below: a wash of the tone
// with an edge of it, ink text, and a press that sinks a pixel. The text stays ink, not
// the tone, because tone text on a tone wash falls short of AA contrast in the light
// theme; an icon inside takes the tone instead. Disabled, the button turns neutral
// rather than fading, since a faded wash reads as mud on the dark theme. It sizes like
// buttonClass and fieldClass, so the three line up in a row.
const tintedButtonClass =
  'inline-flex items-center justify-center gap-1.5 rounded-md border px-3 py-1.5 text-sm font-medium text-text shadow-control inset-shadow-control transition enabled:active:translate-y-px disabled:cursor-default disabled:border-border disabled:bg-raised disabled:text-muted disabled:shadow-none disabled:inset-shadow-none disabled:[&>svg]:text-muted'

// primaryButtonClass marks a view's main action.
export const primaryButtonClass = `${tintedButtonClass} border-accent/45 bg-accent/14 enabled:hover:border-accent/60 enabled:hover:bg-accent/22 [&>svg]:text-accent`

// heldButtonClass is a control that still works beside a tinted button that is held back,
// such as a split button's menu, so the two halves read as one: it looks like the tinted
// button disabled, neutral and flat, but takes a quiet hover. Its neutral classes repeat
// tintedButtonClass's disabled ones rather than share a list, since Tailwind generates only
// the class names it finds written out whole.
export const heldButtonClass =
  'inline-flex items-center justify-center gap-1.5 rounded-md border border-border bg-raised px-3 py-1.5 text-sm font-medium text-muted transition enabled:active:translate-y-px hover:text-text [&>svg]:text-muted hover:[&>svg]:text-text'

// dangerButtonClass confirms a destructive action.
export const dangerButtonClass = `${tintedButtonClass} border-danger/45 bg-danger/14 enabled:hover:border-danger/60 enabled:hover:bg-danger/22 [&>svg]:text-danger`

export const quietButtonClass =
  'inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium text-muted transition-colors hover:bg-raised hover:text-text disabled:cursor-default disabled:opacity-disabled disabled:hover:bg-transparent'

export const chipClass = 'rounded-full border border-border px-2 py-0.5 text-xs whitespace-nowrap text-muted'

// captionClass sets a small uppercase caption over a group, such as a page's section
// heading in the Scripts tab; add the padding that lines it up.
export const captionClass = 'text-caption font-semibold tracking-wide text-muted uppercase'

// smallButtonClass is a bordered button for a dense bar, such as a peek's actions: shorter
// and smaller than buttonClass.
export const smallButtonClass =
  'inline-flex h-7 items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 text-xs font-medium text-text transition-colors hover:bg-raised disabled:cursor-default disabled:opacity-disabled disabled:hover:bg-surface'

// ghostButtonClass is a borderless button as tall as smallButtonClass, in the muted color
// that brightens on hover and while the menu it opens is open.
export const ghostButtonClass =
  'inline-flex h-7 items-center gap-1 rounded-md px-1.5 text-xs font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text'

// The icon buttons: a square holding an icon alone, named by its aria-label. Each is
// muted and brightens on hover. iconButtonClass is the usual size, smallIconButtonClass
// the one for a dense row, and dangerIconButtonClass a remove button, which turns the
// danger color on hover. toolbarButtonClass is a toolbar's, which stays lit while the
// panel it opens is open and holds a CountDot over its corner. A button that can be
// disabled adds disabled:cursor-default disabled:opacity-disabled
// disabled:hover:bg-transparent itself.
export const iconButtonClass =
  'flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text'
export const smallIconButtonClass =
  'flex size-6 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text'
export const dangerIconButtonClass =
  'flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-danger'
export const toolbarButtonClass =
  'relative flex size-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text'

// linkClass is a link or a link-like button in the accent color; inkLinkClass one in the
// text's own color that takes the accent on hover, such as an item's title.
export const linkClass = 'font-medium text-accent hover:underline'
export const inkLinkClass = 'text-text hover:text-accent hover:underline'

// modalClass is a modal dialog's card, dimming the page behind it; the caller adds its
// width, such as max-w-md. sheetDialogClass is the larger, unpadded shell of a sheet or a
// picker, which adds its own size, overflow, and layout.
export const modalClass =
  'm-auto w-[calc(100%-2rem)] rounded-lg border border-border bg-surface p-4 text-text shadow-lg backdrop:bg-scrim'
export const sheetDialogClass =
  'm-auto rounded-xl border border-border bg-surface p-0 text-text shadow-xl backdrop:bg-scrim'

// toolbarClass is the bar across the top of the window: its height, its edge, room for
// the window's buttons on its left, and dragging the window by it.
export const toolbarClass =
  'flex h-[52px] shrink-0 items-center gap-4 border-b border-border bg-surface pr-3 pl-[80px] select-none [--wails-draggable:drag]'

// switchClass draws a native checkbox as a switch; give it role="switch" so assistive
// tech announces on and off.
const switchClass =
  'relative h-5 w-9 shrink-0 appearance-none rounded-full bg-border transition-colors checked:bg-accent before:absolute before:top-0.5 before:left-0.5 before:size-4 before:rounded-full before:bg-paper before:shadow before:transition-transform checked:before:translate-x-4 disabled:opacity-disabled'

export interface SegmentOption<T extends string> {
  value: T
  label: string
  // title is the option's tooltip, if any.
  title?: string
  // icon sits before the label, if any; it is decorative, since the label names the option.
  icon?: ReactNode
  // disabled turns the option off alone, such as one this build can't do yet, whose title
  // then says why.
  disabled?: boolean
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
  const pad = size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-body'

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
          className={`relative flex items-center gap-1.5 rounded-[5px] ${pad} font-medium whitespace-nowrap text-muted transition-colors select-none hover:text-text has-checked:bg-raised has-checked:text-text has-checked:shadow-sm has-focus-visible:outline-2 has-focus-visible:outline-accent ${disabled || o.disabled === true ? 'opacity-disabled' : 'cursor-pointer'}`}
        >
          <input
            type="radio"
            name={name}
            value={o.value}
            checked={value === o.value}
            disabled={disabled || o.disabled === true}
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

export interface SelectOption<T extends string> {
  value: T
  label: string
  // title is the option's tooltip, if any.
  title?: string | undefined
  // disabled turns the option off alone, such as one the choice around it can't take.
  disabled?: boolean | undefined
}

// SelectGroup is a run of options under a heading, an optgroup; its heading names it among
// the others, so each is its own.
export interface SelectGroup<T extends string> {
  group: string
  options: readonly SelectOption<T>[]
}

// SelectProps are a select's own attributes (id, className, aria-*, title, disabled, ...),
// which it carries as they are, with its value, its options in order, and onChange, which
// receives the option chosen.
export type SelectProps<T extends string> = Omit<
  ComponentPropsWithoutRef<'select'>,
  'value' | 'defaultValue' | 'onChange' | 'children' | 'multiple'
> & {
  value: T
  options: readonly (SelectOption<T> | SelectGroup<T>)[]
  onChange: (value: T) => void
}

// selectOption draws one option of a Select.
function selectOption<T extends string>(o: SelectOption<T>) {
  return (
    <option key={o.value} value={o.value} title={o.title} disabled={o.disabled}>
      {o.label}
    </option>
  )
}

// Select is a native select of options typed T, for a choice among strings: the value the
// DOM reports is read back through the options, so onChange receives one of theirs. Its
// options' values are unique within it, as are its groups' headings; a heading may equal
// an option's value, since the groups are keyed apart from the options.
export function Select<T extends string>({ value, options, onChange, ...attributes }: SelectProps<T>) {
  const listed = options.flatMap((o) => ('group' in o ? o.options : [o]))

  return (
    <select
      {...attributes}
      value={value}
      onChange={(e) => {
        const chosen = listed.find((o) => o.value === e.target.value)

        if (chosen !== undefined) {
          onChange(chosen.value)
        }
      }}
    >
      {options.map((o) =>
        'group' in o ? (
          <optgroup key={`group:${o.group}`} label={o.group}>
            {o.options.map(selectOption)}
          </optgroup>
        ) : (
          selectOption(o)
        ),
      )}
    </select>
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
