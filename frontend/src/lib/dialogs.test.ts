import { describe, expect, it, vi } from 'vitest'

import type { FileChoice } from '../platform'
import { platform } from '../test/platform'
import { pickFileToOpen, pickFileToSave } from './dialogs'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const backup: FileChoice = {
  title: 'Restore from a backup',
  button: 'Open',
  filters: [{ name: 'Backups', pattern: '*.json' }],
}

describe('pickFileToOpen', () => {
  it('asks for the file the choice describes, and resolves with its path', async () => {
    platform.chooseFileToOpen.mockResolvedValue('~/backups/example.json')

    expect(await pickFileToOpen(backup)).toEqual({ ok: true, data: '~/backups/example.json' })
    expect(platform.chooseFileToOpen).toHaveBeenCalledExactlyOnceWith(backup)
  })

  it('resolves with nothing chosen when the user cancels', async () => {
    expect(await pickFileToOpen(backup)).toEqual({ ok: true, data: '' })
  })

  it('says why the file dialog couldn’t open, in the backend’s words when it has some', async () => {
    platform.chooseFileToOpen.mockRejectedValueOnce(new Error('no window'))
    platform.chooseFileToOpen.mockRejectedValueOnce(
      new Error('dialog: no window', { cause: { message: 'The window is closing.' } }),
    )

    expect(await pickFileToOpen(backup)).toEqual({ ok: false, error: 'Couldn’t open the file dialog: no window' })
    expect(await pickFileToOpen(backup)).toEqual({
      ok: false,
      error: 'Couldn’t open the file dialog: The window is closing.',
    })
  })

  it('fails as a rejection does when the dialog throws as it opens', async () => {
    platform.chooseFileToOpen.mockImplementation(() => {
      throw new Error('no runtime')
    })

    expect(await pickFileToOpen(backup)).toEqual({ ok: false, error: 'Couldn’t open the file dialog: no runtime' })
  })

  it('words the failure as the caller asks', async () => {
    platform.chooseFileToOpen.mockRejectedValue(new Error('no window'))

    expect(await pickFileToOpen(backup, (why) => why)).toEqual({ ok: false, error: 'no window' })
  })
})

describe('pickFileToSave', () => {
  const save: FileChoice = { ...backup, title: 'Back up your setup', button: 'Save', filename: 'backup.json' }

  it('asks where to save as the choice describes, and resolves with the path', async () => {
    platform.chooseFileToSave.mockResolvedValue('~/backups/backup.json')

    expect(await pickFileToSave(save)).toEqual({ ok: true, data: '~/backups/backup.json' })
    expect(platform.chooseFileToSave).toHaveBeenCalledExactlyOnceWith(save)
  })

  it('resolves with nothing chosen when the user cancels', async () => {
    expect(await pickFileToSave(save)).toEqual({ ok: true, data: '' })
  })

  it('says why the save dialog couldn’t open, or as the caller words it', async () => {
    platform.chooseFileToSave.mockRejectedValue(new Error('no window'))

    expect(await pickFileToSave(save)).toEqual({ ok: false, error: 'Couldn’t open the save dialog: no window' })
    expect(await pickFileToSave(save, (why) => `Not saved: ${why}`)).toEqual({
      ok: false,
      error: 'Not saved: no window',
    })
  })
})
