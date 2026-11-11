import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { RepoScripts, RunsIn, ScriptEntry, ScriptSection, UserScript } from '../../platform'
import { repoScripts, scriptSection, userScript, yourEntry } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { RepoScriptsPage } from './RepoScriptsPage'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// renderPage renders Repo scripts as the tab shows it, runs going where runsIn says and
// the terminal being iTerm2.
function renderPage(runsIn: RunsIn = 'app') {
  const onFollow = vi.fn<(id: number | null) => void>()

  function Page() {
    const [dir, setDir] = useState('')

    return (
      <RepoScriptsPage
        dir={dir}
        onDir={setDir}
        followed={null}
        onFollow={onFollow}
        runsIn={runsIn}
        terminal="iterm2"
        cloneRoot="~/projects"
        onOpenRelease={null}
        onOpenSettings={vi.fn()}
      />
    )
  }

  render(<Page />)

  return { onFollow }
}

// yours is the Yours section holding entries, which found says the user has scripts.
function yours(entries: ScriptEntry[], overrides: Partial<ScriptSection> = {}): ScriptSection {
  return scriptSection({ source: 'yours', title: 'Yours', found: entries.length > 0, entries, ...overrides })
}

// withYours lists, for every clone, its repository's sources with the user's scripts as
// section holds them.
function withYours(section: ScriptSection, page: Partial<RepoScripts> = {}) {
  const sections = repoScripts().sections.slice(1)

  platform.listRepoScripts.mockImplementation((dir) =>
    Promise.resolve(repoScripts({ dir, sections: [section, ...sections], ...page })),
  )
}

// list is the Yours list, once it shows.
async function yourList() {
  return within(await screen.findByRole('list', { name: 'Yours' }))
}

// row is one of the user's scripts, by its name.
async function row(name: string) {
  const item = (await yourList()).getByText(name, { exact: true }).closest('li')

  if (item === null) {
    throw new Error(`no row ${name}`)
  }

  return within(item)
}

// menu opens the ⋯ menu of the script called name.
async function menu(name: string) {
  await userEvent.click((await row(name)).getByRole('button', { name: `More for ${name}` }))
}

const test = userScript({ id: 2, name: 'Test', command: 'magex test', icon: 'test', scope: 'go' })

