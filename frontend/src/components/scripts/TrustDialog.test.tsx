import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ScriptQuestion } from '../../platform'
import { scriptQuestion } from '../../test/fixtures'
import { TrustDialog } from './TrustDialog'

// renderDialog asks question about acme-org/sdk's clone, on its default branch unless
// fromBranch says otherwise.
function renderDialog(question: ScriptQuestion, fromBranch = '') {
  const onRun = vi.fn<(remember: boolean) => void>()
  const onCancel = vi.fn<() => void>()

  render(
    <TrustDialog
      question={question}
      repo="acme-org/sdk"
      name="acme-org/sdk"
      dir="~/projects/sdk"
      fromBranch={fromBranch}
      onRun={onRun}
      onCancel={onCancel}
    />,
  )

  return { onRun, onCancel, dialog: within(screen.getByRole('dialog')) }
}

describe('TrustDialog', () => {
  it('asks once more after a custom target’s magefiles changed, showing the command once', async () => {
    const { onRun, dialog } = renderDialog(
      scriptQuestion({
        ref: 'magex:deploy:docs',
        name: 'deploy:docs',
        source: 'magex',
        command: 'magex deploy:docs',
        trust: 'changed',
        trusted: 'magex deploy:docs',
        trustNote: 'acme-org/sdk’s magefiles changed since you last ran it.',
      }),
    )

    expect(screen.getByRole('dialog', { name: 'Run “deploy:docs” from acme-org/sdk’s magefiles?' })).toBeInTheDocument()
    expect(dialog.getByRole('heading', { name: 'Changed since you last ran it' })).toBeInTheDocument()
    expect(dialog.getAllByText('magex deploy:docs')).toHaveLength(1)
    expect(dialog.getByText('acme-org/sdk’s magefiles changed since you last ran it.')).toBeInTheDocument()
    expect(dialog.queryByRole('term')).not.toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Run' }))
    expect(onRun).toHaveBeenCalledExactlyOnceWith(true)
  })

  it('says a clone on another branch can change what the files say', () => {
    const { dialog } = renderDialog(scriptQuestion(), 'feature-x')

    expect(
      dialog.getByText((_, el) => el?.tagName === 'SPAN' && el.textContent.startsWith('From branch feature-x,')),
    ).toHaveTextContent('From branch feature-x, which can change what the repository’s files say.')
  })

  it('shows a changed command that was never trusted with text as a new one', () => {
    const { dialog } = renderDialog(scriptQuestion({ trust: 'changed', trusted: '' }))

    expect(dialog.getByRole('heading', { name: 'Changed since you last ran it' })).toBeInTheDocument()
    expect(dialog.getByText('This command comes from the repository, not from you:')).toBeInTheDocument()
  })

  it('cancels on Escape', async () => {
    const { onCancel, onRun } = renderDialog(scriptQuestion())

    await userEvent.keyboard('{Escape}')

    expect(onCancel).toHaveBeenCalledOnce()
    expect(onRun).not.toHaveBeenCalled()
  })
})
