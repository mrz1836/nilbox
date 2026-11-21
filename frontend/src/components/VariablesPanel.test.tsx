import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { variables } from '../test/fixtures'
import { VariablesPanel, type VariablesPanelProps } from './VariablesPanel'

function props(overrides: Partial<VariablesPanelProps> = {}): VariablesPanelProps {
  return {
    variables: variables(),
    values: { Title: 'Add the widget cache', Number: '7', URL: '' },
    valuesFor: '#7',
    forIssuesOnly: false,
    onInsertVariable: vi.fn(),
    onInsertText: vi.fn(),
    ...overrides,
  }
}

describe('VariablesPanel', () => {
  it('lists the variables by group with their values for the preview item', () => {
    render(<VariablesPanel {...props()} />)

    const item = within(screen.getByRole('region', { name: 'Item' }))
    const title = item.getByRole('button', { name: /\{\{\.Title\}\}/ })

    expect(title).toHaveTextContent('The title, cut to 200 characters')
    expect(title).toHaveTextContent('Add the widget cache')
    expect(item.getByRole('button', { name: /\{\{\.URL\}\}/ })).toHaveTextContent('empty')
    expect(screen.getByRole('region', { name: 'Pull request' })).toHaveTextContent('PR only')
    expect(screen.getByText(/Values are for #7/)).toBeInTheDocument()
  })

  it('lists Autopilot’s variables in a group of their own, after the session’s', () => {
    render(
      <VariablesPanel
        {...props({
          variables: [
            ...variables(),
            {
              name: 'Routine',
              group: 'Autopilot',
              type: 'text',
              description: 'The routine that started the run, or empty for a click',
              prOnly: false,
              itemOnly: true,
            },
            {
              name: 'Unattended',
              group: 'Autopilot',
              type: 'yes/no',
              description: 'Autopilot started the run, with no one watching',
              prOnly: false,
              itemOnly: true,
            },
          ],
        })}
      />,
    )

    const regions = screen.getAllByRole('region').map((r) => r.getAttribute('aria-label'))
    expect(regions).toEqual(['Item', 'Repository', 'Pull request', 'Session', 'Autopilot', 'Syntax'])

    const autopilot = within(screen.getByRole('region', { name: 'Autopilot' }))
    expect(autopilot.getByRole('button', { name: /\{\{\.Routine\}\}/ })).toBeInTheDocument()
    expect(autopilot.getByRole('button', { name: /\{\{\.Unattended\}\}/ })).toHaveTextContent(
      'Autopilot started the run, with no one watching',
    )
  })

  it('inserts a variable or a snippet on click', async () => {
    const onInsertVariable = vi.fn()
    const onInsertText = vi.fn()
    render(<VariablesPanel {...props({ onInsertVariable, onInsertText })} />)

    await userEvent.click(screen.getByRole('button', { name: /\{\{\.Number\}\}/ }))
    expect(onInsertVariable).toHaveBeenCalledWith('Number')

    const syntax = within(screen.getByRole('region', { name: 'Syntax' }))
    await userEvent.click(syntax.getByRole('button', { name: /Text only when a value is set/ }))
    expect(onInsertText).toHaveBeenCalledWith('{{if .InWorktree}}\n\n{{end}}')

    await userEvent.click(syntax.getByRole('button', { name: /into a command as one word/ }))
    expect(onInsertText).toHaveBeenCalledWith('{{shellquote .Branch}}')
  })

  it('searches names and descriptions', async () => {
    render(<VariablesPanel {...props()} />)

    await userEvent.type(screen.getByRole('searchbox', { name: 'Search variables' }), 'worktree')

    expect(screen.getAllByRole('button').map((b) => b.title)).toEqual(['Insert {{.InWorktree}}'])
    expect(screen.queryByRole('region', { name: 'Syntax' })).not.toBeInTheDocument()

    await userEvent.clear(screen.getByRole('searchbox', { name: 'Search variables' }))
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search variables' }), 'zzz')

    expect(screen.getByText('No variable matches “zzz”.')).toBeInTheDocument()
  })

  it('dims pull request variables for an issue prompt, and waits for values', () => {
    render(<VariablesPanel {...props({ forIssuesOnly: true, values: null })} />)

    const pr = within(screen.getByRole('region', { name: 'Pull request' }))
    const item = within(screen.getByRole('region', { name: 'Item' }))

    expect(pr.getByRole('button', { name: /\{\{\.Branch\}\}/ })).toHaveClass('opacity-60')
    expect(item.getByRole('button', { name: /\{\{\.Title\}\}/ })).not.toHaveTextContent('Add the widget cache')
  })
})