describe('Yours', () => {
  it('shows the user’s scripts whose scope includes the clone, as the backend lists them', async () => {
    platform.listRepoScripts.mockImplementation((dir) => {
      const entries = dir === '~/projects/sdk' ? [yourEntry(), yourEntry(test)] : [yourEntry()]
      const sections = repoScripts().sections.slice(1)

      return Promise.resolve(repoScripts({ dir, sections: [yours(entries), ...sections] }))
    })
    renderPage()

    const list = await yourList()
    expect(list.getAllByRole('listitem').map((li) => li.querySelector('.font-medium')?.textContent)).toEqual([
      'Fix all',
      'Test',
    ])
    expect((await row('Test')).getByText('magex test')).toBeInTheDocument()
    expect((await row('Test')).getByText('Go repositories')).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 3 })[0]).toHaveTextContent('Yours')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), '~/projects/web')

    await waitFor(() => {
      expect(screen.getByRole('list', { name: 'Yours' })).toBeInTheDocument()
    })
    expect(
      within(screen.getByRole('list', { name: 'Yours' }))
        .getAllByRole('listitem')
        .map((li) => li.querySelector('.font-medium')?.textContent),
    ).toEqual(['Fix all'])
  })

  it('says when the user’s scripts show only in other repositories', async () => {
    withYours(yours([], { found: true }))
    renderPage()

    expect(await screen.findByText('None of your scripts show in acme-org/sdk.')).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Yours' })).not.toBeInTheDocument()
  })

  it('says why the user’s scripts couldn’t be read', async () => {
    withYours(yours([], { error: 'Couldn’t read your scripts.' }))
    renderPage()

    expect(await screen.findByText('Couldn’t read your scripts.')).toBeInTheDocument()
    expect(screen.queryByText(/Save a command/)).not.toBeInTheDocument()
  })

  it('makes a new script, then reads the page again', async () => {
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'New script' }))

    const dialog = within(screen.getByRole('dialog', { name: 'New script' }))
    await userEvent.type(dialog.getByRole('textbox', { name: 'Name' }), 'Fix all')
    await userEvent.type(dialog.getByRole('textbox', { name: 'Command' }), 'magex format:fix')
    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(platform.saveUserScript).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ id: 0, name: 'Fix all', command: 'magex format:fix', scope: 'all' }),
    )
    expect(platform.listRepoScripts.mock.calls).toEqual([
      ['~/projects/sdk', false],
      ['~/projects/sdk', false],
    ])
  })

  it('closes the editor on Cancel, saving nothing', async () => {
    renderPage()

    await userEvent.click(await screen.findByRole('button', { name: 'New script' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.saveUserScript).not.toHaveBeenCalled()
  })

  it('offers no New script while Scripts is off', async () => {
    platform.listRepoScripts.mockResolvedValue(repoScripts({ off: true, why: 'Scripts are off.', sections: [] }))
    renderPage()

    expect(await screen.findByText('Scripts are off.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New script' })).not.toBeInTheDocument()
    expect(screen.queryByText('Yours')).not.toBeInTheDocument()
  })

  it('edits a script from its menu', async () => {
    withYours(yours([yourEntry(), yourEntry(test)]))
    renderPage()

    await menu('Test')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Edit…' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Edit “Test”' }))
    expect(dialog.getByRole('radio', { name: 'Go repositories' })).toBeChecked()
    await userEvent.type(dialog.getByRole('textbox', { name: 'Command' }), ':race')
    await userEvent.click(dialog.getByRole('button', { name: 'Save' }))

    expect(platform.saveUserScript).toHaveBeenCalledExactlyOnceWith({ ...test, command: 'magex test:race' })
    await waitFor(() => {
      expect(platform.listRepoScripts).toHaveBeenCalledTimes(2)
    })
  })

  it('duplicates and moves a script among those shown here, and reads the page again each time', async () => {
    withYours(yours([yourEntry(), yourEntry(test)]))
    renderPage()

    await menu('Fix all')
    expect(screen.getByRole('menuitem', { name: 'Move up' })).toBeDisabled()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Move down' }))
    await waitFor(() => {
      expect(platform.moveUserScript).toHaveBeenCalledExactlyOnceWith(1, 'down', '~/projects/sdk')
    })

    await menu('Test')
    expect(screen.getByRole('menuitem', { name: 'Move down' })).toBeDisabled()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Move up' }))
    await waitFor(() => {
      expect(platform.moveUserScript).toHaveBeenLastCalledWith(2, 'up', '~/projects/sdk')
    })

    await menu('Test')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Duplicate' }))
    await waitFor(() => {
      expect(platform.duplicateUserScript).toHaveBeenCalledExactlyOnceWith(2)
    })

    await waitFor(() => {
      expect(platform.listRepoScripts).toHaveBeenCalledTimes(4)
    })
    expect(platform.listRepoScripts.mock.calls.every(([, refresh]) => !refresh)).toBe(true)
  })

  it('deletes a script once the user confirms', async () => {
    withYours(yours([yourEntry()]))
    renderPage()

    await menu('Fix all')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete…' }))

    let dialog = within(screen.getByRole('dialog', { name: 'Delete “Fix all”?' }))
    expect(dialog.getByText('It no longer shows in any repository. Its runs stay in Runs.')).toBeInTheDocument()
    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    expect(platform.deleteUserScript).not.toHaveBeenCalled()

    await menu('Fix all')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete…' }))
    dialog = within(screen.getByRole('dialog', { name: 'Delete “Fix all”?' }))
    await userEvent.click(dialog.getByRole('button', { name: 'Delete' }))

    expect(platform.deleteUserScript).toHaveBeenCalledExactlyOnceWith(1)
    await waitFor(() => {
      expect(platform.listRepoScripts).toHaveBeenCalledTimes(2)
    })
  })

  it('reads the page again after each of two moves that end one after another', async () => {
    const answers: (() => void)[] = []
    platform.moveUserScript.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          answers.push(resolve)
        }),
    )
    withYours(yours([yourEntry(), yourEntry(test)]))
    renderPage()

    await menu('Fix all')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Move down' }))
    await menu('Test')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Move up' }))
    expect(answers).toHaveLength(2)

    answers[0]?.()
    await waitFor(() => {
      expect(platform.listRepoScripts).toHaveBeenCalledTimes(2)
    })

    answers[1]?.()
    await waitFor(() => {
      expect(platform.listRepoScripts).toHaveBeenCalledTimes(3)
    })
  })

  it('says the repository has nothing of its own, beside the user’s scripts', async () => {
    withYours(yours([yourEntry()]), {
      sections: [
        yours([yourEntry()]),
        scriptSection({ off: true }),
        scriptSection({ source: 'tasks', title: 'From .vscode/tasks.json', off: true }),
      ],
    })
    renderPage()

    await yourList()
    expect(
      screen.getByText('Commands from t3.json and VS Code tasks are turned off in Settings → Integrations.'),
    ).toBeInTheDocument()

    withYours(yours([yourEntry()]), { sections: [yours([yourEntry()])] })
    await userEvent.click(screen.getByRole('button', { name: /Read again/ }))

    expect(await screen.findByText('acme-org/sdk has no commands of its own to show.')).toBeInTheDocument()
    expect(screen.queryByText(/has no scripts to show/)).not.toBeInTheDocument()
  })

  it('says why a change didn’t take', async () => {
    withYours(yours([yourEntry()]))
    platform.duplicateUserScript.mockRejectedValueOnce(
      new Error('gone', { cause: { message: 'The script was deleted.' } }),
    )
    renderPage()

    await menu('Fix all')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Duplicate' }))

    expect(await screen.findByText('Couldn’t change your scripts: The script was deleted.')).toBeInTheDocument()
    expect(platform.listRepoScripts).toHaveBeenCalledOnce()
  })

  it('asks before running a script set to ask, then runs it where it says', async () => {
    const asking = userScript({ confirm: true, runsIn: 'terminal' })
    withYours(yours([yourEntry(asking)]))
    const { onFollow } = renderPage()

    expect(
      (await row('Fix all')).getByText('All repositories · Runs in iTerm2 · Asks before running'),
    ).toBeInTheDocument()

    await userEvent.click((await row('Fix all')).getByRole('button', { name: 'Run Fix all' }))

    let dialog = within(screen.getByRole('dialog', { name: 'Run “Fix all”?' }))
    expect(dialog.getByText('magex format:fix && magex lint')).toBeInTheDocument()
    expect(
      dialog.getByText((_, el) => el?.tagName === 'P' && el.textContent.startsWith('It runs in')),
    ).toHaveTextContent('It runs in ~/projects/sdk, in iTerm2.')
    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    expect(platform.runScript).not.toHaveBeenCalled()

    await menu('Fix all')
    await userEvent.click(screen.getByRole('menuitem', { name: `Run in ${brand.name}` }))

    dialog = within(screen.getByRole('dialog', { name: 'Run “Fix all”?' }))
    expect(
      dialog.getByText((_, el) => el?.tagName === 'P' && el.textContent.startsWith('It runs in')),
    ).toHaveTextContent(`It runs in ~/projects/sdk, in ${brand.name}.`)
    await userEvent.click(dialog.getByRole('button', { name: 'Run' }))

    expect(platform.runScript).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ dir: '~/projects/sdk', ref: 'yours:1', runsIn: 'app', confirmed: '', anyway: false }),
    )
    await waitFor(() => {
      expect(onFollow).toHaveBeenCalledWith(31)
    })
  })

  it('asks before running anyway a script that bumps a version', async () => {
    const bump = userScript({ name: 'Bump', command: 'magex version:bump', confirm: true })
    withYours(yours([yourEntry(bump, { release: true })]))
    renderPage('terminal')

    await menu('Bump')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Run it anyway' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Run “Bump”?' }))
    expect(
      dialog.getByText((_, el) => el?.tagName === 'P' && el.textContent.startsWith('It runs in')),
    ).toHaveTextContent('It runs in ~/projects/sdk, in iTerm2.')
    await userEvent.click(dialog.getByRole('button', { name: 'Run it anyway' }))

    expect(platform.runScript).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ ref: 'yours:1', runsIn: '', anyway: true }),
    )
  })

  it('runs a script that doesn’t ask at once, where it says', async () => {
    withYours(yours([yourEntry(userScript({ runsIn: 'app' }))]))
    renderPage('terminal')

    expect((await row('Fix all')).getByText(`All repositories · Runs in ${brand.name}`)).toBeInTheDocument()

    await menu('Fix all')
    expect(screen.getByRole('menuitem', { name: 'Run in iTerm2' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Run in iTerm2' }))

    expect(platform.runScript).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ ref: 'yours:1', runsIn: 'terminal' }),
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('keeps the menu of a script that can’t run, to edit or delete it', async () => {
    const broken: UserScript = userScript({ command: 'magex lint\nrm -rf ~' })
    withYours(yours([yourEntry(broken, { runnable: false, why: 'Holds a character that hides what it runs.' })]))
    renderPage()

    const fix = await row('Fix all')
    expect(fix.queryByRole('button', { name: 'Run Fix all' })).not.toBeInTheDocument()
    expect(fix.getByText('Holds a character that hides what it runs.')).toBeInTheDocument()

    await menu('Fix all')
    expect(screen.getAllByRole('menuitem').map((m) => m.textContent)).toEqual([
      'Edit…',
      'Duplicate',
      'Move up',
      'Move down',
      'Delete…',
    ])
  })
})
