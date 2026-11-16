import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { TokenField, type TokenFieldProps } from './TokenField'

// Field holds the tokens as the builder does, and shows them as stored.
function Field({ start = [], ...props }: Partial<TokenFieldProps> & { start?: string[] }) {
  const [tokens, setTokens] = useState<string[]>(start)

  return (
    <>
      <TokenField
        id="authors"
        label="Opened by"
        tokens={tokens}
        onChange={setTokens}
        suggestions={[
          { value: 'app/dependabot', label: 'Dependabot' },
          { value: 'app/renovate', label: 'Renovate' },
          { value: 'octo-reviewer', label: 'octo-reviewer' },
        ]}
        {...props}
      />
      <output aria-label="Stored">{tokens.join('|')}</output>
    </>
  )
}

const stored = () => screen.getByRole('status', { name: 'Stored' })
const field = () => screen.getByRole('combobox', { name: 'Opened by' })

describe('a token field', () => {
  it('adds a suggestion picked with the arrows and Enter, and what is typed with a comma', async () => {
    const user = userEvent.setup()
    render(<Field toValue={(t) => t.trim().toLowerCase()} />)

    await user.click(field())
    expect(screen.getByRole('listbox', { name: 'Opened by suggestions' })).toBeInTheDocument()

    await user.keyboard('{ArrowDown}{ArrowDown}{ArrowUp}{Enter}')
    expect(stored()).toHaveTextContent('app/dependabot')
    expect(screen.getByRole('button', { name: 'Remove app/dependabot' })).toBeInTheDocument()

    await user.type(field(), 'Octo-Other,')
    expect(stored()).toHaveTextContent('app/dependabot|octo-other')
    expect(field()).toHaveValue('')

    // The arrows wrap around, and Up from nothing picked takes the last.
    await user.keyboard('{ArrowUp}')
    expect(field()).toHaveAttribute('aria-activedescendant')
    await user.keyboard('{ArrowDown}{Enter}')
    expect(stored()).toHaveTextContent('app/dependabot|octo-other|app/renovate')
  })

  it('narrows what it suggests to what is typed, leaving out what it holds, and takes a click', async () => {
    const user = userEvent.setup()
    render(<Field start={['app/dependabot']} tokenLabel={(v) => (v === 'app/dependabot' ? 'Dependabot' : v)} />)

    await user.type(field(), 'reno')
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Renovate'])

    await user.hover(screen.getByRole('option', { name: 'Renovate' }))
    expect(screen.getByRole('option', { name: 'Renovate' })).toHaveAttribute('aria-selected', 'true')

    await user.click(screen.getByRole('option', { name: 'Renovate' }))
    expect(stored()).toHaveTextContent('app/dependabot|app/renovate')

    await user.keyboard('xyz')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('takes one away with Backspace in an empty field, or with its ×, and never adds one twice', async () => {
    const user = userEvent.setup()
    render(<Field start={['app/dependabot', 'octo-reviewer']} />)

    await user.click(screen.getByRole('button', { name: 'Remove app/dependabot' }))
    expect(stored()).toHaveTextContent('octo-reviewer')

    await user.click(field())
    await user.keyboard('{Backspace}')
    expect(stored()).toHaveTextContent('')

    await user.keyboard('octo-reviewer{Enter}OCTO-REVIEWER{Enter}')
    expect(stored()).toHaveTextContent('octo-reviewer')
    expect(stored().textContent).toBe('octo-reviewer')

    // Enter and a comma with nothing typed add nothing, and Backspace with nothing held
    // takes nothing.
    await user.keyboard('{Backspace}{Backspace}{Enter},')
    expect(stored().textContent).toBe('')
  })

  it('closes its suggestions with Escape, and opens them again with the arrows', async () => {
    const user = userEvent.setup()
    render(<Field />)

    await user.click(field())
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    // Escape with nothing to close lets the key go on.
    await user.keyboard('{Escape}')
    await user.keyboard('{ArrowDown}')
    expect(screen.getByRole('listbox')).toBeInTheDocument()

    await user.tab()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('takes only a suggestion in a field of choices', async () => {
    const user = userEvent.setup()
    render(<Field choicesOnly />)

    await user.type(field(), 'nobody{Enter}')
    expect(stored().textContent).toBe('')

    await user.clear(field())
    await user.type(field(), 'reno{Enter}')
    expect(stored()).toHaveTextContent('app/renovate')

    await user.type(field(), '{Enter}')
    expect(stored().textContent).toBe('app/renovate')
  })

  it('leaves an input method’s Enter and comma to it, WebKit’s confirming Enter too', async () => {
    const user = userEvent.setup()
    render(<Field start={['octo-reviewer']} suggestions={[]} />)

    await user.click(field())
    fireEvent.compositionStart(field())
    fireEvent.change(field(), { target: { value: 'ni,' } })
    expect(field()).toHaveValue('ni,')
    fireEvent.keyDown(field(), { key: 'Enter', isComposing: true })
    fireEvent.change(field(), { target: { value: '你' } })
    fireEvent.compositionEnd(field())
    // WebKit ends the composition before the keydown of the Enter that confirms it.
    fireEvent.keyDown(field(), { key: 'Enter', keyCode: 229 })
    expect(stored().textContent).toBe('octo-reviewer')

    fireEvent.keyDown(field(), { key: 'Enter' })
    expect(stored().textContent).toBe('octo-reviewer|你')
  })

  it('keeps focus in the field once a token’s × takes it away, and names its list only while it shows', async () => {
    const user = userEvent.setup()
    render(<Field start={['octo-reviewer']} invalid describedBy="why" />)

    expect(field()).not.toHaveAttribute('aria-controls')
    expect(field()).toHaveAttribute('aria-describedby', 'why')

    await user.click(screen.getByRole('button', { name: 'Remove octo-reviewer' }))
    expect(field()).toHaveFocus()
    expect(field()).toHaveAttribute('aria-controls', screen.getByRole('listbox').id)
  })

  it('arrows through nothing when it has nothing to suggest', async () => {
    const user = userEvent.setup()
    render(<Field suggestions={[]} invalid />)

    expect(field()).toHaveAttribute('aria-invalid', 'true')
    await user.click(field())
    await user.keyboard('{ArrowDown}{ArrowUp}')
    expect(field()).not.toHaveAttribute('aria-activedescendant')
  })
})
