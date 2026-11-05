import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { checkResult, item } from '../test/fixtures'
import { PromptPreview, type PromptPreviewProps } from './PromptPreview'

function props(overrides: Partial<PromptPreviewProps> = {}): PromptPreviewProps {
  return {
    items: [item(), item({ id: 2, number: 8, repo: 'acme-org/api', title: 'Speed up the cache' })],
    itemId: 1,
    onItemChange: vi.fn(),
    inWorktree: true,
    onInWorktreeChange: vi.fn(),
    atPrHead: false,
    onAtPrHeadChange: vi.fn(),
    forPRs: true,
    result: checkResult(),
    hasErrors: false,
    ...overrides,
  }
}

describe('PromptPreview', () => {
  it('shows the prompt filled in for the chosen item', () => {
    render(<PromptPreview {...props()} />)

    expect(screen.getByRole('combobox', { name: 'Preview with' })).toHaveValue('1')
    expect(screen.getByRole('option', { name: 'api #8 · Speed up the cache' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'A sample pull request' })).toBeInTheDocument()
    expect(screen.getByText(/Review pull request #7 in acme-org\/example-repo/)).toBeInTheDocument()
    expect(screen.getByText('66 characters')).toBeInTheDocument()
  })

  it('changes the item and the session switches', async () => {
    const onItemChange = vi.fn()
    const onInWorktreeChange = vi.fn()
    const onAtPrHeadChange = vi.fn()
    render(<PromptPreview {...props({ onItemChange, onInWorktreeChange, onAtPrHeadChange })} />)

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Preview with' }), '2')
    await userEvent.click(screen.getByRole('checkbox', { name: 'In a worktree' }))
    await userEvent.click(screen.getByRole('checkbox', { name: "At the PR's head" }))

    expect(onItemChange).toHaveBeenCalledWith(2)
    expect(onInWorktreeChange).toHaveBeenCalledWith(false)
    expect(onAtPrHeadChange).toHaveBeenCalledWith(true)
  })

  it('only offers the PR head in a worktree, and only for pull requests', () => {
    const { rerender } = render(<PromptPreview {...props({ inWorktree: false })} />)

    expect(screen.getByRole('checkbox', { name: "At the PR's head" })).toBeDisabled()

    rerender(<PromptPreview {...props({ forPRs: false })} />)
    expect(screen.queryByRole('checkbox', { name: "At the PR's head" })).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'A sample issue' })).toBeInTheDocument()
  })

  it('shows the outside contributor notice and a sample note', () => {
    render(
      <PromptPreview
        {...props({
          result: checkResult({
            notice: 'This pull request comes from an outside contributor.',
            previewNote: 'This prompt isn’t for issues, so the preview uses a sample.',
            sample: true,
          }),
        })}
      />,
    )

    expect(screen.getByText('Added for outside contributors')).toBeInTheDocument()
    expect(screen.getByText('This pull request comes from an outside contributor.')).toBeInTheDocument()
    expect(screen.getByText(/the preview uses a sample/)).toBeInTheDocument()
    expect(screen.getByText('66 characters · sample data')).toBeInTheDocument()
  })

  it('explains why there is nothing to preview', () => {
    const { rerender } = render(<PromptPreview {...props({ result: null })} />)
    expect(screen.getByText('Filling in the prompt…')).toBeInTheDocument()

    rerender(<PromptPreview {...props({ hasErrors: true })} />)
    expect(screen.getByText('Fix the problems in the prompt to see it filled in.')).toBeInTheDocument()

    rerender(
      <PromptPreview {...props({ result: checkResult({ previewError: 'For this item the prompt is empty.' }) })} />,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('For this item the prompt is empty.')
    expect(screen.queryByText(/characters/)).not.toBeInTheDocument()
  })
})
