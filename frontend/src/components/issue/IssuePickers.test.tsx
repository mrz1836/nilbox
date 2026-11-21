import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { deferred } from '../../test/async'
import { issue, repoLabels } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { IssueSummary } from './IssueSummary'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// picker opens the picker its button names, and returns its panel.
async function picker(button: string, title: string) {
  await userEvent.click(screen.getByRole('button', { name: button }))

  return within(await screen.findByRole('dialog', { name: title }))
}

describe('LabelPicker', () => {
  it('checks what the issue has, sends only what changed, and closes once GitHub took it', async () => {
    const onEdit = vi.fn().mockResolvedValue('')
    render(<IssueSummary issue={issue()} onEdit={onEdit} />)

    const panel = await picker('Edit labels', 'Labels')
    expect(platform.issueLabels).toHaveBeenCalledWith(1)
    expect(await panel.findByRole('checkbox', { name: 'bug Something isn’t working' })).toBeChecked()
    expect(panel.getByRole('checkbox', { name: 'p1' })).not.toBeChecked()
    expect(panel.getByRole('button', { name: 'Apply' }), 'nothing changed yet').toBeDisabled()

    await userEvent.type(panel.getByRole('searchbox', { name: 'Filter labels' }), 'TRI')
    expect(panel.getAllByRole('checkbox')).toHaveLength(1)
    await userEvent.click(panel.getByRole('checkbox', { name: 'triage Needs a first look' }))

    await userEvent.clear(panel.getByRole('searchbox', { name: 'Filter labels' }))
    await userEvent.click(panel.getByRole('checkbox', { name: 'bug Something isn’t working' }))
    await userEvent.click(panel.getByRole('checkbox', { name: 'p1' }))
    await userEvent.click(panel.getByRole('checkbox', { name: 'p1' }))
    await userEvent.click(panel.getByRole('button', { name: 'Apply' }))

    expect(onEdit).toHaveBeenCalledExactlyOnceWith({ addLabels: ['triage'], removeLabels: ['bug'] })
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Labels' })).not.toBeInTheDocument()
    })
  })

  it('opens as a modal with focus in its field, and Escape closes it back to its button', async () => {
    render(<IssueSummary issue={issue()} onEdit={vi.fn()} />)

    await picker('Edit labels', 'Labels')
    const dialog = screen.getByRole('dialog', { name: 'Labels' })
    expect(dialog.tagName, 'a native modal, over the sheet’s column').toBe('DIALOG')
    expect(dialog).toHaveAttribute('open')
    expect(screen.getByRole('searchbox', { name: 'Filter labels' })).toHaveFocus()

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Labels' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit labels' })).toHaveFocus()
  })

  it('says why GitHub refused part of the change, and sends only the rest again', async () => {
    const onEdit = vi.fn().mockResolvedValueOnce('GitHub kept “bug” on it.').mockResolvedValueOnce('')
    const { rerender } = render(<IssueSummary issue={issue()} onEdit={onEdit} />)

    const panel = await picker('Edit labels', 'Labels')
    await userEvent.click(await panel.findByRole('checkbox', { name: 'p1' }))
    await userEvent.click(panel.getByRole('checkbox', { name: 'bug Something isn’t working' }))
    await userEvent.click(panel.getByRole('button', { name: 'Apply' }))

    expect(onEdit).toHaveBeenLastCalledWith({ addLabels: ['p1'], removeLabels: ['bug'] })
    expect(await panel.findByRole('alert')).toHaveTextContent('GitHub kept “bug” on it.')

    // The sheet reads the issue again: p1 went on, and the picker still wants it, and bug off.
    rerender(
      <IssueSummary
        issue={issue({
          labels: [
            { name: 'bug', color: '#d73a4a' },
            { name: 'p1', color: '#b60205' },
          ],
        })}
        onEdit={onEdit}
      />,
    )
    expect(panel.getByRole('checkbox', { name: 'p1' })).toBeChecked()
    expect(panel.getByRole('checkbox', { name: 'bug Something isn’t working' })).not.toBeChecked()

    await userEvent.click(panel.getByRole('button', { name: 'Apply' }))
    expect(onEdit).toHaveBeenLastCalledWith({ addLabels: [], removeLabels: ['bug'] })
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Labels' })).not.toBeInTheDocument()
    })
  })

  it('holds open while GitHub answers, and applies with ⌘↩', async () => {
    const answer = deferred<string>()
    const onEdit = vi.fn(() => answer.promise)
    render(<IssueSummary issue={issue()} onEdit={onEdit} />)

    const panel = await picker('Edit labels', 'Labels')
    await userEvent.click(await panel.findByRole('checkbox', { name: 'p1' }))
    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')

    expect(onEdit).toHaveBeenCalledOnce()
    expect(panel.getByRole('button', { name: 'Applying…' })).toBeDisabled()
    expect(panel.getByRole('button', { name: 'Cancel' })).toBeDisabled()

    await userEvent.keyboard('{Escape}')
    await userEvent.click(document.body)
    expect(screen.getByRole('dialog', { name: 'Labels' }), 'its outcome shows there').toBeInTheDocument()

    answer.resolve('GitHub is down')
    expect(await panel.findByRole('alert')).toHaveTextContent('GitHub is down')

    await userEvent.click(panel.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog', { name: 'Labels' })).not.toBeInTheDocument()
  })

  it('checks the label the field’s filter finds first with Enter, and moves with ↑ and ↓', async () => {
    const onEdit = vi.fn().mockResolvedValue('')
    render(<IssueSummary issue={issue()} onEdit={onEdit} />)

    const panel = await picker('Edit labels', 'Labels')
    const field = panel.getByRole('searchbox', { name: 'Filter labels' })
    await panel.findAllByRole('checkbox')

    await userEvent.type(field, 'tri{Enter}')
    expect(panel.getByRole('checkbox', { name: 'triage Needs a first look' })).toBeChecked()

    await userEvent.clear(field)
    await userEvent.keyboard('{ArrowDown}{Enter}')
    expect(
      panel.getByRole('checkbox', { name: 'bug Something isn’t working' }),
      'the first row, taken off',
    ).not.toBeChecked()

    await userEvent.keyboard('{ArrowUp}{ArrowDown}{Enter}{Meta>}{Enter}{/Meta}')
    expect(onEdit).toHaveBeenCalledExactlyOnceWith({ addLabels: ['triage', 'p1'], removeLabels: ['bug'] })
  })

  it('says how the labels are coming, and when there are more than it shows', async () => {
    const answer = deferred<ReturnType<typeof repoLabels>>()
    platform.issueLabels.mockReturnValueOnce(answer.promise)
    render(<IssueSummary issue={issue()} onEdit={vi.fn()} />)

    const panel = await picker('Edit labels', 'Labels')
    expect(panel.getByText('Reading them from GitHub…')).toBeInTheDocument()

    answer.resolve(repoLabels({ total: 5 }))
    expect(await panel.findByText('The first 3 labels, of 5.')).toBeInTheDocument()

    await userEvent.type(panel.getByRole('searchbox', { name: 'Filter labels' }), 'nothing')
    expect(panel.getByText('None of the first 3 labels matches, of 5.')).toBeInTheDocument()
  })

  it('says no label matches', async () => {
    render(<IssueSummary issue={issue()} onEdit={vi.fn()} />)

    const panel = await picker('Edit labels', 'Labels')
    await panel.findAllByRole('checkbox')
    await userEvent.type(panel.getByRole('searchbox', { name: 'Filter labels' }), 'nothing')
    expect(panel.getByText('No label matches.')).toBeInTheDocument()
  })

  it('says why the labels couldn’t be read', async () => {
    platform.issueLabels.mockRejectedValueOnce(new Error('GitHub is down'))
    render(<IssueSummary issue={issue()} onEdit={vi.fn()} />)

    const panel = await picker('Edit labels', 'Labels')
    expect(await panel.findByText('Couldn’t read them from GitHub: GitHub is down')).toBeInTheDocument()
  })
})

