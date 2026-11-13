import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { Prompt, PromptCheckRequest } from '../platform'
import {
  agents,
  checkResult,
  defaultPrompts,
  importEntry,
  item,
  prompt,
  prompts,
  replyPrompt,
  replyPrompts,
  variables,
} from '../test/fixtures'
import { platform } from '../test/platform'
import { PromptsView } from './PromptsView'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// next is the library the next reload brings in, and nextReplies its replies, when a
// test changes them.
let next: Prompt[] | null = null
let nextReplies: Prompt[] | null = null

interface HarnessProps {
  initial?: Prompt[]
  initialReplies?: Prompt[]
  onChanged?: () => void
}

// Harness holds the library the way App does: a reload brings in `next` and
// `nextReplies`, where set.
function Harness({ initial = prompts(), initialReplies = replyPrompts(), onChanged }: HarnessProps) {
  const [list, setList] = useState(initial)
  const [replies, setReplies] = useState(initialReplies)

  return (
    <PromptsView
      agents={agents()}
      prompts={list}
      replies={replies}
      defaults={defaultPrompts()}
      variables={variables()}
      items={[item(), item({ id: 2, kind: 'issue', number: 12, title: 'Crash on start' })]}
      onChanged={() => {
        onChanged?.()

        if (next !== null) {
          setList(next)
          next = null
        }

        if (nextReplies !== null) {
          setReplies(nextReplies)
          nextReplies = null
        }

        return Promise.resolve()
      }}
    />
  )
}

function library() {
  return within(screen.getByRole('navigation', { name: 'Prompts' }))
}

// lastCheck returns the request of the latest check.
function lastCheck(): PromptCheckRequest {
  const call = platform.checkPrompt.mock.lastCall

  if (call === undefined) {
    throw new Error('no check yet')
  }

  return call[0] as PromptCheckRequest
}

// waitForCheck waits for the debounced check of the current draft.
async function waitForCheck() {
  await waitFor(() => {
    expect(platform.checkPrompt).toHaveBeenCalled()
  })
}

beforeEach(() => {
  next = null
  nextReplies = null
})

