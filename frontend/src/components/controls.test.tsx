import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { describedBy, Field, fromTriState, Segmented, triState } from './controls'

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
