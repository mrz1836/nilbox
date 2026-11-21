import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import {
  dangerIconButtonClass,
  describedBy,
  ErrorText,
  Field,
  fromTriState,
  ghostButtonClass,
  iconButtonClass,
  inkLinkClass,
  linkClass,
  modalClass,
  Segmented,
  Select,
  sheetDialogClass,
  smallButtonClass,
  smallIconButtonClass,
  Switch,
  toolbarButtonClass,
  toolbarClass,
  triState,
  type SelectOption,
} from './controls'

describe('the shared classes', () => {
  it.each([
    [
      'smallButtonClass',
      smallButtonClass,
      'inline-flex h-7 items-center gap-1.5 rounded-md border border-border bg-surface px-2.5 text-xs font-medium text-text transition-colors hover:bg-raised disabled:cursor-default disabled:opacity-disabled disabled:hover:bg-surface',
    ],
    [
      'ghostButtonClass',
      ghostButtonClass,
      'inline-flex h-7 items-center gap-1 rounded-md px-1.5 text-xs font-medium text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text',
    ],
    [
      'iconButtonClass',
      iconButtonClass,
      'flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text',
    ],
    [
      'smallIconButtonClass',
      smallIconButtonClass,
      'flex size-6 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text',
    ],
    [
      'dangerIconButtonClass',
      dangerIconButtonClass,
      'flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-danger',
    ],
    [
      'toolbarButtonClass',
      toolbarButtonClass,
      'relative flex size-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text aria-expanded:bg-raised aria-expanded:text-text',
    ],
    ['linkClass', linkClass, 'font-medium text-accent hover:underline'],
    ['inkLinkClass', inkLinkClass, 'text-text hover:text-accent hover:underline'],
    [
      'modalClass',
      modalClass,
      'm-auto w-[calc(100%-2rem)] rounded-lg border border-border bg-surface p-4 text-text shadow-lg backdrop:bg-scrim',
    ],
    [
      'sheetDialogClass',
      sheetDialogClass,
      'm-auto rounded-xl border border-border bg-surface p-0 text-text shadow-xl backdrop:bg-scrim',
    ],
    [
      'toolbarClass',
      toolbarClass,
      'flex h-[52px] shrink-0 items-center gap-4 border-b border-border bg-surface pr-3 pl-[80px] select-none [--wails-draggable:drag]',
    ],
  ])('pins %s to its class string', (_, given, want) => {
    expect(given).toBe(want)
  })

  it('sizes and colors the icon buttons from one shape', () => {
    const diff = (a: string, b: string) => {
      const [as, bs] = [a.split(' '), b.split(' ')]

      return [...as.filter((c) => !bs.includes(c)), ...bs.filter((c) => !as.includes(c))].sort()
    }

    expect(diff(iconButtonClass, smallIconButtonClass)).toEqual(['size-6', 'size-7'])
    expect(diff(iconButtonClass, dangerIconButtonClass)).toEqual(['hover:text-danger', 'hover:text-text'])
  })

  it('fades a small button while it is disabled, and leaves the fade to an icon button’s caller', () => {
    render(
      <>
        <button type="button" disabled className={smallButtonClass}>
          Copy
        </button>
        <button type="button" aria-label="Close" className={iconButtonClass} />
      </>,
    )

    expect(screen.getByRole('button', { name: 'Copy' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Copy' })).toHaveClass('disabled:opacity-disabled')
    expect(screen.getByRole('button', { name: 'Close' }).className).not.toContain('opacity')
  })
})

describe('Segmented', () => {
  it('is a group of radios, one checked', async () => {
    const onChange = vi.fn()
    render(
      <Segmented
        label="For"
        value="pr"
        options={[
          { value: 'pr', label: 'Pull requests' },
          { value: 'issue', label: 'Issues', title: 'Issues only' },
        ]}
        onChange={onChange}
      />,
    )

    expect(screen.getByRole('radiogroup', { name: 'For' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Pull requests' })).toBeChecked()

    await userEvent.click(screen.getByRole('radio', { name: 'Issues' }))

    expect(onChange).toHaveBeenCalledWith('issue')
  })

  it('can be disabled', () => {
    render(
      <Segmented label="For" value="a" options={[{ value: 'a', label: 'A' }]} onChange={vi.fn()} disabled size="sm" />,
    )

    expect(screen.getByRole('radio', { name: 'A' })).toBeDisabled()
    expect(screen.getByText('A')).toHaveClass('opacity-disabled')
    expect(screen.getByText('A')).not.toHaveClass('cursor-pointer')
  })

  it('points at the text that describes it', () => {
    render(
      <>
        <Segmented label="For" value="a" options={[{ value: 'a', label: 'A' }]} onChange={vi.fn()} describedBy="why" />
        <p id="why">That choice was refused.</p>
      </>,
    )

    expect(screen.getByRole('radiogroup', { name: 'For' })).toHaveAccessibleDescription('That choice was refused.')
  })
})

describe('Select', () => {
  type Every = 'hour' | 'day' | 'week'

  const every: readonly SelectOption<Every>[] = [
    { value: 'hour', label: 'Every hour' },
    { value: 'day', label: 'Every day', title: 'At the time below' },
    { value: 'week', label: 'Every week', disabled: true },
  ]

  it('hands onChange the option chosen, typed', async () => {
    const onChange = vi.fn<(value: Every) => void>()
    render(<Select aria-label="How often" value="hour" options={every.slice(0, 2)} onChange={onChange} />)

    const select = screen.getByRole('combobox', { name: 'How often' })
    expect(select).toHaveValue('hour')

    await userEvent.selectOptions(select, 'Every day')

    expect(onChange).toHaveBeenCalledExactlyOnceWith('day')
  })

  it('renders the select its attributes and options make, groups and all', () => {
    const { container: drawn } = render(
      <Select
        id="when"
        aria-describedby="when-hint"
        aria-invalid
        title="When it fires"
        className="rounded-md py-1"
        value="day"
        options={[
          { value: '', label: 'always' },
          { group: 'Often', options: every.slice(0, 2) },
          { group: 'Seldom', options: every.slice(2) },
          { group: 'Never', options: [] },
        ]}
        onChange={vi.fn()}
      />,
    )
    const { container: written } = render(
      <select
        id="when"
        aria-describedby="when-hint"
        aria-invalid
        title="When it fires"
        className="rounded-md py-1"
        value="day"
        onChange={vi.fn()}
      >
        <option value="">always</option>
        <optgroup label="Often">
          <option value="hour">Every hour</option>
          <option value="day" title="At the time below">
            Every day
          </option>
        </optgroup>
        <optgroup label="Seldom">
          <option value="week" disabled>
            Every week
          </option>
        </optgroup>
        <optgroup label="Never" />
      </select>,
    )

    const select = within(drawn).getByRole('combobox')

    expect(select.isEqualNode(within(written).getByRole('combobox'))).toBe(true)
    expect(select).toHaveValue('day')
    expect(within(drawn).getByRole('option', { name: 'Every week' })).toBeDisabled()
  })

  it('keys a group apart from an option whose value is the group’s heading', () => {
    const logged = vi.spyOn(console, 'error')
    render(
      <Select
        aria-label="How often"
        value="Often"
        options={[
          { value: 'Often', label: 'Often' },
          { group: 'Often', options: every.slice(0, 2) },
        ]}
        onChange={vi.fn()}
      />,
    )

    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Often', 'Every hour', 'Every day'])
    expect(screen.getByRole('group', { name: 'Often' })).toBeInTheDocument()
    expect(logged).not.toHaveBeenCalled()
  })

  it('reads a choice from within a group, and can be disabled as a whole', async () => {
    const onChange = vi.fn<(value: Every | '') => void>()
    const { rerender } = render(
      <Select
        aria-label="How often"
        value=""
        options={[
          { value: '', label: 'always' },
          { group: 'Often', options: every.slice(0, 2) },
        ]}
        onChange={onChange}
      />,
    )

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'How often' }), 'hour')

    expect(onChange).toHaveBeenCalledExactlyOnceWith('hour')

    rerender(<Select aria-label="How often" value="" options={[]} onChange={onChange} disabled />)

    expect(screen.getByRole('combobox', { name: 'How often' })).toBeDisabled()
  })

  it('ignores a value the DOM reports that none of its options has', () => {
    const onChange = vi.fn()
    render(<Select aria-label="How often" value="hour" options={every} onChange={onChange} />)

    const select = screen.getByRole('combobox', { name: 'How often' })
    const stray = document.createElement('option')
    stray.value = 'month'
    select.append(stray)

    fireEvent.change(select, { target: { value: 'month' } })

    expect(onChange).not.toHaveBeenCalled()
  })
})

describe('Switch', () => {
  it('is announced as a switch and reports whether it was turned on', async () => {
    const onChange = vi.fn()
    render(
      <>
        <label htmlFor="wrap">Wrap lines</label>
        <Switch id="wrap" checked={false} describedBy="wrap-help" onChange={onChange} />
        <p id="wrap-help">Long lines wrap to the width of the window.</p>
      </>,
    )

    const toggle = screen.getByRole('switch', { name: 'Wrap lines' })
    expect(toggle).not.toBeChecked()
    expect(toggle).toHaveAccessibleDescription('Long lines wrap to the width of the window.')

    await userEvent.click(toggle)

    expect(onChange).toHaveBeenCalledExactlyOnceWith(true)
  })

  it('can be held, with a tooltip saying why', () => {
    render(<Switch id="locked" checked disabled title="Always on" onChange={vi.fn()} className="mt-0.5" />)

    const toggle = screen.getByRole('switch')
    expect(toggle).toBeDisabled()
    expect(toggle).toHaveAttribute('title', 'Always on')
    expect(toggle).toHaveClass('mt-0.5', 'disabled:opacity-disabled')
  })
})

describe('tri-state settings', () => {
  it('reads and writes inherit, on, and off', () => {
    expect(triState(null)).toBe('inherit')
    expect(triState(true)).toBe('on')
    expect(triState(false)).toBe('off')
    expect(fromTriState('inherit')).toBeNull()
    expect(fromTriState('on')).toBe(true)
    expect(fromTriState('off')).toBe(false)
  })
})

describe('ErrorText', () => {
  it('shows nothing while there is no error, prefix or not', () => {
    const { container } = render(
      <ErrorText id="save-error" error="" className="mt-1">
        Couldn’t save:{' '}
      </ErrorText>,
    )

    expect(container).toBeEmptyDOMElement()
  })

  it('announces the error after its prefix, under the id a control points at', () => {
    render(
      <>
        <input aria-label="Name" aria-describedby="save-error" />
        <ErrorText id="save-error" error="the disk is full." className="mt-1">
          Couldn’t save:{' '}
        </ErrorText>
      </>,
    )

    const alert = screen.getByRole('alert')
    expect(alert.tagName).toBe('P')
    expect(alert).toHaveTextContent('Couldn’t save: the disk is full.')
    expect(alert).toHaveAttribute('id', 'save-error')
    expect(alert).toHaveAttribute('class', 'mt-1 text-xs text-danger')
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveAccessibleDescription(
      'Couldn’t save: the disk is full.',
    )
  })

  it('needs no id, placement, or prefix', () => {
    render(<ErrorText error="Too long." />)

    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent(/^Too long\.$/)
    expect(alert).not.toHaveAttribute('id')
    expect(alert).toHaveAttribute('class', 'text-xs text-danger')
  })
})

describe('Field', () => {
  it('labels a control and shows its hint, or its error instead', () => {
    const { rerender } = render(
      <Field label="Nickname" controlId="nick" hint="Shown on the button." aside={<span>6/20</span>}>
        <input id="nick" aria-describedby={describedBy('nick', '', true)} />
      </Field>,
    )

    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveAccessibleDescription('Shown on the button.')
    expect(screen.getByText('6/20')).toBeInTheDocument()

    rerender(
      <Field label="Nickname" controlId="nick" hint="Shown on the button." error="Too long.">
        <input id="nick" aria-describedby={describedBy('nick', 'Too long.', true)} />
      </Field>,
    )

    expect(screen.getByRole('alert')).toHaveTextContent('Too long.')
    expect(screen.getByRole('alert')).toHaveAttribute('id', 'nick-error')
    expect(screen.getByRole('alert')).toHaveAttribute('class', 'text-xs text-danger')
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveAccessibleDescription('Too long.')
    expect(screen.queryByText('Shown on the button.')).not.toBeInTheDocument()
  })

  it('points at nothing without a hint or an error', () => {
    expect(describedBy('x', undefined, false)).toBeUndefined()
  })
})
