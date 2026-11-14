import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { agents, bundlePrompt, defaultOffer, importEntry, importPreview, promptBackup } from '../test/fixtures'
import { platform } from '../test/platform'
import { ImportPromptsDialog, type ImportStart } from './ImportPromptsDialog'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const file: ImportStart = { kind: 'file', path: '/exports/shared prompts.json' }

// entries are what a file holds: a new prompt, one renamed, a starter edit, and one the
// library already has.
function entries() {
  return [
    importEntry({
      prompt: bundlePrompt({
        models: { codex: 'gpt-6-sol', claude: 'opus' },
        effort: 'high',
        planMode: true,
        extraArgs: { claude: ['--verbose'], codex: null },
      }),
    }),
    importEntry({
      prompt: bundlePrompt({
        nickname: 'Work on it',
        name: 'Work on an issue my way',
        appliesTo: 'issue',
        models: { claude: '' },
      }),
      outcome: 'rename',
      nickname: 'Work on it 2',
      notes: ['Its text has problems to fix before it can launch.'],
    }),
    importEntry({
      prompt: bundlePrompt({ nickname: 'Review (mine)', name: 'Review a pull request', appliesTo: 'pr' }),
      outcome: 'replace',
      nickname: 'Review (mine)',
      replaces: 'Review',
    }),
    importEntry({
      prompt: bundlePrompt({ nickname: 'Quick look', worktree: false }),
      outcome: 'skip',
      nickname: 'Quick look',
    }),
  ]
}

// renderDialog renders the dialog starting at start, returning its callbacks.
function renderDialog(start: ImportStart) {
  const onImported = vi.fn()
  const onCancel = vi.fn()

  render(<ImportPromptsDialog start={start} agents={agents()} onImported={onImported} onCancel={onCancel} />)

  return { onImported, onCancel }
}

