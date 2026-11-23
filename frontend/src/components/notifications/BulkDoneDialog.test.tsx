import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { doneURL } from '../../lib/githubThreads'
import type { GitHubThread } from '../../platform'
import { renderAt } from '../../test/clock'
import { githubThread } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { BulkDoneDialog, type BulkDoneDialogProps } from './BulkDoneDialog'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// threadsIn are n notifications in acme-org/name, updated at, the first unread ones.
function threadsIn(name: string, n: number, at: string, unread = 0): GitHubThread[] {
  return Array.from({ length: n }, (_, i) =>
    githubThread({ id: name.length * 1000 + i, owner: 'acme-org', name, updatedAt: at, unread: i < unread }),
  )
}

// backlog is 412 old notifications in example-repo, 37 of them unread, 301 in other-repo,
// 60 in keep-me, and 5 from this morning in fresh-repo.
const backlog = [
  ...threadsIn('example-repo', 412, '2026-05-01T00:00:00Z', 37),
  ...threadsIn('other-repo', 301, '2026-06-05T00:00:00Z', 2),
  ...threadsIn('keep-me', 60, '2026-06-14T00:00:00Z'),
  ...threadsIn('fresh-repo', 5, '2026-06-15T09:00:00Z'),
]

// shown renders the dialog over threads, with its callbacks as mocks.
function shown(props: Partial<BulkDoneDialogProps> = {}) {
  const onConfirm = vi.fn()
  const onCancel = vi.fn()

  renderAt(
    <BulkDoneDialog
      threads={backlog}
      truncated={false}
      query=""
      perMinute={40}
      perHour={1200}
      busy={false}
      error=""
      onConfirm={onConfirm}
      onCancel={onCancel}
      {...props}
    />,
  )

  return { onConfirm, onCancel, dialog: screen.getByRole('dialog', { name: 'Mark notifications done on GitHub?' }) }
}

// rows returns the repositories the dialog lists, each "name: counts".
function rows(): string[] {
  return within(screen.getByRole('list', { name: 'Repositories' }))
    .getAllByRole('listitem')
    .map((li) => li.textContent)
}

// box is a repository's box.
function box(repo: string): HTMLInputElement {
  const row = within(screen.getByRole('list', { name: 'Repositories' }))
    .getAllByRole('listitem')
    .find((li) => li.textContent.startsWith(repo))

  if (row === undefined) {
    throw new Error(`no row for ${repo}`)
  }

  return within(row).getByRole('checkbox')
}

describe('BulkDoneDialog', () => {
  it('lists every repository, the noisiest first, each ticked, and says what follows', () => {
    const { dialog } = shown()

    expect(rows()).toEqual([
      'acme-org/example-repo412 notifications · 37 unread',
      'acme-org/other-repo301 notifications · 2 unread',
      'acme-org/keep-me60 notifications',
      'acme-org/fresh-repo5 notifications',
    ])
    expect(dialog).toHaveTextContent('778 notifications in 4 repositories · 778 ticked')
    expect(screen.getByRole('button', { name: 'Mark 778 done' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()

    expect(dialog).toHaveTextContent(
      'GitHub takes these one at a time, so this runs in the background: about 19 minutes. Keep working, or quit: ' +
        'it picks up where it left off. Anything updated after now stays in your inbox.',
    )
    expect(dialog).toHaveTextContent('There’s no undo here.')
    expect(dialog).not.toHaveTextContent('GitHub holds more than the newest 1,000')
  })

  it('recounts every repository by age, and remembers the box of one that left the list', async () => {
    const { dialog } = shown()

    await userEvent.click(box('acme-org/keep-me'))
    expect(dialog).toHaveTextContent('778 notifications in 4 repositories · 718 ticked')

    await userEvent.click(screen.getByRole('radio', { name: '1 week' }))
    expect(rows()).toEqual([
      'acme-org/example-repo412 notifications · 37 unread',
      'acme-org/other-repo301 notifications · 2 unread',
    ])
    expect(dialog).toHaveTextContent('713 notifications in 2 repositories · 713 ticked')
    expect(dialog).toHaveTextContent('Anything updated in the last week stays in your inbox.')

    await userEvent.click(screen.getByRole('radio', { name: 'Any age' }))
    expect(box('acme-org/keep-me')).not.toBeChecked()

    await userEvent.click(screen.getByRole('radio', { name: '3 months' }))
    expect(dialog).toHaveTextContent('None of your notifications are that old.')
    expect(screen.getByRole('button', { name: 'Mark 0 done' })).toBeDisabled()
  })

  it('marks every repository’s when all are ticked, and the ticked ones’ otherwise', async () => {
    const { onConfirm } = shown()

    await userEvent.click(screen.getByRole('radio', { name: '1 day' }))
    await userEvent.click(screen.getByRole('button', { name: 'Mark 773 done' }))
    expect(onConfirm).toHaveBeenLastCalledWith({ age: 'day', repos: [], all: true })

    await userEvent.click(box('acme-org/other-repo'))
    await userEvent.click(screen.getByRole('button', { name: 'Mark 472 done' }))
    expect(onConfirm).toHaveBeenLastCalledWith({
      age: 'day',
      repos: ['acme-org/example-repo', 'acme-org/keep-me'],
      all: false,
    })
  })

  it('selects none and all of what it lists', async () => {
    const { dialog } = shown()

    await userEvent.click(screen.getByRole('button', { name: 'Select none' }))
    expect(dialog).toHaveTextContent('· 0 ticked')
    expect(screen.getByRole('button', { name: 'Mark 0 done' })).toBeDisabled()

    await userEvent.click(screen.getByRole('button', { name: 'Select all' }))
    expect(dialog).toHaveTextContent('· 778 ticked')
  })

  it('ticks only the repositories the toolbar’s field names, as GitHub’s own per-repository mark does', () => {
    shown({ query: 'repo:other-repo flaky' })

    expect(box('acme-org/other-repo')).toBeChecked()
    expect(box('acme-org/example-repo')).not.toBeChecked()
    expect(screen.getByRole('button', { name: 'Mark 301 done' })).toBeEnabled()
  })

  it('says GitHub holds more than it shows, and opens GitHub’s Done view', async () => {
    const { dialog } = shown({ truncated: true })

    expect(dialog).toHaveTextContent(
      `GitHub holds more than the newest 1,000 that ${brand.name} shows. Older ones in the ticked repositories are ` +
        'marked done too, so the final count may be higher.',
    )

    await userEvent.click(screen.getByRole('button', { name: 'GitHub’s Done view' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(doneURL)
  })

  it('holds both buttons while it starts, and says why a start failed', () => {
    shown({ busy: true, error: 'Notifications are being marked done already.' })

    expect(screen.getByRole('button', { name: 'Mark 778 done' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Couldn’t start marking them done: Notifications are being marked done already.',
    )
  })

  it('cancels', async () => {
    const { onCancel, onConfirm } = shown()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalledOnce()
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
