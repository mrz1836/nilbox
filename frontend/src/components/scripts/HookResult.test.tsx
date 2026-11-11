import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { HookActionResult } from '../../platform'
import { hookActionResult } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { HookResult } from './HookResult'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// blockedResult is an install husky's hook stopped in acme-org/sdk, though go-pre-commit
// exited 0.
function blockedResult(overrides: Partial<HookActionResult> = {}) {
  return hookActionResult({
    outcome: 'nothingInstalled',
    message: 'Nothing installed: husky’s hook is in the way.',
    repo: 'acme-org/sdk',
    name: 'acme-org/sdk',
    dir: '~/projects/sdk',
    hooksDir: '~/projects/sdk/.git/hooks',
    otherTool: 'husky',
    ...overrides,
  })
}

function renderResult() {
  const onSelect = vi.fn<(id: number) => void>()

  render(<HookResult runId={11} onSelect={onSelect} />)

  return { onSelect }
}

describe('HookResult', () => {
  it('says what the action did, as the hook files say, and where', async () => {
    renderResult()

    const heading = await screen.findByRole('heading', { name: /Installed the pre-commit hook in acme-org\/api\./ })

    expect(platform.getHookActionResult).toHaveBeenCalledExactlyOnceWith(11)
    expect(within(heading).getByText('Done:')).toHaveClass('sr-only')
    expect(screen.getByText('~/projects/api/.git/hooks')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Replace/ })).not.toBeInTheDocument()
  })

  it('names no hooks folder it couldn’t read', async () => {
    platform.getHookActionResult.mockResolvedValue(
      hookActionResult({ outcome: 'unverified', message: 'Couldn’t read what go-pre-commit did.', hooksDir: '' }),
    )
    renderResult()

    const heading = await screen.findByRole('heading', { name: /Couldn’t read what go-pre-commit did\./ })

    expect(within(heading).getByText('Warning:')).toBeInTheDocument()
    expect(screen.queryByText(/Hooks folder/)).not.toBeInTheDocument()
  })

  it('offers Replace… when another tool’s hook stopped the install, asking first', async () => {
    platform.getHookActionResult.mockResolvedValue(blockedResult())
    const { onSelect } = renderResult()

    const replace = await screen.findByRole('button', { name: 'Replace the hook in acme-org/sdk…' })

    await userEvent.click(replace)

    let dialog = screen.getByRole('dialog', { name: 'Replace husky’s hook?' })

    expect(dialog).toHaveAccessibleDescription(
      'Replace husky’s pre-commit hook in acme-org/sdk? go-pre-commit moves it to .git/hooks/pre-commit.backup.<number>, and puts it back if you uninstall.',
    )
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(platform.replaceHook).not.toHaveBeenCalled()

    await userEvent.click(replace)
    dialog = screen.getByRole('dialog', { name: 'Replace husky’s hook?' })
    await userEvent.click(within(dialog).getByRole('button', { name: 'Replace husky’s hook' }))

    expect(platform.replaceHook).toHaveBeenCalledExactlyOnceWith({ dir: '~/projects/sdk', confirm: '', runsIn: '' })
    await waitFor(() => {
      expect(onSelect).toHaveBeenCalledExactlyOnceWith(11)
    })
  })

  it('names a shared hooks folder before Replace… changes it, and confirms that folder', async () => {
    platform.getHookActionResult.mockResolvedValue(
      blockedResult({ hooksDir: '~/.githooks', shared: true, sharedWith: 6 }),
    )
    renderResult()

    await userEvent.click(await screen.findByRole('button', { name: 'Replace the hook in acme-org/sdk…' }))

    const dialog = screen.getByRole('dialog', { name: 'Replace husky’s hook?' })

    expect(dialog).toHaveAccessibleDescription(
      /~\/\.githooks is shared by 6 of your repositories, and by any other repository that uses it\. Installing here installs go-pre-commit’s hook for all of them\.$/,
    )
    await userEvent.click(within(dialog).getByRole('button', { name: 'Replace husky’s hook' }))

    expect(platform.replaceHook).toHaveBeenCalledExactlyOnceWith({
      dir: '~/projects/sdk',
      confirm: '~/.githooks',
      runsIn: '',
    })
  })

  it('says why Replace… couldn’t start', async () => {
    platform.getHookActionResult.mockResolvedValue(blockedResult())
    platform.replaceHook.mockRejectedValue(new Error('~/.githooks is shared by other repositories.'))
    const { onSelect } = renderResult()

    await userEvent.click(await screen.findByRole('button', { name: 'Replace the hook in acme-org/sdk…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Replace husky’s hook' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('~/.githooks is shared by other repositories.')
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('shows nothing for a run that ended without a result', async () => {
    platform.getHookActionResult.mockRejectedValue(new Error('This run ended without a result to show.'))
    const { container } = render(<HookResult runId={11} onSelect={vi.fn()} />)

    await vi.waitFor(() => {
      expect(platform.getHookActionResult).toHaveBeenCalled()
    })
    await Promise.resolve()
    expect(container).toBeEmptyDOMElement()
  })
})
