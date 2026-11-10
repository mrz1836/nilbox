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
})
