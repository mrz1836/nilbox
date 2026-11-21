import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { checkResult, item } from '../test/fixtures'
import { platform } from '../test/platform'
import { PromptPreview, type PromptPreviewProps } from './PromptPreview'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function props(overrides: Partial<PromptPreviewProps> = {}): PromptPreviewProps {
  return {
    items: [item(), item({ id: 2, number: 8, repo: 'acme-org/api', title: 'Speed up the cache' })],
    itemId: 1,
    onItemChange: vi.fn(),
    inWorktree: true,
    onInWorktreeChange: vi.fn(),
    atPrHead: false,
    onAtPrHeadChange: vi.fn(),
    kind: 'pr',
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

    rerender(<PromptPreview {...props({ kind: 'issue' })} />)
    expect(screen.queryByRole('checkbox', { name: "At the PR's head" })).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'A sample issue' })).toBeInTheDocument()
  })

  it('fills a prompt for repositories in for the sample repository, in a worktree or not', async () => {
    const onInWorktreeChange = vi.fn()
    render(<PromptPreview {...props({ items: [], itemId: 0, kind: 'repo', onInWorktreeChange })} />)

    const preview = screen.getByRole('combobox', { name: 'Preview with' })
    expect(preview).toHaveValue('0')
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['A sample repository'])
    expect(screen.queryByRole('checkbox', { name: "At the PR's head" })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('checkbox', { name: 'In a worktree' }))
    expect(onInWorktreeChange).toHaveBeenCalledWith(false)
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

  it('copies the filled-in prompt, and says so', async () => {
    render(<PromptPreview {...props()} />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy the filled-in prompt' }))

    expect(platform.copyText).toHaveBeenCalledWith(checkResult().preview)
    expect(screen.getByRole('button', { name: 'Copy the filled-in prompt' })).toHaveTextContent('Copied')
    expect(screen.getByRole('status')).toHaveTextContent('Copied to the clipboard')
  })

  it('copies the notice with the prompt, as a launch sends them', async () => {
    const notice = 'This pull request comes from an outside contributor.'
    render(<PromptPreview {...props({ result: checkResult({ notice }) })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy the filled-in prompt' }))

    expect(platform.copyText).toHaveBeenCalledWith(`${checkResult().preview}\n${notice}\n`)
  })

  it('says when the clipboard refuses the text', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.copyText.mockRejectedValueOnce(new Error('no clipboard'))
    render(<PromptPreview {...props({ reply: true })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy the filled-in reply' }))

    expect(screen.getByRole('button', { name: 'Copy the filled-in reply' })).toHaveTextContent('Copy failed')
    expect(screen.getByRole('status')).toHaveTextContent('Select the text and copy it by hand')
  })

  it('offers no copy without a preview', () => {
    const { rerender } = render(<PromptPreview {...props({ result: null })} />)
    expect(screen.queryByRole('button', { name: /Copy/ })).not.toBeInTheDocument()

    rerender(<PromptPreview {...props({ hasErrors: true })} />)
    expect(screen.queryByRole('button', { name: /Copy/ })).not.toBeInTheDocument()

    rerender(
      <PromptPreview {...props({ result: checkResult({ previewError: 'For this item the prompt is empty.' }) })} />,
    )
    expect(screen.queryByRole('button', { name: /Copy/ })).not.toBeInTheDocument()
  })

  it('shows the filled-in text in a region the keyboard can scroll', () => {
    render(<PromptPreview {...props()} />)

    expect(screen.getByRole('region', { name: 'Filled-in prompt' })).toHaveAttribute('tabindex', '0')
  })
})
