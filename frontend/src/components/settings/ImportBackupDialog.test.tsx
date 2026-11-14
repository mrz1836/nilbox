import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { backupSections } from '../../lib/backup'
import { backupImported, backupPlan, backupPreview, backupSectionPreview } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ImportBackupDialog } from './ImportBackupDialog'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const path = '/backups/setup.json'

// renderDialog renders the dialog over the backup at path, returning its callbacks.
function renderDialog() {
  const onImported = vi.fn()
  const onCancel = vi.fn()

  render(<ImportBackupDialog path={path} onImported={onImported} onCancel={onCancel} />)

  return { onImported, onCancel }
}

// categories returns the dialog's rows, one per category in order, once the backup is
// read.
async function categories() {
  const list = within(await screen.findByRole('list', { name: 'Categories' }))

  return backupSections.map(({ label }) => list.getByRole('group', { name: label }))
}

// row returns the row of a category by its name.
async function row(name: string) {
  await screen.findByRole('list', { name: 'Categories' })

  return within(screen.getByRole('group', { name }))
}

describe('ImportBackupDialog', () => {
  it('shows what importing each category the backup holds would do, merging by default', async () => {
    renderDialog()

    expect(screen.getByRole('heading', { name: 'Restore from “setup.json”' })).toBeInTheDocument()
    expect(await screen.findByText(/^Made by \S+ v1\.2\.3 on /)).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Merge' })).toBeChecked()
    expect(
      screen.getByText('Adds what the backup holds and changes what it names. Nothing here is deleted.'),
    ).toBeVisible()

    const rows = await categories()
    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining('Settings1 to change · 20 already here'),
      expect.stringContaining('RepositoriesNothing to import'),
      expect.stringContaining('Prompts and replies1 to add · 8 already here'),
      expect.stringContaining('Launch defaultsNothing to import'),
      expect.stringContaining('Your scriptsNothing to import'),
    ])

    const settings = await row('Settings')
    expect(settings.getByRole('checkbox')).toBeChecked()
    // The items are folded until opened.
    await userEvent.click(settings.getByText('1 item'))
    const items = settings.getByRole('list', { name: 'Settings items' })
    expect(items).toHaveTextContent('Themedark (now system)Changes')

    expect(platform.previewBackup).toHaveBeenCalledWith(path)
    expect(platform.importBackup).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: 'Import 5 categories' })).toBeEnabled()
  })

  it('imports the categories left chosen', async () => {
    const { onImported } = renderDialog()

    await userEvent.click((await row('Repositories')).getByRole('checkbox'))
    await userEvent.click((await row('Your scripts')).getByRole('checkbox'))
    await userEvent.click((await row('Settings')).getByRole('checkbox'))
    expect(screen.getByRole('button', { name: 'Import 2 categories' })).toBeEnabled()

    // A category left out can be put back.
    await userEvent.click((await row('Settings')).getByRole('checkbox'))
    await userEvent.click(screen.getByRole('button', { name: 'Import 3 categories' }))

    expect(platform.importBackup).toHaveBeenCalledWith({
      path,
      hash: 'c0ffee',
      sections: ['settings', 'prompts', 'launchDefaults'],
      mode: platform.BackupMode.ModeMerge,
    })
    expect(onImported).toHaveBeenCalledWith(backupImported(['settings', 'prompts', 'launchDefaults']))
  })

  it('offers only the categories the backup holds and can read, with their notes', async () => {
    platform.previewBackup.mockResolvedValue(
      backupPreview({
        sections: [
          backupSectionPreview('settings', {
            merge: backupPlan({ notes: ['Claude Code profile was left as it is. Unknown profile "work".'] }),
          }),
          backupSectionPreview('repositories', { present: false }),
          backupSectionPreview('prompts', { error: 'The file isn’t a prompts file.' }),
          backupSectionPreview('launchDefaults', { present: false }),
          backupSectionPreview('scripts', { present: false }),
        ],
      }),
    )
    renderDialog()

    const settings = await row('Settings')
    expect(settings.getByText('Claude Code profile was left as it is. Unknown profile "work".')).toBeInTheDocument()

    const repos = await row('Repositories')
    expect(repos.getByText('Not in this backup')).toBeInTheDocument()
    expect(repos.getByRole('checkbox')).toBeDisabled()
    expect(repos.getByRole('checkbox')).not.toBeChecked()

    const prompts = await row('Prompts and replies')
    expect(prompts.getByText('Can’t be imported: The file isn’t a prompts file.')).toBeInTheDocument()
    expect(prompts.getByRole('checkbox')).toBeDisabled()

    await userEvent.click(settings.getByRole('checkbox'))
    expect(screen.getByRole('button', { name: 'Import 0 categories' })).toBeDisabled()
  })

  it('asks again before replacing, naming everything it would delete', async () => {
    const { onImported } = renderDialog()

    await userEvent.click(await screen.findByRole('radio', { name: 'Replace what’s here' }))

    expect(
      screen.getByText(
        'Makes each category you choose exactly the backup’s. What’s here and not in the backup is deleted.',
      ),
    ).toBeInTheDocument()
    expect((await row('Prompts and replies')).getByText('1 to add, 1 to delete · 8 already here')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Import 5 categories' }))

    expect(platform.importBackup).not.toHaveBeenCalled()
    expect(screen.getByRole('heading', { name: 'Replace what’s here?' })).toBeInTheDocument()
    const deleted = within(screen.getByRole('list', { name: 'To delete' }))
    expect(deleted.getByText('Prompts and replies: 1 item deleted')).toBeInTheDocument()
    expect(deleted.getByRole('list', { name: 'Prompts and replies to delete' })).toHaveTextContent('Old review')
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()

    // Cancel goes back, with the choices as they were.
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.getByRole('radio', { name: 'Replace what’s here' })).toBeChecked()

    await userEvent.click(screen.getByRole('button', { name: 'Import 5 categories' }))
    await userEvent.click(screen.getByRole('button', { name: 'Replace' }))

    expect(platform.importBackup).toHaveBeenCalledWith({
      path,
      hash: 'c0ffee',
      sections: ['settings', 'repositories', 'prompts', 'launchDefaults', 'scripts'],
      mode: platform.BackupMode.ModeReplace,
    })
    expect(onImported).toHaveBeenCalled()
  })

  it('says when replacing deletes nothing', async () => {
    renderDialog()

    await userEvent.click(await screen.findByRole('radio', { name: 'Replace what’s here' }))
    await userEvent.click((await row('Prompts and replies')).getByRole('checkbox'))
    await userEvent.click(screen.getByRole('button', { name: 'Import 4 categories' }))

    expect(screen.getByText(/Nothing here is deleted, but what the backup holds takes the place/)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'To delete' })).not.toBeInTheDocument()
  })

  it('says why a backup could not be read', async () => {
    platform.previewBackup.mockRejectedValue(new Error('The file isn’t a backup.'))
    renderDialog()

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read the backup: The file isn’t a backup.')
    expect(screen.getByRole('button', { name: 'Import 0 categories' })).toBeDisabled()
  })

  it('keeps the dialog open and says why an import failed', async () => {
    platform.importBackup.mockRejectedValue(new Error('The file changed since you opened it.'))
    const { onImported, onCancel } = renderDialog()

    await userEvent.click(await screen.findByRole('button', { name: 'Import 5 categories' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The file changed since you opened it.')
    expect(onImported).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalled()
  })
})