describe('PromptsView', () => {
  it('lists the library by kind and opens the first prompt', async () => {
    render(<Harness />)

    expect(
      within(library().getByRole('region', { name: 'Pull requests' }))
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual([
      expect.stringContaining('Fix checks'),
      expect.stringContaining('Quick look'),
      expect.stringContaining('Review'),
    ])
    expect(library().getByRole('region', { name: 'Issues' })).toHaveTextContent('Work on it')
    expect(library().getByRole('region', { name: 'Both' })).toHaveTextContent('Explain')
    expect(library().getByRole('button', { name: /Quick look/ })).toHaveTextContent('Default in 2 places')
    expect(library().getByRole('button', { name: /Fix checks/ })).toHaveAttribute('aria-current', 'true')

    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Fix a pull request’s failing checks')
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Fix checks')
    expect(screen.getByRole('radio', { name: 'Pull requests' })).toBeChecked()
    expect(screen.getByText('Starter prompt')).toBeInTheDocument()

    await waitForCheck()
    expect(platform.checkPrompt).toHaveBeenLastCalledWith(
      expect.objectContaining({ id: 3, itemId: 1, inWorktree: true, atPrHead: true }),
    )
    expect(await screen.findByText('No problems')).toBeInTheDocument()
  })

  it('shows where a prompt is a default', async () => {
    render(<Harness />)

    await userEvent.click(library().getByRole('button', { name: /Quick look/ }))

    expect(screen.getByText('1 org · 1 repo')).toBeInTheDocument()

    await userEvent.click(library().getByRole('button', { name: /^Review/ }))

    expect(screen.getByText('App default for pull requests')).toBeInTheDocument()
  })

  it('edits and saves a prompt', async () => {
    const onChanged = vi.fn()
    render(<Harness onChanged={onChanged} />)
    await waitForCheck()

    const nickname = screen.getByRole('textbox', { name: 'Nickname' })
    await userEvent.clear(nickname)
    await userEvent.type(nickname, 'Fix CI')

    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    expect(screen.getByText('6/20')).toBeInTheDocument()
    expect(library().getByLabelText('Unsaved changes')).toBeInTheDocument()

    await waitFor(() => {
      expect(lastCheck().input.nickname).toBe('Fix CI')
    })

    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.updatePrompt).toHaveBeenCalledWith(
      3,
      expect.objectContaining({ nickname: 'Fix CI', appliesTo: 'pr' }),
    )
    expect(await screen.findByText('Saved.')).toBeInTheDocument()
    expect(onChanged).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('keeps the draft and says why when a save is refused', async () => {
    platform.updatePrompt.mockRejectedValue(new Error('a prompt named Fix CI already exists'))
    const onChanged = vi.fn()
    render(<Harness onChanged={onChanged} />)
    await waitForCheck()

    const nickname = screen.getByRole('textbox', { name: 'Nickname' })
    await userEvent.clear(nickname)
    await userEvent.type(nickname, 'Fix CI')
    await waitFor(() => {
      expect(lastCheck().input.nickname).toBe('Fix CI')
    })

    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('a prompt named Fix CI already exists')
    expect(nickname).toHaveValue('Fix CI')
    expect(onChanged).not.toHaveBeenCalled()
  })

  it('edits the launch settings a prompt sets', async () => {
    render(<Harness />)
    await waitForCheck()

    const toggle = screen.getByRole('button', { name: /Launch settings/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveTextContent('Inherits everything')

    await userEvent.click(toggle)

    await userEvent.type(screen.getByRole('combobox', { name: 'Claude model' }), 'opus')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Effort' }), 'max')
    await userEvent.click(
      within(screen.getByRole('radiogroup', { name: 'Plan mode' })).getByRole('radio', { name: 'On' }),
    )
    await userEvent.click(
      within(screen.getByRole('radiogroup', { name: 'Start in a worktree' })).getByRole('radio', { name: 'Off' }),
    )
    await userEvent.type(screen.getByRole('textbox', { name: 'Claude extra flags' }), '--advisor fable')
    await userEvent.click(
      within(screen.getByRole('radiogroup', { name: 'Agent' })).getByRole('radio', { name: 'Codex' }),
    )
    await userEvent.type(screen.getByRole('combobox', { name: 'Codex model' }), 'gpt-6-sol')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Effort' }), 'ultra')

    expect(toggle).toHaveTextContent('Claude: Opus')
    expect(toggle).toHaveTextContent('Codex: GPT-6-Sol')
    expect(toggle).toHaveTextContent('ultra effort')
    expect(toggle).toHaveTextContent('plan mode')
    expect(toggle).toHaveTextContent('no worktree')
    expect(toggle).toHaveTextContent('2 flags')

    await waitFor(() => {
      expect(lastCheck().input.extraFlags).toEqual({ claude: '--advisor fable' })
    })
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.updatePrompt).toHaveBeenCalledWith(
      3,
      expect.objectContaining({
        models: { claude: 'opus', codex: 'gpt-6-sol' },
        effort: 'ultra',
        planMode: true,
        worktree: false,
        extraFlags: { claude: '--advisor fable' },
      }),
    )
  })

  it('shows a problem with the launch settings', async () => {
    platform.checkPrompt.mockResolvedValue(
      checkResult({ fields: [{ field: 'extraFlags.claude', message: '--model is set by the Model setting.' }] }),
    )
    render(<Harness />)

    const toggle = await screen.findByRole('button', { name: /Has a problem/ })

    await userEvent.click(toggle)

    expect(screen.getByRole('textbox', { name: 'Claude extra flags' })).toHaveAccessibleDescription(
      '--model is set by the Model setting.',
    )
    // The refused agent's tab is marked, so a problem under another tab is not missed.
    expect(
      within(screen.getByRole('radiogroup', { name: 'Agent' })).getByRole('radio', { name: 'Claude !' }),
    ).toBeChecked()

    await userEvent.click(
      within(screen.getByRole('radiogroup', { name: 'Plan mode' })).getByRole('radio', { name: 'Inherit' }),
    )
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('keeps edits typed while a save is in flight', async () => {
    let finish: (p: Prompt) => void = () => undefined
    platform.updatePrompt.mockReturnValue(
      new Promise<Prompt>((resolve) => {
        finish = resolve
      }),
    )
    render(<Harness />)
    await waitForCheck()

    const title = screen.getByRole('textbox', { name: 'Title' })
    await userEvent.type(title, ' A')
    await waitFor(() => {
      expect(lastCheck().input.name).toBe('Fix a pull request’s failing checks A')
    })
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await userEvent.type(title, 'B')

    finish(prompt({ id: 3, name: 'Fix a pull request’s failing checks A', nickname: 'Fix checks' }))

    expect(await screen.findByText('Unsaved changes')).toBeInTheDocument()
    expect(title).toHaveValue('Fix a pull request’s failing checks AB')
  })

  it('saves with ⌘S', async () => {
    render(<Harness />)
    await waitForCheck()

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), '!')
    await userEvent.keyboard('{Meta>}s{/Meta}')

    expect(platform.updatePrompt).toHaveBeenCalledWith(
      3,
      expect.objectContaining({ name: 'Fix a pull request’s failing checks!' }),
    )
  })

  it('blocks saving while the check finds problems', async () => {
    platform.checkPrompt.mockResolvedValue(
      checkResult({
        fields: [{ field: 'nickname', message: 'Another prompt is already called "Review".' }],
        diagnostics: [
          {
            severity: 'error',
            line: 1,
            column: 9,
            length: 6,
            message: '.Titel isn’t a variable. Did you mean .Title?',
          },
        ],
        preview: '',
      }),
    )
    render(<Harness />)

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), ' x')

    expect(await screen.findByText('Another prompt is already called "Review".')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('button', { name: /Line 1, column 9/ })).toHaveTextContent('Did you mean .Title?')
    expect(screen.getByText('Fix 2 problems to save.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.getByText('Fix the problems in the prompt to see it filled in.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Line 1, column 9/ }))

    const body = screen.getByRole<HTMLTextAreaElement>('textbox', { name: 'Prompt text' })
    expect(body).toHaveFocus()
    expect([body.selectionStart, body.selectionEnd]).toEqual([8, 14])
  })

  it('asks before leaving unsaved changes', async () => {
    render(<Harness />)

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), ' draft')
    await userEvent.click(library().getByRole('button', { name: /Quick look/ }))

    const dialog = within(screen.getByRole('dialog', { name: 'Discard your changes?' }))
    expect(dialog.getByText('“Fix checks” has changes that aren’t saved.')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Keep editing' }))
    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Fix a pull request’s failing checks draft')

    await userEvent.click(library().getByRole('button', { name: /Quick look/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Discard changes' }))

    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Take a quick look for blockers')
  })

  it('discards a draft', async () => {
    render(<Harness />)

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), ' draft')
    await userEvent.click(screen.getByRole('button', { name: 'Discard' }))

    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Fix a pull request’s failing checks')
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()
  })

  it('creates a new prompt', async () => {
    next = [...prompts(), prompt({ id: 9, name: 'Address review feedback', nickname: 'Feedback', starterKey: '' })]
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'New' }))

    expect(library().getByText('New prompt')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('')
    expect(screen.getByRole('textbox', { name: 'Prompt text' })).toHaveValue(
      'Look at {{.Kind}} #{{.Number}} in {{.Repo}}: {{.Title}}\n{{.URL}}\n\n',
    )

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), 'Address review feedback')
    await userEvent.type(screen.getByRole('textbox', { name: 'Nickname' }), 'Feedback')
    await waitFor(() => {
      expect(platform.checkPrompt).toHaveBeenLastCalledWith(expect.objectContaining({ id: 0 }))
    })
    await userEvent.click(screen.getByRole('button', { name: 'Create prompt' }))

    expect(platform.createPrompt).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'Address review feedback', nickname: 'Feedback', appliesTo: 'pr' }),
    )
    expect(await screen.findByText('Prompt created.')).toBeInTheDocument()
    expect(library().getByRole('button', { name: /Feedback/ })).toHaveAttribute('aria-current', 'true')
  })

  it('points at a new prompt’s empty fields only once they are edited', async () => {
    platform.checkPrompt.mockResolvedValue(
      checkResult({
        fields: [
          { field: 'name', message: 'Give the prompt a title.' },
          { field: 'nickname', message: 'Give the prompt a nickname for the launch menu.' },
        ],
      }),
    )
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'New' }))
    await waitFor(() => {
      expect(lastCheck().id).toBe(0)
    })

    expect(await screen.findByText('Give the prompt a title and a nickname to create it.')).toBeInTheDocument()
    expect(screen.queryByText('Give the prompt a title.')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Create prompt' })).toBeDisabled()

    await userEvent.type(screen.getByRole('textbox', { name: 'Nickname' }), 'x')

    expect(await screen.findByText('Give the prompt a nickname for the launch menu.')).toBeInTheDocument()
    expect(screen.getByText('Fix 1 problem to save.')).toBeInTheDocument()
  })

  it('cancels a new prompt', async () => {
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'New' }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), 'x')
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Fix a pull request’s failing checks')
  })

  it('deletes a prompt nothing uses, after asking', async () => {
    next = prompts().filter((p) => p.id !== 3)
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'More actions for Fix checks' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete…' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Delete “Fix checks”?' }))
    expect(dialog.getByText('It’s a starter prompt, and it won’t be added back.')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Delete prompt' }))

    expect(platform.deletePrompt).toHaveBeenCalledWith(3)
    expect(await screen.findByText('Deleted “Fix checks”.')).toBeInTheDocument()
    expect(library().getByRole('button', { name: /Quick look/ })).toHaveAttribute('aria-current', 'true')
  })

  it('explains why a prompt in use cannot be deleted', async () => {
    render(<Harness />)

    await userEvent.click(library().getByRole('button', { name: /Quick look/ }))
    await userEvent.click(screen.getByRole('button', { name: 'More actions for Quick look' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete…' }))

    const dialog = within(screen.getByRole('dialog', { name: '“Quick look” is still in use' }))
    expect(dialog.getByText('acme-org (all repositories)')).toBeInTheDocument()
    expect(dialog.getByText('acme-org/api')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Got it' }))

    expect(platform.deletePrompt).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('resets a changed starter, makes a prompt a default, and duplicates one', async () => {
    const changed = prompts().map((p) => (p.id === 3 ? { ...p, modified: true } : p))
    render(<Harness initial={changed} />)

    expect(screen.getByText('Starter prompt · changed')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'More actions for Fix checks' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Reset to original' }))
    await userEvent.click(screen.getByRole('button', { name: 'Reset to original' }))

    expect(platform.resetPrompt).toHaveBeenCalledWith(3)
    expect(await screen.findByText('Reset “Fix checks” to its original text and settings.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'More actions for Fix checks' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Make the default for pull requests' }))

    expect(platform.setDefaultPrompt).toHaveBeenCalledWith('pr', 3)
    expect(await screen.findByText('“Fix checks” is now the default for pull requests.')).toBeInTheDocument()

    next = [
      ...changed,
      prompt({ id: 9, nickname: 'Fix checks copy', name: 'Fix a pull request’s failing checks (copy)' }),
    ]
    platform.duplicatePrompt.mockResolvedValue(prompt({ id: 9 }))

    await userEvent.click(screen.getByRole('button', { name: 'More actions for Fix checks' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Duplicate' }))

    expect(platform.duplicatePrompt).toHaveBeenCalledWith(3)
    expect(await screen.findByText('Duplicated “Fix checks”.')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Fix checks copy')
  })

  it('shows an action that failed', async () => {
    platform.setDefaultPrompt.mockRejectedValue(new Error('the prompt doesn’t apply to that kind of item'))
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'More actions for Fix checks' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Make the default for pull requests' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('doesn’t apply to that kind of item')
  })

  it('searches the library', async () => {
    render(<Harness />)

    await userEvent.type(screen.getByRole('searchbox', { name: 'Search prompts' }), 'issue')

    expect(
      library()
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual([expect.stringContaining('Work on it')])

    await userEvent.clear(screen.getByRole('searchbox', { name: 'Search prompts' }))
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search prompts' }), 'zzz')

    expect(library().getByText('No prompt matches “zzz”.')).toBeInTheDocument()
  })

  it('previews with the items the prompt applies to', async () => {
    render(<Harness />)
    await waitForCheck()

    const preview = screen.getByRole('combobox', { name: 'Preview with' })
    expect(
      within(preview)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['A sample pull request', 'example-repo #7 · Add the widget cache'])
    expect(await screen.findByText(/Review pull request #7/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: 'Issues' }))
    await waitFor(() => {
      expect([lastCheck().itemId, lastCheck().input.appliesTo]).toEqual([2, 'issue'])
    })

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Preview with' }), '0')
    await waitFor(() => {
      expect(platform.checkPrompt).toHaveBeenLastCalledWith(expect.objectContaining({ itemId: 0 }))
    })

    // A sample picked on purpose stays picked.
    await userEvent.click(screen.getByRole('radio', { name: 'Both' }))
    await waitFor(() => {
      expect([lastCheck().itemId, lastCheck().input.appliesTo]).toEqual([0, 'any'])
    })
  })

  it('inserts a variable from the cheat sheet', async () => {
    render(<Harness />)

    await userEvent.click(screen.getByRole('tab', { name: 'Variables' }))

    expect(screen.getByRole('tab', { name: 'Variables' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Values are for #7.')

    const body = screen.getByRole<HTMLTextAreaElement>('textbox', { name: 'Prompt text' })
    body.setSelectionRange(0, 0)

    await userEvent.click(screen.getByRole('button', { name: /\{\{\.Number\}\}/ }))

    expect(body.value.startsWith('{{.Number}}')).toBe(true)

    await userEvent.click(screen.getByRole('tab', { name: 'Variables' }))
    await userEvent.keyboard('{ArrowLeft}')
    expect(screen.getByRole('tab', { name: 'Preview' })).toHaveAttribute('aria-selected', 'true')
  })

  it('shows a check that could not run', async () => {
    platform.checkPrompt.mockRejectedValue(new Error('database is locked'))
    render(<Harness />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not check the prompt: database is locked')
  })

  it('offers to create a prompt when the library is empty', async () => {
    render(<Harness initial={[]} />)

    await userEvent.click(screen.getByRole('button', { name: 'New prompt' }))

    expect(screen.getByRole('button', { name: 'Create prompt' })).toBeInTheDocument()
  })

  it('offers to restore prompts when the library is empty', async () => {
    render(<Harness initial={[]} />)

    await userEvent.click(screen.getByRole('button', { name: 'Restore from a backup' }))

    expect(await screen.findByRole('heading', { name: 'Restore prompts from a backup' })).toBeInTheDocument()
  })

  it('exports the whole library, and one prompt', async () => {
    platform.chooseFileToSave.mockResolvedValue('/exports/all.json')
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Export all prompts…' }))

    const asked = platform.chooseFileToSave.mock.lastCall?.[0]
    expect(asked?.filename).toMatch(new RegExp(`^${brand.name} prompts \\d{4}-\\d{2}-\\d{2}\\.json$`))
    expect(asked?.filters).toEqual([{ name: `${brand.name} prompts`, pattern: '*.json' }])
    expect(platform.exportPrompts).toHaveBeenCalledWith('/exports/all.json', [])
    expect(await screen.findByText(`Exported ${String(prompts().length)} prompts to “all.json”.`)).toBeInTheDocument()

    platform.chooseFileToSave.mockResolvedValue('/exports/Fix checks.json')
    await userEvent.click(screen.getByRole('button', { name: 'More actions for Fix checks' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Export…' }))

    expect(platform.chooseFileToSave).toHaveBeenLastCalledWith(expect.objectContaining({ filename: 'Fix checks.json' }))
    expect(platform.exportPrompts).toHaveBeenLastCalledWith('/exports/Fix checks.json', [3])
    expect(await screen.findByText('Exported 1 prompt to “Fix checks.json”.')).toBeInTheDocument()
  })

  it('writes nothing when the save dialog is cancelled', async () => {
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Export all prompts…' }))

    await waitFor(() => {
      expect(platform.chooseFileToSave).toHaveBeenCalled()
    })
    expect(platform.exportPrompts).not.toHaveBeenCalled()
  })

  it('shows an export that failed', async () => {
    platform.chooseFileToSave.mockResolvedValue('/Volumes/gone/all.json')
    platform.exportPrompts.mockRejectedValue(new Error('the disk is gone'))
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Export all prompts…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the disk is gone')
  })

  it('imports prompts from a file and opens the first one', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/exports/shared.json')
    platform.previewPromptImport.mockResolvedValue([importEntry()])
    platform.importPrompts.mockResolvedValue({ added: 1, replaced: 0, skipped: 0, first: 9 })
    next = [...prompts(), prompt({ id: 9, nickname: 'Security', name: 'Look for security problems', appliesTo: 'any' })]
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Import from a file…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Import 1 prompt' }))

    expect(platform.chooseFileToOpen.mock.lastCall?.[0].filters).toEqual([
      { name: `${brand.name} prompts`, pattern: '*.json' },
      { name: `${brand.name} database`, pattern: '*.db' },
    ])
    expect(platform.previewPromptImport).toHaveBeenCalledWith({
      kind: 'file',
      path: '/exports/shared.json',
    })
    expect(await screen.findByText('Imported 1 prompt.')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Security')
  })

  it('restores prompts from a backup', async () => {
    platform.importPrompts.mockResolvedValue({ added: 1, replaced: 2, skipped: 1, first: 1 })
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Restore from a backup…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Show prompts' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Restore 1 prompt' }))

    expect(await screen.findByText('Restored 3 prompts. One was already in your library.')).toBeInTheDocument()
  })

  it('keeps the open prompt after an import that brings in nothing new', async () => {
    platform.importPrompts.mockResolvedValue({ added: 0, replaced: 0, skipped: 2, first: 0 })
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Restore from a backup…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Show prompts' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Restore 1 prompt' }))

    expect(await screen.findByText('Restored 0 prompts. 2 were already in your library.')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Fix checks')
  })

  it('keeps the open prompt when the one an action names is not in the library', async () => {
    platform.importPrompts.mockResolvedValue({ added: 1, replaced: 0, skipped: 0, first: 99 })
    next = prompts()
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Restore from a backup…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Show prompts' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Restore 1 prompt' }))

    expect(await screen.findByText('Restored 1 prompt.')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Fix checks')
    expect(screen.getByRole('radio', { name: 'Agent prompts' })).toBeChecked()
  })

  it('cancels an import', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/exports/shared.json')
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Import from a file…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.importPrompts).not.toHaveBeenCalled()
  })

  it('says when the save dialog cannot open', async () => {
    platform.chooseFileToSave.mockRejectedValue(new Error('no window'))
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Export all prompts…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t open the save dialog: no window')
    expect(platform.exportPrompts).not.toHaveBeenCalled()
  })

  it('says when a file dialog cannot open', async () => {
    platform.chooseFileToOpen.mockRejectedValue(new Error('no window'))
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Import from a file…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t open the file dialog: no window')
  })

  it('asks for unsaved changes to be dealt with before an import', async () => {
    render(<Harness />)

    await userEvent.type(screen.getByRole('textbox', { name: 'Nickname' }), '!')
    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))

    expect(screen.getByRole('menuitem', { name: 'Import from a file…' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: 'Restore from a backup…' })).toBeDisabled()
    expect(screen.getByRole('menuitem', { name: 'Export all prompts…' })).toBeEnabled()
  })
})

describe('PromptsView review replies', () => {
  // replyLibrary returns the replies part of the library.
  function replyLibrary() {
    return within(screen.getByRole('navigation', { name: 'Review replies' }))
  }

  // showReplies switches the library to the review replies.
  async function showReplies() {
    await userEvent.click(screen.getByRole('radio', { name: 'Review replies' }))
  }

  // menuOf opens the ⋯ menu of the open prompt and lists its items.
  async function menuOf(nickname: string): Promise<(string | null)[]> {
    await userEvent.click(screen.getByRole('button', { name: `More actions for ${nickname}` }))

    return screen.getAllByRole('menuitem').map((m) => m.textContent)
  }

  // editor is the editor's half of the page.
  function editor() {
    return within(screen.getByRole('region', { name: 'Prompt editor' }))
  }

  it('switches to the replies, grouped by review, each marked with the defaults it is', async () => {
    const [lgtm, updateOk, needsChanges, question] = replyPrompts()
    render(
      <Harness
        initialReplies={[
          replyPrompt({ ...lgtm }),
          replyPrompt({ ...updateOk }),
          replyPrompt({
            ...needsChanges,
            usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: ['request_changes'] },
          }),
          replyPrompt({ ...question, usage: { defaults: [], orgs: [], repos: [], kinds: [], replies: ['comment'] } }),
        ]}
      />,
    )

    expect(screen.getByRole('radiogroup', { name: 'Kind of prompt' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Agent prompts' })).toBeChecked()

    await showReplies()

    expect(screen.getByRole('radio', { name: 'Review replies' })).toBeChecked()
    expect(screen.queryByRole('navigation', { name: 'Prompts' })).not.toBeInTheDocument()
    expect(
      within(replyLibrary().getByRole('region', { name: 'Approvals' }))
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual([expect.stringContaining('LGTM'), expect.stringContaining('Update OK')])
    expect(replyLibrary().getByRole('button', { name: /LGTM/ })).toHaveTextContent('Default for approvals')
    // The row's tooltip holds its nickname and name whole, which the row may cut short.
    expect(replyLibrary().getByRole('button', { name: /LGTM/ })).toHaveAttribute(
      'title',
      'LGTM\nApprove with thanks\nDefault for approvals',
    )
    expect(replyLibrary().getByRole('button', { name: /Update OK/ })).toHaveTextContent('Default for bots')
    expect(replyLibrary().getByRole('region', { name: 'Requests for changes' })).toHaveTextContent(
      'Needs changesAsk for changesDefault for changes',
    )
    expect(replyLibrary().getByRole('region', { name: 'Comments' })).toHaveTextContent(
      'QuestionAsk a questionDefault for comments',
    )
    expect(replyLibrary().getByRole('button', { name: /LGTM/ })).toHaveAttribute('aria-current', 'true')

    await userEvent.click(screen.getByRole('radio', { name: 'Agent prompts' }))

    expect(library().getByRole('button', { name: /Fix checks/ })).toHaveAttribute('aria-current', 'true')
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Fix checks')
  })

  it('edits a reply with its action in place of what it is for, and no launch settings', async () => {
    render(<Harness />)
    await showReplies()

    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Approve with thanks')
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveAccessibleDescription(
      'Shown when you pick a reply.',
    )
    expect(
      within(screen.getByRole('radiogroup', { name: 'Action' }))
        .getAllByRole('radio')
        .map((r) => r.parentElement?.textContent),
    ).toEqual(['Approve', 'Request changes', 'Comment'])
    expect(screen.getByRole('radio', { name: 'Approve' })).toBeChecked()
    expect(screen.queryByRole('radiogroup', { name: 'What the prompt is for' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Launch settings/ })).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Reply text' })).toHaveValue('Looks good to me. Thanks, @{{.Author}}!\n')
    expect(screen.getByText('Starter reply')).toBeInTheDocument()
    expect(editor().getByText('Default for approvals')).toBeInTheDocument()

    await userEvent.click(replyLibrary().getByRole('button', { name: /Needs changes/ }))

    expect(screen.getByRole('radio', { name: 'Request changes' })).toBeChecked()
    expect(editor().getByText('Not a default')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: 'Comment' }))

    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
    await waitFor(() => {
      expect(lastCheck().input.replyAction).toBe('comment')
    })
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.updatePrompt).toHaveBeenCalledWith(
      23,
      expect.objectContaining({ purpose: 'reply', replyAction: 'comment', nickname: 'Needs changes' }),
    )
    expect(await screen.findByText('Saved.')).toBeInTheDocument()
  })

  it('asks what a comment reply is for, which an approval never is', async () => {
    render(
      <Harness
        initialReplies={replyPrompts().map((p) =>
          p.nickname === 'Question' ? { ...p, appliesTo: 'any' as const } : p,
        )}
      />,
    )
    await showReplies()

    expect(screen.queryByRole('radiogroup', { name: 'What the reply is for' }), 'an approval').not.toBeInTheDocument()
    expect(replyLibrary().getByRole('button', { name: /Question/ })).toHaveTextContent('For pull requests and issues')
    expect(replyLibrary().getByRole('button', { name: /LGTM/ })).not.toHaveTextContent(/For /)

    await userEvent.click(replyLibrary().getByRole('button', { name: /Question/ }))
    expect(screen.getByRole('radio', { name: 'Both' })).toBeChecked()

    await userEvent.click(screen.getByRole('radio', { name: 'Issues' }))
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.updatePrompt).toHaveBeenCalledWith(
      24,
      expect.objectContaining({ purpose: 'reply', replyAction: 'comment', appliesTo: 'issue' }),
    )
    expect(await screen.findByText('Saved.')).toBeInTheDocument()

    // An approval is a review of a pull request, so it leaves what the reply was for behind.
    await userEvent.click(screen.getByRole('radio', { name: 'Approve' }))
    expect(screen.queryByRole('radiogroup', { name: 'What the reply is for' })).not.toBeInTheDocument()
    await waitFor(() => {
      expect(lastCheck().input).toEqual(expect.objectContaining({ replyAction: 'approve', appliesTo: 'pr' }))
    })
  })

  it('shows why a reply’s action is refused', async () => {
    platform.checkPrompt.mockResolvedValue(
      checkResult({
        fields: [
          {
            field: 'replyAction',
            message: 'It’s the default for approvals, so it has to stay a reply for that review.',
          },
        ],
      }),
    )
    render(<Harness />)
    await showReplies()

    await userEvent.click(screen.getByRole('radio', { name: 'Comment' }))

    expect(
      await screen.findByText('It’s the default for approvals, so it has to stay a reply for that review.'),
    ).toBeInTheDocument()
    expect(screen.getByText('Fix 1 problem to save.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('previews a reply with a pull request, and no session', async () => {
    render(<Harness />)
    await showReplies()
    await waitFor(() => {
      expect(lastCheck().input.purpose).toBe('reply')
    })

    expect(lastCheck()).toEqual(expect.objectContaining({ id: 21, itemId: 1, inWorktree: false, atPrHead: false }))
    expect(
      within(screen.getByRole('combobox', { name: 'Preview with' }))
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['A sample pull request', 'example-repo #7 · Add the widget cache'])
    expect(screen.queryByRole('checkbox', { name: 'In a worktree' })).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: /At the PR/ })).not.toBeInTheDocument()
    expect(await screen.findByText(/Review pull request #7/)).toBeInTheDocument()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Preview with' }), '0')
    await waitFor(() => {
      expect(lastCheck().itemId).toBe(0)
    })
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Preview with' }), '1')
    await waitFor(() => {
      expect(lastCheck().itemId).toBe(1)
    })
  })

  it('creates a new reply, an approval thanking the author until changed', async () => {
    nextReplies = [
      ...replyPrompts(),
      replyPrompt({ id: 9, name: 'Thank a first-time contributor', nickname: 'Welcome' }),
    ]
    render(<Harness />)
    await showReplies()

    await userEvent.click(screen.getByRole('button', { name: 'New reply' }))

    expect(replyLibrary().getByText('New reply')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('')
    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveAttribute(
      'placeholder',
      'Name this reply, such as “Approve with thanks”',
    )
    expect(screen.getByRole('textbox', { name: 'Reply text' })).toHaveValue('Thanks, @{{.Author}}!\n')
    expect(screen.getByRole('radio', { name: 'Approve' })).toBeChecked()

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), 'Thank a first-time contributor')
    await userEvent.type(screen.getByRole('textbox', { name: 'Nickname' }), 'Welcome')
    await waitFor(() => {
      expect(lastCheck()).toEqual(expect.objectContaining({ id: 0 }))
    })
    await userEvent.click(screen.getByRole('button', { name: 'Create reply' }))

    expect(platform.createPrompt).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'Thank a first-time contributor',
        nickname: 'Welcome',
        purpose: 'reply',
        replyAction: 'approve',
        body: 'Thanks, @{{.Author}}!\n',
      }),
    )
    expect(await screen.findByText('Reply created.')).toBeInTheDocument()
    expect(replyLibrary().getByRole('button', { name: /Welcome/ })).toHaveAttribute('aria-current', 'true')
  })

  it('asks before leaving a new reply, and points at its empty fields only once edited', async () => {
    platform.checkPrompt.mockResolvedValue(
      checkResult({
        fields: [
          { field: 'name', message: 'Give the prompt a title.' },
          { field: 'nickname', message: 'Give the prompt a nickname for the launch menu.' },
        ],
      }),
    )
    render(<Harness />)
    await showReplies()

    await userEvent.click(screen.getByRole('button', { name: 'New reply' }))

    expect(await screen.findByText('Give the reply a title and a nickname to create it.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: 'Comment' }))
    await userEvent.click(replyLibrary().getByRole('button', { name: /Question/ }))

    const dialog = within(screen.getByRole('dialog', { name: 'Discard your changes?' }))
    expect(dialog.getByText('The new reply hasn’t been created yet.')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Discard changes' }))

    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Question')
  })

  it('asks before switching parts of the library with unsaved changes', async () => {
    render(<Harness />)

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), ' draft')
    await showReplies()

    const dialog = within(screen.getByRole('dialog', { name: 'Discard your changes?' }))
    expect(dialog.getByText('“Fix checks” has changes that aren’t saved.')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Keep editing' }))

    expect(screen.getByRole('radio', { name: 'Agent prompts' })).toBeChecked()
    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Fix a pull request’s failing checks draft')

    await showReplies()
    await userEvent.click(screen.getByRole('button', { name: 'Discard changes' }))

    expect(screen.getByRole('radio', { name: 'Review replies' })).toBeChecked()
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('LGTM')
  })

  it('offers only the defaults for a reply’s own review, and makes it one', async () => {
    render(<Harness />)
    await showReplies()

    expect(await menuOf('LGTM')).toEqual([
      'Make the default for approvals on bot pull requests',
      'Duplicate',
      'Export…',
      'Delete…',
    ])

    await userEvent.click(screen.getByRole('menuitem', { name: 'Make the default for approvals on bot pull requests' }))

    expect(platform.setReplyDefault).toHaveBeenCalledWith('approve_bots', 21)
    expect(await screen.findByText('“LGTM” is now the default for approvals on bot pull requests.')).toBeInTheDocument()

    await userEvent.click(replyLibrary().getByRole('button', { name: /Needs changes/ }))

    expect(await menuOf('Needs changes')).toEqual([
      'Make the default for requests for changes',
      'Duplicate',
      'Export…',
      'Delete…',
    ])

    await userEvent.keyboard('{Escape}')
    await userEvent.click(replyLibrary().getByRole('button', { name: /Question/ }))

    expect(await menuOf('Question')).toContain('Make the default for comments')

    await userEvent.click(screen.getByRole('menuitem', { name: 'Make the default for comments' }))

    expect(platform.setReplyDefault).toHaveBeenLastCalledWith('comment', 24)
  })

  it('offers both approval defaults to an approval that is neither', async () => {
    render(<Harness initialReplies={[replyPrompt({ usage: { ...replyPrompt().usage, replies: [] } })]} />)
    await showReplies()

    expect((await menuOf('LGTM')).slice(0, 2)).toEqual([
      'Make the default for approvals',
      'Make the default for approvals on bot pull requests',
    ])

    await userEvent.click(screen.getByRole('menuitem', { name: 'Make the default for approvals' }))

    expect(platform.setReplyDefault).toHaveBeenCalledWith('approve', 21)
  })

  it('deletes a reply that is a default, saying it stops being one', async () => {
    nextReplies = replyPrompts().filter((p) => p.id !== 21)
    render(<Harness />)
    await showReplies()

    await menuOf('LGTM')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete…' }))

    const dialog = within(screen.getByRole('dialog', { name: 'Delete “LGTM”?' }))
    expect(
      dialog.getByText('It’s a starter reply, and it won’t be added back. It stops being the default for approvals.'),
    ).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Delete reply' }))

    expect(platform.deletePrompt).toHaveBeenCalledWith(21)
    expect(await screen.findByText('Deleted “LGTM”.')).toBeInTheDocument()
    expect(replyLibrary().getByRole('button', { name: /Update OK/ })).toHaveAttribute('aria-current', 'true')
  })

  it('shows no reply once the last one is deleted, and offers a new one', async () => {
    const [, , , question] = replyPrompts()
    nextReplies = []
    render(<Harness initialReplies={question === undefined ? [] : [question]} />)
    await showReplies()

    await menuOf('Question')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete…' }))

    expect(screen.getByText('This can’t be undone.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Delete reply' }))

    expect(await editor().findByText('No replies yet.')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Title' })).not.toBeInTheDocument()

    await userEvent.click(editor().getByRole('button', { name: 'New reply' }))

    expect(screen.getByRole('button', { name: 'Create reply' })).toBeInTheDocument()
  })

  it('resets a changed starter reply', async () => {
    render(<Harness initialReplies={[replyPrompt({ modified: true })]} />)
    await showReplies()

    expect(screen.getByText('Starter reply · changed')).toBeInTheDocument()

    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), '!')
    await menuOf('LGTM')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Reset to original' }))

    expect(
      screen.getByText(
        'Its title, text, and action go back to how the app ships them. Your unsaved changes are discarded too.',
      ),
    ).toBeInTheDocument()

    nextReplies = [replyPrompt()]
    await userEvent.click(screen.getByRole('button', { name: 'Reset to original' }))

    expect(platform.resetPrompt).toHaveBeenCalledWith(21)
    expect(await screen.findByText('Reset “LGTM” to its original text and action.')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Title' })).toHaveValue('Approve with thanks')
    expect(screen.getByText('Starter reply')).toBeInTheDocument()
  })

  it('searches the replies', async () => {
    render(<Harness />)
    await showReplies()

    await userEvent.type(screen.getByRole('searchbox', { name: 'Search replies' }), 'dependency')

    expect(
      replyLibrary()
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual([expect.stringContaining('Update OK')])

    await userEvent.clear(screen.getByRole('searchbox', { name: 'Search replies' }))
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search replies' }), 'zzz')

    expect(replyLibrary().getByText('No reply matches “zzz”.')).toBeInTheDocument()
  })

  it('opens an imported reply in the replies', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/exports/replies.json')
    platform.importPrompts.mockResolvedValue({ added: 1, replaced: 0, skipped: 0, first: 9 })
    nextReplies = [...replyPrompts(), replyPrompt({ id: 9, nickname: 'Ship it', name: 'Approve and say so' })]
    render(<Harness />)

    await userEvent.click(screen.getByRole('button', { name: 'Import and export prompts' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Import from a file…' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Import 1 prompt' }))

    expect(await screen.findByText('Imported 1 prompt.')).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Review replies' })).toBeChecked()
    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Ship it')
  })

  it('shows no replies when the page is given none, and cancels a new one back to none', async () => {
    render(
      <PromptsView
        agents={agents()}
        prompts={prompts()}
        replies={[]}
        defaults={defaultPrompts()}
        variables={variables()}
        items={[item()]}
        onChanged={() => Promise.resolve()}
      />,
    )
    await showReplies()

    expect(editor().getByText('No replies yet.')).toBeInTheDocument()

    await userEvent.click(editor().getByRole('button', { name: 'New reply' }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), 'x')
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(editor().getByText('No replies yet.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('radio', { name: 'Agent prompts' }))

    expect(screen.getByRole('textbox', { name: 'Nickname' })).toHaveValue('Fix checks')
  })
})
