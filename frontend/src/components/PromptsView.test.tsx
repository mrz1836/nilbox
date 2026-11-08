import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Prompt, PromptCheckRequest } from '../platform'
import { checkResult, item, prompt, prompts, variables } from '../test/fixtures'
import { PromptsView } from './PromptsView'

const platform = vi.hoisted(() => ({
  checkPrompt: vi.fn(),
  createPrompt: vi.fn(),
  updatePrompt: vi.fn(),
  deletePrompt: vi.fn(),
  duplicatePrompt: vi.fn(),
  resetPrompt: vi.fn(),
  setDefaultPrompt: vi.fn(),
}))

vi.mock('../platform', () => platform)

// next is the library the next reload brings in, when a test changes it.
let next: Prompt[] | null = null

interface HarnessProps {
  initial?: Prompt[]
  onChanged?: () => void
}

// Harness holds the library the way App does: a reload brings in `next`, if set.
function Harness({ initial = prompts(), onChanged }: HarnessProps) {
  const [list, setList] = useState(initial)

  return (
    <PromptsView
      prompts={list}
      defaults={{ pr: 1, issue: 4 }}
      variables={variables()}
      items={[item(), item({ id: 2, kind: 'issue', number: 12, title: 'Crash on start' })]}
      active
      onChanged={() => {
        onChanged?.()

        if (next !== null) {
          setList(next)
          next = null
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
  const call = platform.checkPrompt.mock.lastCall as [PromptCheckRequest] | undefined

  if (call === undefined) {
    throw new Error('no check yet')
  }

  return call[0]
}

// waitForCheck waits for the debounced check of the current draft.
async function waitForCheck() {
  await waitFor(() => {
    expect(platform.checkPrompt).toHaveBeenCalled()
  })
}

beforeEach(() => {
  next = null
  platform.checkPrompt.mockResolvedValue(checkResult())

  for (const fn of [platform.createPrompt, platform.updatePrompt, platform.duplicatePrompt, platform.resetPrompt]) {
    fn.mockImplementation((idOrInput: unknown, input?: Record<string, unknown>) =>
      Promise.resolve(
        prompt({
          ...(input ?? (idOrInput as Record<string, unknown>)),
          id: typeof idOrInput === 'number' ? idOrInput : 9,
        }),
      ),
    )
  }

  platform.deletePrompt.mockResolvedValue(undefined)
  platform.setDefaultPrompt.mockResolvedValue(undefined)
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

  it('edits the launch settings a prompt sets', async () => {
    render(<Harness />)
    await waitForCheck()

    const toggle = screen.getByRole('button', { name: /Launch settings/ })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(toggle).toHaveTextContent('Inherits everything')

    await userEvent.click(toggle)

    await userEvent.type(screen.getByRole('combobox', { name: 'Model' }), 'opus')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Effort' }), 'max')
    await userEvent.click(
      within(screen.getByRole('radiogroup', { name: 'Plan mode' })).getByRole('radio', { name: 'On' }),
    )
    await userEvent.click(
      within(screen.getByRole('radiogroup', { name: 'Start in a worktree' })).getByRole('radio', { name: 'Off' }),
    )
    await userEvent.type(screen.getByRole('textbox', { name: 'Extra flags' }), '--advisor fable')

    expect(toggle).toHaveTextContent('Opus')
    expect(toggle).toHaveTextContent('max effort')
    expect(toggle).toHaveTextContent('plan mode')
    expect(toggle).toHaveTextContent('no worktree')
    expect(toggle).toHaveTextContent('2 flags')

    await waitFor(() => {
      expect(lastCheck().input.extraFlags).toBe('--advisor fable')
    })
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(platform.updatePrompt).toHaveBeenCalledWith(
      3,
      expect.objectContaining({
        model: 'opus',
        effort: 'max',
        planMode: true,
        worktree: false,
        extraFlags: '--advisor fable',
      }),
    )
  })

  it('shows a problem with the launch settings', async () => {
    platform.checkPrompt.mockResolvedValue(
      checkResult({ fields: [{ field: 'extraFlags', message: '--model is set by the Model setting.' }] }),
    )
    render(<Harness />)

    const toggle = await screen.findByRole('button', { name: /Has a problem/ })

    await userEvent.click(toggle)

    expect(screen.getByRole('textbox', { name: 'Extra flags' })).toHaveAccessibleDescription(
      '--model is set by the Model setting.',
    )

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
})
