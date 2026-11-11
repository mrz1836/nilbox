import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { newUserScript } from '../../lib/userScripts'
import type { UserScript } from '../../platform'
import { userScript } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ScriptEditor } from './ScriptEditor'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// renderEditor opens the editor on script, a new one unless given.
function renderEditor(script: UserScript = newUserScript()) {
  const onSaved = vi.fn<(saved: UserScript) => void>()
  const onCancel = vi.fn<() => void>()

  render(<ScriptEditor script={script} onSaved={onSaved} onCancel={onCancel} />)

  return { onSaved, onCancel, dialog: within(screen.getByRole('dialog')) }
}

describe('ScriptEditor', () => {
  it('makes a new script with every field', async () => {
    const { onSaved, dialog } = renderEditor()
    const save = dialog.getByRole('button', { name: 'Save' })

    expect(screen.getByRole('dialog', { name: 'New script' })).toBeInTheDocument()
    expect(dialog.getByRole('textbox', { name: 'Name' })).toHaveFocus()
    expect(dialog.getByRole('radio', { name: 'All repositories' })).toBeChecked()
    expect(dialog.getByRole('radio', { name: 'As set in Settings' })).toBeChecked()
    expect(dialog.getByRole('radio', { name: 'Play' })).toBeChecked()
    expect(dialog.getByRole('checkbox', { name: 'Ask before running' })).not.toBeChecked()
    expect(dialog.getByRole('checkbox', { name: 'Ask before running' })).toHaveClass('checkbox')
    expect(save).toBeDisabled()

    const prefix = brand.slug.toUpperCase()
    expect(dialog.getByRole('textbox', { name: 'Command' })).toHaveAccessibleDescription(
      `A shell command, as you’d type it in a terminal. It runs in the clone, with ${prefix}_REPO, ${prefix}_REPO_DIR, ${prefix}_DEFAULT_BRANCH set, so it never needs to quote a path.`,
    )

    await userEvent.type(dialog.getByRole('textbox', { name: 'Name' }), 'Test all')
    expect(save).toBeDisabled()
    await userEvent.type(dialog.getByRole('textbox', { name: 'Command' }), 'magex test:race')
    await userEvent.click(dialog.getByRole('radio', { name: 'Go repositories' }))
    expect(dialog.getByText('Clones with a go.mod at their root.')).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('radio', { name: 'Terminal' }))
    await userEvent.click(dialog.getByRole('checkbox', { name: 'Ask before running' }))
    await userEvent.click(dialog.getByRole('radio', { name: 'Test' }))
    await userEvent.click(save)

    const made: UserScript = {
      id: 0,
      name: 'Test all',
      command: 'magex test:race',
      icon: 'test',
      scope: 'go',
      repos: [],
      runsIn: 'terminal',
      confirm: true,
    }
    expect(platform.saveUserScript).toHaveBeenCalledExactlyOnceWith(made)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith({ ...made, id: 41 })
    })
    expect(platform.listScriptRepoNames).not.toHaveBeenCalled()
  })

  it('edits a script as saved, and saves with Enter', async () => {
    const { onSaved, dialog } = renderEditor(userScript({ runsIn: 'app', icon: 'build' }))

    expect(screen.getByRole('dialog', { name: 'Edit “Fix all”' })).toBeInTheDocument()
    expect(dialog.getByRole('textbox', { name: 'Command' })).toHaveValue('magex format:fix && magex lint')
    expect(dialog.getByRole('radio', { name: brand.name })).toBeChecked()
    expect(dialog.getByRole('radio', { name: 'Build' })).toBeChecked()

    const name = dialog.getByRole('textbox', { name: 'Name' })
    await userEvent.clear(name)
    await userEvent.type(name, 'Format{Enter}')

    expect(platform.saveUserScript).toHaveBeenCalledExactlyOnceWith(
      userScript({ name: 'Format', runsIn: 'app', icon: 'build' }),
    )
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
  })

  it('chooses repositories from the clones’ and those chosen before', async () => {
    const { onSaved, dialog } = renderEditor(userScript({ scope: 'repos', repos: ['acme-org/gone'] }))
    const list = within(await dialog.findByRole('list', { name: 'Repositories' }))

    expect(list.getAllByRole('checkbox').map((c) => c.closest('label')?.textContent)).toEqual([
      'acme-org/gone',
      'acme-org/sdk',
      'acme-org/web',
    ])
    expect(list.getByRole('checkbox', { name: 'acme-org/gone' })).toBeChecked()
    expect(list.getByRole('checkbox', { name: 'acme-org/sdk' })).toHaveClass('checkbox')
    expect(dialog.getByText('1 chosen')).toBeInTheDocument()

    await userEvent.click(list.getByRole('checkbox', { name: 'acme-org/gone' }))
    expect(dialog.getByText('Choose at least one.')).toBeInTheDocument()
    expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled()

    await userEvent.click(list.getByRole('checkbox', { name: 'acme-org/web' }))
    await userEvent.click(list.getByRole('checkbox', { name: 'acme-org/sdk' }))
    expect(dialog.getByText('2 chosen')).toBeInTheDocument()

    await userEvent.type(dialog.getByRole('searchbox', { name: 'Filter the repositories' }), 'WEB')
    expect(list.getAllByRole('checkbox')).toHaveLength(1)
    await userEvent.type(dialog.getByRole('searchbox', { name: 'Filter the repositories' }), 'x')
    expect(dialog.getByText('No repository matches.')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))

    expect(platform.saveUserScript).toHaveBeenCalledExactlyOnceWith(
      userScript({ scope: 'repos', repos: ['acme-org/web', 'acme-org/sdk'] }),
    )
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(platform.listScriptRepoNames).toHaveBeenCalledOnce()
  })

  it('reads the clones’ repositories only once Chosen is picked, and says how it went', async () => {
    let answer: (names: string[]) => void = () => undefined
    platform.listScriptRepoNames.mockReturnValueOnce(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    const { dialog } = renderEditor()

    await userEvent.click(dialog.getByRole('radio', { name: 'Chosen…' }))
    expect(dialog.getByRole('status')).toHaveTextContent('Reading your clones…')

    answer([])
    expect(await dialog.findByText('None of your clones is on GitHub.')).toBeInTheDocument()
    expect(dialog.queryByRole('list', { name: 'Repositories' })).not.toBeInTheDocument()
  })

  it('says why the clones’ repositories couldn’t be read', async () => {
    platform.listScriptRepoNames.mockRejectedValueOnce(new Error('git is missing'))
    const { dialog } = renderEditor(userScript({ scope: 'repos' }))

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Couldn’t list your clones’ repositories: git is missing',
    )
  })

  it('shows why Save refused the script, and keeps it open', async () => {
    platform.saveUserScript.mockRejectedValueOnce(
      new Error('invalid script: a hidden character', {
        cause: { message: 'The command holds a character that hides what it runs.' },
      }),
    )
    const { onSaved, dialog } = renderEditor(userScript())

    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))

    expect(await dialog.findByRole('alert')).toHaveTextContent('The command holds a character that hides what it runs.')
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('says it is saving while Save is under way', async () => {
    platform.saveUserScript.mockReturnValueOnce(new Promise(() => undefined))
    const { dialog } = renderEditor(userScript())

    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))

    expect(dialog.getByRole('button', { name: 'Saving…' })).toBeDisabled()
  })

  it('cancels with Cancel and with Escape, saving nothing', async () => {
    const { onCancel, dialog } = renderEditor(userScript())

    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    await userEvent.keyboard('{Escape}')

    expect(onCancel).toHaveBeenCalledTimes(2)
    expect(platform.saveUserScript).not.toHaveBeenCalled()
  })
})
