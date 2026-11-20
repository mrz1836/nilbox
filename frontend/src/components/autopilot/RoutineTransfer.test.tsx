import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { AutopilotMissingRef, AutopilotRoutine } from '../../platform'
import { nextTask } from '../../test/async'
import { routine, routineImportPreview } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { choicesOf, importedWords, refWords, useRoutineTransfer } from './RoutineTransfer'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// missing is a reference a bundle's routines name that this Mac lacks.
function missing(overrides: Partial<AutopilotMissingRef>): AutopilotMissingRef {
  return {
    kind: 'prompt',
    agent: '',
    name: 'Triage',
    starter: '',
    reply: false,
    routines: ['Triage bugs'],
    choices: [],
    inherits: false,
    ...overrides,
  }
}

describe('the transfer’s words', () => {
  it('names what this Mac lacks, and maps each to inherit until chosen', () => {
    expect(refWords(missing({}))).toBe('The prompt “Triage”')
    expect(refWords(missing({ reply: true, name: 'Thanks' }))).toBe('The reply “Thanks”')
    expect(refWords(missing({ kind: 'agent', agent: 'codex', name: 'codex', inherits: true }))).toBe(
      'The agent codex, which this Mac doesn’t have, so it inherits the one a click would use',
    )
    expect(refWords(missing({ kind: 'profile', agent: 'claude', name: 'work' }))).toBe('claude’s profile “work”')
    expect(refWords(missing({ kind: 'profile', agent: 'inherit', name: 'work' }))).toBe('The profile “work”')

    const preview = routineImportPreview({
      missing: [
        missing({}),
        missing({ kind: 'agent', agent: 'codex', name: 'codex', inherits: true }),
        missing({ kind: 'profile', agent: 'claude', name: 'work' }),
      ],
    })

    expect(choicesOf(preview, { 'profile:claude::work': 'personal' })).toEqual([
      { kind: 'prompt', agent: '', name: 'Triage', starter: '', to: '' },
      { kind: 'profile', agent: 'claude', name: 'work', starter: '', to: 'personal' },
    ])
  })

  it('says what an import brought in', () => {
    expect(importedWords(['A'], [])).toBe('Imported 1 routine, switched off.')
    expect(importedWords(['A', 'B'], ['Triage', 'Look'])).toBe(
      'Imported 2 routines, switched off. Added 2 prompts: Triage, Look.',
    )
  })
})

// Transfer renders the tab's Export every routine and Import, saying what each does.
function Transfer({ routines, said }: { routines: readonly AutopilotRoutine[]; said: (text: string) => void }) {
  const transfer = useRoutineTransfer(said, () => undefined)

  return (
    <>
      <button
        type="button"
        onClick={() => {
          transfer.exportRoutines(routines)
        }}
      >
        Export them
      </button>
      <button type="button" onClick={transfer.importRoutines}>
        Import some
      </button>
      {transfer.node}
    </>
  )
}

describe('useRoutineTransfer', () => {
  it('exports every routine without their prompts when asked, and does nothing when the save is cancelled', async () => {
    const user = userEvent.setup()
    const said = vi.fn()
    render(<Transfer routines={[routine(), routine({ id: 4, name: 'Second' })]} said={said} />)

    await user.click(screen.getByRole('button', { name: 'Export them' }))
    await user.click(screen.getByRole('button', { name: 'Choose where…' }))
    await waitFor(() => {
      expect(platform.chooseFileToSave).toHaveBeenCalledOnce()
    })
    expect(platform.chooseFileToSave.mock.lastCall?.[0]).toMatchObject({ filename: `${brand.slug}-routines.json` })
    expect(platform.exportRoutines).not.toHaveBeenCalled()

    platform.chooseFileToSave.mockResolvedValue('/exports/all.json')
    platform.exportRoutines.mockResolvedValue({ routines: 2, prompts: 1 })
    await user.click(screen.getByRole('button', { name: 'Export them' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Export your routines?' }))

    await user.click(dialog.getByRole('checkbox', { name: 'Include the text of the prompts they run' }))
    await user.click(dialog.getByRole('button', { name: 'Choose where…' }))

    await waitFor(() => {
      expect(platform.exportRoutines).toHaveBeenCalledExactlyOnceWith('/exports/all.json', [3, 4], false)
    })
    expect(said).toHaveBeenLastCalledWith('Exported 2 routines, with 1 prompt.')

    platform.exportRoutines.mockRejectedValue(new Error('The routines couldn’t be saved there.'))
    await user.click(screen.getByRole('button', { name: 'Export them' }))
    await user.click(screen.getByRole('button', { name: 'Choose where…' }))
    await waitFor(() => {
      expect(said).toHaveBeenLastCalledWith('The routines couldn’t be saved there.', 'error')
    })

    await user.click(screen.getByRole('button', { name: 'Export them' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('imports nothing when no file is chosen, and says why a file couldn’t be read or brought in', async () => {
    const user = userEvent.setup()
    const said = vi.fn()
    render(<Transfer routines={[]} said={said} />)

    await user.click(screen.getByRole('button', { name: 'Import some' }))
    await act(async () => {
      await Promise.resolve()
    })
    expect(platform.previewRoutineImport).not.toHaveBeenCalled()

    platform.chooseFileToOpen.mockResolvedValue('/exports/routines.json')
    platform.previewRoutineImport.mockRejectedValueOnce(new Error('The file doesn’t hold routines.'))
    await user.click(screen.getByRole('button', { name: 'Import some' }))
    await waitFor(() => {
      expect(said).toHaveBeenLastCalledWith('The file doesn’t hold routines.', 'error')
    })

    platform.importRoutines.mockRejectedValueOnce(new Error('The file changed since it was read.'))
    await user.click(screen.getByRole('button', { name: 'Import some' }))
    await user.click(await screen.findByRole('button', { name: 'Import 1 routine' }))
    await waitFor(() => {
      expect(said).toHaveBeenLastCalledWith('The file changed since it was read.', 'error')
    })

    await user.click(screen.getByRole('button', { name: 'Import some' }))
    await user.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says why the file dialog couldn’t open, and reads nothing', async () => {
    const user = userEvent.setup()
    const said = vi.fn()
    platform.chooseFileToOpen.mockRejectedValue(new Error('the dialog is already open'))
    render(<Transfer routines={[]} said={said} />)

    await user.click(screen.getByRole('button', { name: 'Import some' }))
    await waitFor(() => {
      expect(said).toHaveBeenCalledExactlyOnceWith('Couldn’t open the file dialog: the dialog is already open', 'error')
    })

    // The failure is handled: an unhandled rejection would fail the run once it surfaces.
    await nextTask()
    expect(platform.previewRoutineImport).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says why the save dialog couldn’t open, closes Export, and saves nothing', async () => {
    const user = userEvent.setup()
    const said = vi.fn()
    platform.chooseFileToSave.mockRejectedValue(new Error('the dialog is already open'))
    render(<Transfer routines={[routine()]} said={said} />)

    await user.click(screen.getByRole('button', { name: 'Export them' }))
    await user.click(screen.getByRole('button', { name: 'Choose where…' }))
    await waitFor(() => {
      expect(said).toHaveBeenCalledExactlyOnceWith('Couldn’t open the save dialog: the dialog is already open', 'error')
    })

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await nextTask()
    expect(platform.exportRoutines).not.toHaveBeenCalled()
  })
})
