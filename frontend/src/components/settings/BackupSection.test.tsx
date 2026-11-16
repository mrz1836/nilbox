import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { BackupSection } from './BackupSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// renderSection renders the section showing the System theme, returning its callbacks.
function renderSection() {
  const onSaved = vi.fn()
  const onLaunchChanged = vi.fn(() => Promise.resolve())
  const onThemeChange = vi.fn(() => Promise.resolve({ ok: true as const, data: undefined }))

  render(
    <BackupSection onSaved={onSaved} onLaunchChanged={onLaunchChanged} theme="system" onThemeChange={onThemeChange} />,
  )

  return { onSaved, onLaunchChanged, onThemeChange }
}

describe('BackupSection', () => {
  it('backs up, restores, and says what a backup never holds', () => {
    renderSection()

    expect(screen.getByRole('heading', { name: 'Back up your setup' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Restore from a backup' })).toBeInTheDocument()

    const never = within(screen.getByRole('list', { name: 'What a backup never holds' }))
    expect(never.getAllByRole('listitem')).toHaveLength(8)
    expect(never.getByText(`Your GitHub sign-in, which ${brand.name} never stores.`)).toBeInTheDocument()
    expect(never.getByText(/Repository scripts you trusted/)).toBeInTheDocument()
  })

  it('exports the categories left ticked to the file the user chooses', async () => {
    platform.chooseFileToSave.mockResolvedValue('/backups/setup.json')
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Export…' }))

    const boxes = within(screen.getByRole('group', { name: /Choose what to include/ })).getAllByRole('checkbox')
    expect(boxes).toHaveLength(5)
    expect(boxes.every((b) => (b as HTMLInputElement).checked)).toBe(true)

    await userEvent.click(screen.getByRole('checkbox', { name: /^Your scripts/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Choose where to save…' }))

    const asked = platform.chooseFileToSave.mock.lastCall?.[0]
    expect(asked?.filename).toMatch(new RegExp(`^${brand.name} backup \\d{4}-\\d{2}-\\d{2}\\.json$`))
    expect(asked?.filters).toEqual([{ name: `${brand.name} backup`, pattern: '*.json' }])
    expect(platform.exportBackup).toHaveBeenCalledWith('/backups/setup.json', [
      'settings',
      'repositories',
      'prompts',
      'launchDefaults',
    ])
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Backed up to “setup.json”: Settings (3), Repositories (3), Prompts and replies (3), Launch defaults (3).',
    )
  })

  it('exports nothing until a category is ticked, or when the save dialog is cancelled', async () => {
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Export…' }))
    for (const box of screen.getAllByRole('checkbox')) {
      await userEvent.click(box)
    }

    expect(screen.getByRole('button', { name: 'Choose where to save…' })).toBeDisabled()

    await userEvent.click(screen.getByRole('checkbox', { name: /^Settings/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Choose where to save…' }))

    await waitFor(() => {
      expect(platform.chooseFileToSave).toHaveBeenCalled()
    })
    expect(platform.exportBackup).not.toHaveBeenCalled()
  })

  it('says when the save dialog cannot open, and closes the choice on Cancel', async () => {
    platform.chooseFileToSave.mockRejectedValue(new Error('no window'))
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Export…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Export…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Choose where to save…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t open the save dialog: no window')
    expect(platform.exportBackup).not.toHaveBeenCalled()
  })

  it('says why an export failed', async () => {
    platform.chooseFileToSave.mockResolvedValue('/Volumes/gone/setup.json')
    platform.exportBackup.mockRejectedValue(new Error('the disk is gone'))
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Export…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Choose where to save…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the disk is gone')
  })

  it('imports a backup, reads the setup again, and names the copy saved first', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/backups/setup.json')
    platform.getSettings.mockResolvedValue(settings({ theme: 'dark' }))
    platform.importBackup.mockResolvedValue({
      savedTo: '~/backups/before import.json',
      sections: [
        { section: 'settings', count: { add: 0, update: 1, same: 20, remove: 0 }, notes: ['Theme was set.'] },
        { section: 'prompts', count: { add: 2, update: 0, same: 8, remove: 1 }, notes: [] },
      ],
    })
    const { onSaved, onLaunchChanged, onThemeChange } = renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Import…' }))

    expect(platform.chooseFileToOpen.mock.lastCall?.[0].filters).toEqual([
      { name: `${brand.name} backup`, pattern: '*.json' },
    ])

    await userEvent.click(await screen.findByRole('button', { name: 'Import 5 categories' }))

    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent('Imported Settings (1 changed), Prompts and replies (2 added, 1 deleted).')
    expect(status).toHaveTextContent('Your setup as it was before is saved as ~/backups/before import.json.')
    expect(status).toHaveTextContent('Theme was set.')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(onSaved).toHaveBeenCalledWith(settings({ theme: 'dark' }))
    expect(onThemeChange).toHaveBeenCalledWith('dark')
    expect(onLaunchChanged).toHaveBeenCalled()
  })

  it('leaves the theme as it is when the import keeps it', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/backups/setup.json')
    const { onSaved, onThemeChange } = renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Import…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Import 5 categories' }))

    await screen.findByRole('status')
    expect(onSaved).toHaveBeenCalledWith(settings())
    expect(onThemeChange).not.toHaveBeenCalled()
  })

  it('opens nothing when the file dialog is cancelled, and says when it cannot open', async () => {
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Import…' }))
    await waitFor(() => {
      expect(platform.chooseFileToOpen).toHaveBeenCalled()
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.previewBackup).not.toHaveBeenCalled()

    platform.chooseFileToOpen.mockRejectedValue(new Error('no window'))
    await userEvent.click(screen.getByRole('button', { name: 'Import…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t open the file dialog: no window')
  })

  it('closes the import dialog on Cancel, importing nothing', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/backups/setup.json')
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Import…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.importBackup).not.toHaveBeenCalled()
  })
})
