import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { describedBy, Field, fromTriState, Segmented, Switch, triState } from './controls'

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
    expect(toggle).toHaveClass('mt-0.5')
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
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveAccessibleDescription('Too long.')
    expect(screen.queryByText('Shown on the button.')).not.toBeInTheDocument()
  })

  it('points at nothing without a hint or an error', () => {
    expect(describedBy('x', undefined, false)).toBeUndefined()
  })
})
