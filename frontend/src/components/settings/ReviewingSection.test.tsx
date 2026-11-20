import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ReviewingSection } from './ReviewingSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderSection(current: Settings = settings(), onManageReplies?: () => void) {
  const onSaved = vi.fn()

  render(<ReviewingSection settings={current} onSaved={onSaved} onManageReplies={onManageReplies} />)

  return { onSaved }
}

function nudgeAfter() {
  return within(screen.getByRole('radiogroup', { name: 'Offer a nudge after' }))
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
      'Reply templates for approvals, requests for changes, comments, and nudges live in Prompts → Review replies.',
    )
  })

  it('offers each quiet time before a nudge, with the stored one chosen', () => {
    renderSection(settings({ nudgeAfterHours: 48 }))

    expect(
      nudgeAfter()
        .getAllByRole('radio')
        .map((r) => r.closest('label')?.textContent),
    ).toEqual(['Off', '12 hours', '1 day', '2 days', '3 days', '1 week'])
    expect(nudgeAfter().getByRole('radio', { name: '2 days' })).toBeChecked()
  })

  it('offers a stored quiet time that is none of the usual ones, in its place', () => {
    renderSection(settings({ nudgeAfterHours: 36 }))

    expect(
      nudgeAfter()
        .getAllByRole('radio')
        .map((r) => r.closest('label')?.textContent),
    ).toEqual(['Off', '12 hours', '1 day', '36 hours', '2 days', '3 days', '1 week'])
    expect(nudgeAfter().getByRole('radio', { name: '36 hours' })).toBeChecked()
  })

  it('saves a quiet time as soon as it is chosen, Off among them', async () => {
    const { onSaved } = renderSection()

    expect(nudgeAfter().getByRole('radio', { name: '1 day' })).toBeChecked()

    await userEvent.click(nudgeAfter().getByRole('radio', { name: 'Off' }))

    expect(platform.setNudgeAfterHours).toHaveBeenCalledWith(0)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ nudgeAfterHours: 0 }))
    })
  })

  it('shows a quiet time the backend refused, and goes back to the stored one', async () => {
    platform.setNudgeAfterHours.mockRejectedValue(new Error('unknown time before a nudge'))
    const { onSaved } = renderSection()

    await userEvent.click(nudgeAfter().getByRole('radio', { name: '1 week' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('unknown time before a nudge')
    expect(screen.getByRole('radiogroup', { name: 'Offer a nudge after' })).toHaveAccessibleDescription(
      'unknown time before a nudge',
    )
    await waitFor(() => {
      expect(nudgeAfter().getByRole('radio', { name: '1 day' })).toBeChecked()
    })
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('saves whether a nudge snoozes, naming the snooze length', async () => {
    const { onSaved } = renderSection(settings({ snoozeDays: 3 }))
    const snooze = screen.getByRole('switch', { name: 'Snooze after a nudge' })

    expect(snooze).not.toBeChecked()
    expect(snooze).toHaveAccessibleDescription(
      'Start each nudge with “Then snooze for 3 days” ticked. The pull request comes back early when its author pushes.',
    )

    await userEvent.click(snooze)

    expect(platform.setNudgeSnooze).toHaveBeenCalledWith(true)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ snoozeDays: 3, nudgeSnooze: true }))
    })
  })

  it('points at the Nudge reply, and opens the replies from there', async () => {
    const onManageReplies = vi.fn()
    renderSection(settings(), onManageReplies)

    expect(screen.getByText(/The comment starts from your/)).toHaveTextContent(
      'The comment starts from your Nudge reply in Prompts › Review replies.',
    )

    await userEvent.click(screen.getByRole('button', { name: 'Prompts › Review replies' }))

    expect(onManageReplies).toHaveBeenCalledOnce()
  })

  it('names where the Nudge reply is kept without a way there', () => {
    renderSection()

    expect(screen.queryByRole('button', { name: 'Prompts › Review replies' })).not.toBeInTheDocument()
    expect(screen.getByText(/The comment starts from your/)).toHaveTextContent(
      'The comment starts from your Nudge reply in Prompts › Review replies.',
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
    // The change shows at once and goes back as its save ends, which can be a render
    // after the refusal shows.
    await waitFor(() => {
      expect(methods().getByRole('radio', { name: 'As each repository offers first' })).toBeChecked()
    })
    expect(onSaved).not.toHaveBeenCalled()
  })

  it('saves whether your own pull requests merge past the rules, off at first', async () => {
    const { onSaved } = renderSection()
    const bypass = screen.getByRole('switch', { name: 'Merge your own pull requests past the rules' })

    expect(bypass).not.toBeChecked()
    expect(bypass).toHaveAccessibleDescription(
      'When an approval from someone else is all your pull request lacks, and GitHub lets you merge past the branch’s rules, the review sheet’s button does that instead of Merge when ready. You still confirm each rule it skips.',
    )

    await userEvent.click(bypass)

    expect(platform.setBypassOwn).toHaveBeenCalledWith(true)
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ bypassOwn: true }))
    })
  })

  it('shows a change of merging past the rules the backend refused, and goes back to the stored one', async () => {
    platform.setBypassOwn.mockRejectedValue(new Error('the settings could not be saved'))
    const { onSaved } = renderSection()
    const bypass = screen.getByRole('switch', { name: 'Merge your own pull requests past the rules' })

    await userEvent.click(bypass)

    expect(await screen.findByRole('alert')).toHaveTextContent('the settings could not be saved')
    await waitFor(() => {
      expect(bypass).not.toBeChecked()
    })
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

  it('says how many people the app learned, and forgets them all once the user confirms', async () => {
    renderSection()

    const people = within(screen.getByRole('region', { name: 'People' }))
    expect(await people.findByText('12 people across 3 repositories.')).toBeInTheDocument()

    await userEvent.click(people.getByRole('button', { name: 'Forget the people the app learned' }))

    const confirm = within(await screen.findByRole('dialog', { name: 'Forget the people the app learned?' }))
    await userEvent.click(confirm.getByRole('button', { name: 'Cancel' }))
    expect(platform.forgetPeople, 'cancelled').not.toHaveBeenCalled()

    await userEvent.click(people.getByRole('button', { name: 'Forget the people the app learned' }))
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: 'Forget the people the app learned?' })).getByRole('button', {
        name: 'Forget',
      }),
    )

    expect(platform.forgetPeople).toHaveBeenCalledOnce()
    expect(await people.findByText(/Nothing yet/)).toBeInTheDocument()
    expect(
      people.getByRole('button', { name: 'Forget the people the app learned' }),
      'nothing to forget',
    ).toBeDisabled()
  })

  it('says why the people couldn’t be forgotten', async () => {
    platform.forgetPeople.mockRejectedValue(new Error('The database is busy.'))
    renderSection()

    const people = within(screen.getByRole('region', { name: 'People' }))
    await userEvent.click(await people.findByRole('button', { name: 'Forget the people the app learned' }))
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: 'Forget the people the app learned?' })).getByRole('button', {
        name: 'Forget',
      }),
    )

    expect(await people.findByText('The database is busy.')).toBeInTheDocument()
    expect(people.getByText('12 people across 3 repositories.'), 'what was learned stays').toBeInTheDocument()
  })
})
