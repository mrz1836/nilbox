import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ReviewingSection } from './ReviewingSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderSection(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<ReviewingSection settings={current} onSaved={onSaved} />)

  return { onSaved }
}

function methods() {
  return within(screen.getByRole('radiogroup', { name: 'Merge method' }))
}

function ignoredList() {
  return within(screen.getByRole('list', { name: 'Ignored workflows' }))
}

describe('ReviewingSection', () => {
  it('offers each merge method, with the stored one chosen', () => {
    renderSection(settings({ mergeMethod: 'rebase_merge' }))

    expect(
      methods()
        .getAllByRole('radio')
        .map((r) => r.getAttribute('value')),
    ).toEqual(['repo', 'squash', 'merge_commit', 'rebase_merge'])
    expect(methods().getByRole('radio', { name: 'Rebase and merge' })).toBeChecked()
    expect(methods().getByRole('radio', { name: 'As each repository offers first' })).toHaveAccessibleDescription(
      'The method GitHub’s merge button offers you first in that repository.',
    )
    expect(screen.getByRole('radiogroup', { name: 'Merge method' })).toHaveAccessibleDescription(
      'A repository that doesn’t allow the method you choose merges the way it offers first.',
    )
  })

  it('says where the review replies are kept', () => {
    renderSection()

    expect(screen.getByRole('heading', { name: 'Review replies' }).parentElement).toHaveTextContent(
      'Reply templates for approvals, requests for changes, and comments live in Prompts → Review replies.',
    )
  })

  it('saves a merge method as soon as it is chosen', async () => {
    const { onSaved } = renderSection()

    expect(methods().getByRole('radio', { name: 'As each repository offers first' })).toBeChecked()

    await userEvent.click(methods().getByRole('radio', { name: 'Squash and merge' }))

    expect(platform.setMergeMethod).toHaveBeenCalledWith('squash')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ mergeMethod: 'squash' }))
    })
  })

  it('shows a merge method the backend refused, and goes back to the stored one', async () => {
    platform.setMergeMethod.mockRejectedValue(new Error('unknown merge method'))
    const { onSaved } = renderSection()

    await userEvent.click(methods().getByRole('radio', { name: 'Create a merge commit' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown merge method')
    expect(methods().getByRole('radio', { name: 'Create a merge commit' })).toHaveAccessibleDescription(
      'Adds every commit to the base branch, joined by a merge commit. unknown merge method',
    )
    expect(methods().getByRole('radio', { name: 'As each repository offers first' })).toBeChecked()
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('lists the ignored workflows, each with its remove button', () => {
    renderSection(settings({ ignoredWorkflows: ['Auto-merge Trigger', 'Deploy Preview'] }))

    expect(screen.getByRole('heading', { name: 'Ignored workflows' }).parentElement).toHaveTextContent(
      'Workflows whose runs, held for approval, never count toward where a pull request stands, even when GitHub ' +
        'reports the merge blocked: its card reads its checks as they are, and the review sheet names the runs ' +
        'quietly under its checks.',
    )
    expect(
      ignoredList()
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Auto-merge Trigger', 'Deploy Preview'])
    expect(ignoredList().getByRole('button', { name: 'Count Auto-merge Trigger again' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Workflow to ignore' })).toHaveAccessibleDescription(
      'A name matches a workflow’s whole name, case and all.',
    )
  })

  it('says what counts while no workflow is ignored', () => {
    renderSection()

    expect(screen.queryByRole('list', { name: 'Ignored workflows' })).not.toBeInTheDocument()
    expect(
      screen.getByText(
        'No workflow is ignored. A run GitHub approves only on its site doesn’t count unless GitHub reports the ' +
          'merge blocked; ignore a workflow from a held run in the review sheet, or here.',
      ),
    ).toBeInTheDocument()
  })

  it('counts a workflow again', async () => {
    const { onSaved } = renderSection(settings({ ignoredWorkflows: ['Auto-merge Trigger', 'Deploy Preview'] }))

    await userEvent.click(ignoredList().getByRole('button', { name: 'Count Auto-merge Trigger again' }))

    expect(platform.setIgnoredWorkflows).toHaveBeenCalledWith(['Deploy Preview'])
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ ignoredWorkflows: ['Deploy Preview'] }))
    })
  })

  it('ignores another workflow, and says when it already is, case and all', async () => {
    const { onSaved } = renderSection(settings({ ignoredWorkflows: ['Deploy Preview'] }))
    const input = screen.getByRole('textbox', { name: 'Workflow to ignore' })

    await userEvent.type(input, 'Deploy Preview')
    await userEvent.click(screen.getByRole('button', { name: 'Ignore workflow' }))

    expect(screen.getByRole('alert')).toHaveTextContent('Deploy Preview is already ignored.')
    expect(platform.setIgnoredWorkflows).not.toHaveBeenCalled()

    await userEvent.clear(input)
    await userEvent.type(input, ' deploy preview ')
    await userEvent.click(screen.getByRole('button', { name: 'Ignore workflow' }))

    expect(platform.setIgnoredWorkflows).toHaveBeenCalledWith(['Deploy Preview', 'deploy preview'])
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ ignoredWorkflows: ['Deploy Preview', 'deploy preview'] }))
    })
    expect(input).toHaveValue('')
  })

  it('shows a list the backend refused', async () => {
    platform.setIgnoredWorkflows.mockRejectedValue(new Error('an ignored workflow’s name is too long'))
    const { onSaved } = renderSection()

    await userEvent.type(screen.getByRole('textbox', { name: 'Workflow to ignore' }), 'x'.repeat(201))
    await userEvent.click(screen.getByRole('button', { name: 'Ignore workflow' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('too long')
    expect(screen.queryByRole('list', { name: 'Ignored workflows' })).not.toBeInTheDocument()
    expect(onSaved).not.toHaveBeenCalled()
  })
})