describe('the issue’s assignees', () => {
  it('edits them on the shared picker, from what the app learned', async () => {
    const onEdit = vi.fn().mockResolvedValue('')
    const reads = { candidates: platform.issueAssigneeCandidates, learn: vi.fn(), search: vi.fn() }
    render(<IssueSummary issue={issue()} assignees={{ reads, ownLock: 'Use Unassign me.', onEdit }} />)

    const panel = await picker('Edit assignees', 'Assignees')
    expect(platform.issueAssigneeCandidates).toHaveBeenCalledWith(1)
    const me = panel.getByRole('checkbox', { name: 'octo-user (you)' })
    expect(me).toBeChecked()
    expect(me, 'Unassign me does that').toBeDisabled()
    expect(me.closest('label')).toHaveAttribute('title', 'Use Unassign me.')

    await userEvent.click(await panel.findByRole('checkbox', { name: /^octo-other/ }))
    await userEvent.click(panel.getByRole('button', { name: 'Assign 1' }))

    expect(onEdit).toHaveBeenCalledExactlyOnceWith(['octo-other'], [])
  })
})

describe('IssueSummary edits', () => {
  it('offers no edits to a user who can’t make them, or with nothing to make them with', () => {
    const { rerender } = render(<IssueSummary issue={issue({ canEdit: false })} onEdit={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Edit labels' })).not.toBeInTheDocument()

    rerender(<IssueSummary issue={issue()} />)
    expect(screen.queryByRole('button', { name: 'Edit assignees' })).not.toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Assignees' })).toHaveTextContent('octo-user (you)')
  })
})