describe('ImportPromptsDialog', () => {
  it('shows what importing each prompt in a file would do', async () => {
    platform.previewPromptImport.mockResolvedValue(importPreview({ entries: entries() }))
    renderDialog(file)

    expect(screen.getByRole('heading', { name: 'Import prompts from “shared prompts.json”' })).toBeInTheDocument()

    const list = within(await screen.findByRole('list', { name: 'Prompts' }))
    const rows = list.getAllByRole('listitem')
    expect(rows).toHaveLength(4)

    expect(rows[0]).toHaveTextContent('New')
    // Each agent's settings are named in the agents' order, by the agent's own labels.
    expect(rows[0]).toHaveTextContent(
      'For pull requests and issues · Claude: Opus · Codex: GPT-6-Sol · high effort · plan mode',
    )
    expect(rows[0]).toHaveTextContent(/Adds flags: Claude: --verbose$/)
    expect(rows[1]).toHaveTextContent('New · saved as “Work on it 2”')
    // A model left empty inherits, so it is not listed.
    expect(rows[1]).not.toHaveTextContent('Claude:')
    expect(rows[1]).toHaveTextContent('Its text has problems to fix before it can launch.')
    expect(rows[2]).toHaveTextContent('Updates the starter “Review”')
    expect(rows[3]).toHaveTextContent('Already in your library')
    expect(rows[3]).toHaveTextContent('not in a worktree')

    const boxes = list.getAllByRole('checkbox')
    expect(boxes.map((b) => (b as HTMLInputElement).checked)).toEqual([true, true, true, false])
    expect(boxes[3]).toBeDisabled()

    expect(platform.previewPromptImport).toHaveBeenCalledWith({ kind: 'file', path: file.path })
    expect(platform.importPrompts).not.toHaveBeenCalled()
  })

  it('imports the prompts left chosen', async () => {
    const all = entries()
    platform.previewPromptImport.mockResolvedValue(importPreview({ entries: all }))
    platform.importPrompts.mockResolvedValue({ added: 1, replaced: 1, skipped: 0, defaults: 0, first: 4 })
    const { onImported } = renderDialog(file)

    const list = within(await screen.findByRole('list', { name: 'Prompts' }))
    await userEvent.click(list.getAllByRole('checkbox')[1] as HTMLElement)
    await userEvent.click(screen.getByRole('button', { name: 'Import 2 prompts' }))

    expect(platform.importPrompts).toHaveBeenCalledWith([all[0]?.prompt, all[2]?.prompt], {
      pr: '',
      issue: '',
      replies: {},
    })
    expect(onImported).toHaveBeenCalledWith({ added: 1, replaced: 1, skipped: 0, defaults: 0, first: 4 })
  })

  it('has nothing to import when the library holds everything', async () => {
    platform.previewPromptImport.mockResolvedValue(importPreview({ entries: [importEntry({ outcome: 'skip' })] }))
    renderDialog(file)

    expect(await screen.findByText('Everything in it is already in your library.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Import 0 prompts' })).toBeDisabled()
  })

  it('says why a file could not be read', async () => {
    platform.previewPromptImport.mockRejectedValue(new Error(`the file isn't a ${brand.name} prompts file`))
    renderDialog(file)

    expect(await screen.findByRole('alert')).toHaveTextContent(
      `Couldn’t read the prompts: the file isn't a ${brand.name} prompts file`,
    )
  })

  it('keeps the dialog open and says why an import failed', async () => {
    platform.previewPromptImport.mockResolvedValue(importPreview({ entries: [importEntry()] }))
    platform.importPrompts.mockRejectedValue(new Error('the disk is full'))
    const { onImported } = renderDialog(file)

    await userEvent.click(await screen.findByRole('button', { name: 'Import 1 prompt' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the disk is full')
    expect(onImported).not.toHaveBeenCalled()
  })

  it('restores prompts from the backup the user picks', async () => {
    platform.listPromptBackups.mockResolvedValue([
      promptBackup({ path: '/data/prompts.json', kind: 'library', at: '2026-11-08T18:00:00Z', prompts: 4, new: 0 }),
      promptBackup(),
      promptBackup({ path: '/data/app.db.backup-x', kind: 'update', error: 'the file is damaged', prompts: 0, new: 0 }),
    ])
    platform.previewPromptImport.mockResolvedValue(importPreview({ entries: [importEntry()] }))
    const { onImported } = renderDialog({ kind: 'backups' })

    expect(screen.getByRole('heading', { name: 'Restore prompts from a backup' })).toBeInTheDocument()

    const radios = await screen.findAllByRole('radio')
    expect(radios).toHaveLength(3)
    expect(radios[1]).toBeChecked()
    expect(radios[2]).toBeDisabled()

    const rows = screen.getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('Saved copy of your prompts')
    expect(rows[0]).toHaveTextContent('4 prompts, all already in your library')
    expect(rows[1]).toHaveTextContent('Data set aside')
    expect(rows[1]).toHaveTextContent('4 prompts, 3 not in your library')
    expect(rows[2]).toHaveTextContent('Couldn’t be read: the file is damaged')

    await userEvent.click(screen.getByRole('button', { name: 'Show prompts' }))

    expect(platform.previewPromptImport).toHaveBeenCalledWith({ kind: 'backup', path: promptBackup().path })
    expect(await screen.findByRole('heading', { name: 'Restore prompts from the backup' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Restore 1 prompt' }))
    expect(onImported).toHaveBeenCalled()
  })

  it('picks another backup', async () => {
    platform.listPromptBackups.mockResolvedValue([
      promptBackup(),
      promptBackup({ path: '/data/app.db.backup-y', kind: 'update' }),
    ])
    renderDialog({ kind: 'backups' })

    const radios = await screen.findAllByRole('radio')
    await userEvent.click(radios[1] as HTMLElement)
    await userEvent.click(screen.getByRole('button', { name: 'Show prompts' }))

    expect(platform.previewPromptImport).toHaveBeenCalledWith({ kind: 'backup', path: '/data/app.db.backup-y' })
  })

  it('says when there are no backups yet', async () => {
    platform.listPromptBackups.mockResolvedValue([])
    const { onCancel } = renderDialog({ kind: 'backups' })

    expect(await screen.findByText(`${brand.name} hasn’t kept any backups of your prompts yet.`)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show prompts' })).toBeDisabled()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalled()
  })

  it('says why the backups could not be listed', async () => {
    platform.listPromptBackups.mockRejectedValue(new Error('the folder is gone'))
    renderDialog({ kind: 'backups' })

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t list the backups: the folder is gone')
  })

  it('puts a prompt back after leaving it out', async () => {
    platform.previewPromptImport.mockResolvedValue(
      importPreview({ entries: [importEntry(), importEntry({ nickname: 'Other' })] }),
    )
    renderDialog(file)

    const box = (await screen.findAllByRole('checkbox'))[0] as HTMLElement
    await userEvent.click(box)
    expect(screen.getByRole('button', { name: 'Import 1 prompt' })).toBeEnabled()

    await userEvent.click(box)
    expect(screen.getByRole('button', { name: 'Import 2 prompts' })).toBeEnabled()
  })

  it('says when a file holds no prompts', async () => {
    platform.previewPromptImport.mockResolvedValue(importPreview({ entries: [] }))
    renderDialog(file)

    expect(await screen.findByText('There are no prompts in it.')).toBeInTheDocument()
  })

  it('names the prompt that already holds an entry, and every setting it turns off', async () => {
    platform.previewPromptImport.mockResolvedValue(
      importPreview({
        entries: [
          importEntry({
            prompt: bundlePrompt({ nickname: 'Review (old)', planMode: false, worktree: true }),
            outcome: 'skip',
            nickname: 'Review',
          }),
        ],
      }),
    )
    renderDialog(file)

    const row = (await screen.findAllByRole('listitem'))[0]
    expect(row).toHaveTextContent('Already in your library as “Review”')
    expect(row).toHaveTextContent('no plan mode · in a worktree')
  })

  it('offers no backup that holds no prompts', async () => {
    platform.listPromptBackups.mockResolvedValue([promptBackup({ prompts: 0, new: 0 })])
    renderDialog({ kind: 'backups' })

    expect(await screen.findByText('Holds no prompts.')).toBeInTheDocument()
    expect(screen.getByRole('radio')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Show prompts' })).toBeDisabled()
  })

  it('offers the file’s defaults, each going with its prompt', async () => {
    const all = entries()
    platform.previewPromptImport.mockResolvedValue(
      importPreview({
        entries: all,
        defaults: [
          defaultOffer({ slot: 'pr', nickname: 'Review (mine)', current: 'Review', entry: 2 }),
          defaultOffer({ slot: 'approve', nickname: 'Security', current: 'LGTM', entry: 0 }),
          defaultOffer({ slot: 'comment', nickname: 'Quick look', current: '', entry: 3 }),
        ],
      }),
    )
    platform.importPrompts.mockResolvedValue({ added: 2, replaced: 1, skipped: 1, defaults: 2, first: 4 })
    const { onImported } = renderDialog(file)

    const group = within(await screen.findByRole('group', { name: 'Defaults' }))
    const boxes = group.getAllByRole('checkbox')
    expect(boxes.map((b) => b.closest('label')?.textContent)).toEqual([
      'Make “Review (mine)” the default prompt for pull requests (now “Review”)',
      'Make “Security” the default reply for approvals (now “LGTM”)',
      'Make “Quick look” the default reply for comments (now none)',
    ])
    expect(boxes.map((b) => (b as HTMLInputElement).checked)).toEqual([true, true, true])
    expect(screen.getByRole('button', { name: 'Import 3 prompts and 3 defaults' })).toBeEnabled()

    // Leaving a prompt out holds its default back with it.
    const prompts = within(screen.getByRole('list', { name: 'Prompts' }))
    await userEvent.click(prompts.getAllByRole('checkbox')[0] as HTMLElement)
    expect(boxes[1]).toBeDisabled()
    expect(boxes[1]).not.toBeChecked()

    // And leaving a default out keeps the one here.
    await userEvent.click(boxes[0] as HTMLElement)
    await userEvent.click(screen.getByRole('button', { name: 'Import 2 prompts and 1 default' }))

    // A prompt the library holds is sent with the default it brings, and skipped again.
    expect(platform.importPrompts).toHaveBeenCalledWith([all[1]?.prompt, all[2]?.prompt, all[3]?.prompt], {
      pr: '',
      issue: '',
      replies: { comment: 'Quick look' },
    })
    expect(onImported).toHaveBeenCalledWith({ added: 2, replaced: 1, skipped: 1, defaults: 2, first: 4 })
  })

  it('imports a default alone', async () => {
    const held = [importEntry({ outcome: 'skip', nickname: 'Security' })]
    platform.previewPromptImport.mockResolvedValue(importPreview({ entries: held, defaults: [defaultOffer()] }))
    renderDialog(file)

    await userEvent.click(await screen.findByRole('button', { name: 'Import 1 default' }))

    expect(platform.importPrompts).toHaveBeenCalledWith([held[0]?.prompt], {
      pr: '',
      issue: '',
      replies: { approve: 'Security' },
    })
  })

  it('shows no defaults when the file offers none', async () => {
    platform.previewPromptImport.mockResolvedValue(importPreview())
    renderDialog(file)

    expect(await screen.findByRole('button', { name: 'Import 1 prompt' })).toBeEnabled()
    expect(screen.queryByRole('group', { name: 'Defaults' })).not.toBeInTheDocument()
  })
})
