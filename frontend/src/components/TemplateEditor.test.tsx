import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { Diagnostic } from '../platform'
import { variables } from '../test/fixtures'
import { TemplateEditor, type TemplateEditorHandle } from './TemplateEditor'

interface HarnessProps {
  initial: string
  diagnostics?: Diagnostic[]
  onChange?: (value: string) => void
  handle?: React.Ref<TemplateEditorHandle>
}

// Harness holds the editor's text the way the prompt editor does.
function Harness({ initial, diagnostics = [], onChange, handle }: HarnessProps) {
  const [value, setValue] = useState(initial)

  return (
    <TemplateEditor
      id="body"
      label="Prompt text"
      value={value}
      onChange={(v) => {
        setValue(v)
        onChange?.(v)
      }}
      variables={variables()}
      diagnostics={diagnostics}
      ref={handle}
    />
  )
}

function textarea(): HTMLTextAreaElement {
  return screen.getByRole('textbox', { name: 'Prompt text' })
}

describe('TemplateEditor', () => {
  it('draws the text colored behind the text area', () => {
    const { container } = render(<Harness initial="Review {{.Title}} {{.Nope}}" />)

    const backdrop = container.querySelector('pre')

    expect(backdrop).toHaveAttribute('aria-hidden', 'true')
    expect(backdrop).toHaveTextContent('Review {{.Title}} {{.Nope}}')
    expect(screen.getByText('.Title')).toHaveClass('text-info')
    expect(screen.getByText('.Nope')).toHaveClass('text-danger')
    expect(textarea()).toHaveValue('Review {{.Title}} {{.Nope}}')
    expect(textarea()).toHaveAttribute('spellcheck', 'false')
  })

  it('underlines the backend’s diagnostics', () => {
    render(
      <Harness
        initial="Review {{.Titel}}"
        diagnostics={[{ severity: 'error', line: 1, column: 10, length: 6, message: 'bad' }]}
      />,
    )

    expect(screen.getByText('.Titel')).toHaveClass('decoration-danger')
  })

  it('offers variables after "{{", filters them as you type, and inserts the chosen one', async () => {
    const onChange = vi.fn()
    render(<Harness initial="" onChange={onChange} />)

    await userEvent.type(textarea(), 'Go {{{{')

    const list = screen.getByRole('listbox', { name: 'Variables' })

    expect(list).toBeInTheDocument()
    expect(textarea()).toHaveAttribute('aria-controls', list.id)
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toContain('.TitleThe title, cut to 200 characters')

    await userEvent.type(textarea(), '.Ti')

    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(screen.getByRole('option', { selected: true })).toHaveTextContent('.Title')

    await userEvent.keyboard('{Enter}')

    expect(textarea()).toHaveValue('Go {{.Title}}')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('moves through the variables with the arrows and closes on Escape', async () => {
    const onKeyDown = vi.fn()
    document.addEventListener('keydown', onKeyDown)
    render(<Harness initial="" />)

    await userEvent.type(textarea(), '{{{{.')

    const options = screen.getAllByRole('option')

    expect(options[0]).toHaveAttribute('aria-selected', 'true')

    await userEvent.keyboard('{ArrowDown}')
    expect(options[1]).toHaveAttribute('aria-selected', 'true')
    expect(textarea()).toHaveAttribute('aria-activedescendant', options[1]?.id)

    await userEvent.keyboard('{ArrowUp}{ArrowUp}')
    expect(options.at(-1)).toHaveAttribute('aria-selected', 'true')

    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(onKeyDown).not.toHaveBeenCalledWith(expect.objectContaining({ key: 'Escape' }))
    document.removeEventListener('keydown', onKeyDown)
  })

  it('inserts a variable with a click and keeps an open action open', () => {
    render(<Harness initial="{{if .In}}x{{end}}" />)

    const area = textarea()
    area.setSelectionRange(8, 8)
    fireEvent.click(area)

    const option = screen.getByRole('option', { name: /InWorktree/ })

    fireEvent.mouseDown(option)

    expect(area).toHaveValue('{{if .InWorktree}}x{{end}}')
  })

  it('inserts variables and text where the caret is, and reveals a spot', () => {
    const handle = createRef<TemplateEditorHandle>()
    render(<Harness initial="Hello  world" handle={handle} />)

    const area = textarea()
    area.setSelectionRange(6, 6)

    act(() => {
      handle.current?.insertVariable('Title')
    })
    expect(area).toHaveValue('Hello {{.Title}} world')

    area.setSelectionRange(9, 9)
    act(() => {
      handle.current?.insertVariable('URL')
    })
    expect(area).toHaveValue('Hello {{..URLTitle}} world')

    area.setSelectionRange(0, 5)
    act(() => {
      handle.current?.insertText('Bye')
    })
    expect(area).toHaveValue('Bye {{..URLTitle}} world')

    act(() => {
      handle.current?.reveal(1, 5, 2)
    })
    expect(area).toHaveFocus()
    expect([area.selectionStart, area.selectionEnd]).toEqual([4, 6])
  })

  it('closes the variables when the text area loses focus', async () => {
    render(<Harness initial="" />)

    await userEvent.type(textarea(), '{{{{')
    expect(screen.getByRole('listbox')).toBeInTheDocument()

    fireEvent.blur(textarea())
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })
})
