import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ScriptInput } from '../../platform'
import { bumpInput, scriptEntry } from '../../test/fixtures'
import { TaskInputsForm } from './TaskInputsForm'

// renderForm opens the form for a task asking for inputs.
function renderForm(inputs: ScriptInput[]) {
  const onRun = vi.fn<(values: Record<string, string>) => void>()
  const onCancel = vi.fn<() => void>()

  render(
    <TaskInputsForm
      entry={scriptEntry({ ref: 'tasks:Release', name: 'Release', inputs })}
      onRun={onRun}
      onCancel={onCancel}
    />,
  )

  return { onRun, onCancel, form: within(screen.getByRole('dialog', { name: 'Run “Release”' })) }
}

// versions is a pickString with more choices than fit as radio buttons.
function versions(): ScriptInput {
  return {
    id: 'version',
    kind: 'pickString',
    description: '',
    default: 'v1.3.0',
    options: ['v1.0.0', 'v1.1.0', 'v1.2.0', 'v1.3.0', 'v1.4.0', 'v1.5.0', 'v1.6.0', 'v1.7.0'].map((v) => ({
      label: v,
      value: v,
      shown: v,
    })),
  }
}

describe('TaskInputsForm', () => {
  it('starts on the first choice, says it asks for a value, and runs with it', async () => {
    const { onRun, form } = renderForm([bumpInput()])

    expect(form.getByText('This task asks for a value.')).toBeInTheDocument()
    expect(form.getByRole('group', { name: 'Which part of the version to bump' })).toBeInTheDocument()
    expect(form.getByRole('radio', { name: 'Patch (patch)' })).toHaveFocus()

    await userEvent.click(form.getByRole('radio', { name: 'Minor (minor)' }))
    await userEvent.click(form.getByRole('button', { name: 'Run' }))

    expect(onRun).toHaveBeenCalledExactlyOnceWith({ bump: 'minor' })
  })

  it('shows many choices as a menu, named by the input’s ID when it has no description', async () => {
    const { onRun, form } = renderForm([versions(), bumpInput()])

    const menu = form.getByRole('combobox', { name: 'version' })

    expect(form.getByText('This task asks for values.')).toBeInTheDocument()
    expect(within(menu).getByRole('option', { name: 'v1.3.0' })).toHaveProperty('selected', true)
    expect(menu).toHaveFocus()
    expect(within(menu).getAllByRole('option')).toHaveLength(8)

    await userEvent.selectOptions(menu, 'v1.7.0')
    expect(menu).toHaveValue('7')
    await userEvent.click(form.getByRole('button', { name: 'Run' }))

    expect(onRun).toHaveBeenCalledExactlyOnceWith({ version: 'v1.7.0', bump: 'patch' })
  })

  it('tells apart choices that fill in the same value, and sends the one chosen', async () => {
    const { onRun, form } = renderForm([
      bumpInput({
        default: 'patch',
        options: [
          { label: 'Patch', value: 'patch', shown: 'Patch (patch)' },
          { label: 'Fix', value: 'patch', shown: 'Fix (patch)' },
          { label: 'Minor', value: 'minor', shown: 'Minor (minor)' },
        ],
      }),
    ])

    expect(form.getByRole('radio', { name: 'Patch (patch)' })).toBeChecked()
    expect(form.getByRole('radio', { name: 'Fix (patch)' })).not.toBeChecked()

    await userEvent.click(form.getByRole('radio', { name: 'Fix (patch)' }))
    expect(form.getByRole('radio', { name: 'Fix (patch)' })).toBeChecked()
    expect(form.getByRole('radio', { name: 'Patch (patch)' })).not.toBeChecked()
    expect(form.getAllByRole('radio').filter((r) => (r as HTMLInputElement).checked)).toHaveLength(1)

    await userEvent.click(form.getByRole('button', { name: 'Run' }))
    expect(onRun).toHaveBeenCalledExactlyOnceWith({ bump: 'patch' })
  })

  it('tells apart menu choices that fill in the same value', async () => {
    const many = versions()
    const { onRun, form } = renderForm([
      { ...many, options: [...many.options, { label: 'latest', value: 'v1.7.0', shown: 'latest (v1.7.0)' }] },
    ])

    const menu = form.getByRole('combobox', { name: 'version' })
    expect(within(menu).getAllByRole('option')).toHaveLength(9)

    await userEvent.selectOptions(menu, 'latest (v1.7.0)')
    expect(within(menu).getByRole('option', { name: 'latest (v1.7.0)' })).toHaveProperty('selected', true)
    expect(within(menu).getByRole('option', { name: 'v1.7.0' })).toHaveProperty('selected', false)

    await userEvent.click(form.getByRole('button', { name: 'Run' }))
    expect(onRun).toHaveBeenCalledExactlyOnceWith({ version: 'v1.7.0' })
  })

  it('keeps the default of a choice with nothing to choose', async () => {
    const { onRun, form } = renderForm([bumpInput({ options: [] })])

    expect(form.queryByRole('radio')).not.toBeInTheDocument()
    await userEvent.click(form.getByRole('button', { name: 'Run' }))

    expect(onRun).toHaveBeenCalledExactlyOnceWith({ bump: 'patch' })
  })

  it('runs on Enter in a text field, and cancels on Escape', async () => {
    const { onRun, onCancel, form } = renderForm([
      { id: 'name', kind: 'promptString', description: 'Branch name', default: 'main', options: [] },
    ])

    const field = form.getByRole('textbox', { name: 'Branch name' })

    expect(field).toHaveFocus()
    await userEvent.type(field, '-next{Enter}')
    expect(onRun).toHaveBeenCalledExactlyOnceWith({ name: 'main-next' })

    await userEvent.keyboard('{Escape}')
    expect(onCancel).toHaveBeenCalledOnce()
  })
})
