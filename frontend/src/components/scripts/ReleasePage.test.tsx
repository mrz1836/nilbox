import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { SettingsSection } from '../../lib/settingsIndex'
import { useReleaseCandidates } from '../../lib/useReleaseCandidates'
import type { IntegrationTool, ReleaseCandidate, ReleaseCandidates, ScriptRun } from '../../platform'
import { at } from '../../test/clock'
import {
  integrationTool,
  releaseCandidate,
  releaseCandidates,
  releaseRun,
  scriptRun,
  toolCopy,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import { ReleasePage } from './ReleasePage'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

interface PageProps {
  tool: IntegrationTool | undefined
  runs?: readonly ScriptRun[] | null
  onRelease: (candidate: ReleaseCandidate) => void
  onOpenRun: (id: number) => void
  onOpenSettings: (section: SettingsSection) => void
}

// Page is the page as the tab shows it, reading the list itself.
function Page({ runs = [], ...props }: PageProps) {
  return <ReleasePage candidates={useReleaseCandidates(true)} runs={runs} cloneRoot="~/projects" {...props} />
}

function renderPage(props: Partial<Omit<PageProps, 'onRelease' | 'onOpenRun' | 'onOpenSettings'>> = {}) {
  const handlers = {
    onRelease: vi.fn<(candidate: ReleaseCandidate) => void>(),
    onOpenRun: vi.fn<(id: number) => void>(),
    onOpenSettings: vi.fn<(section: SettingsSection) => void>(),
  }

  render(<Page tool={integrationTool()} {...handlers} {...props} />, { wrapper: at() })

  return handlers
}

// group is a group's heading button, by its title.
function group(title: RegExp) {
  return screen.getByRole('button', { name: title })
}

// heldList makes the next read of the list wait until the test answers it, as a read of
// GitHub does, and returns the answer.
function heldList(): (list: ReleaseCandidates) => void {
  let answer: (list: ReleaseCandidates) => void = () => undefined

  platform.listReleaseCandidates.mockImplementationOnce(
    () =>
      new Promise<ReleaseCandidates>((resolve) => {
        answer = resolve
      }),
  )

  return (list) => {
    answer(list)
  }
}

describe('ReleasePage', () => {
  it('lists the unreleased repositories open, and the other groups collapsed with their counts', async () => {
    const { onRelease } = renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('Listing your repositories…')

    const unreleased = await screen.findByRole('list', { name: 'Unreleased changes' })
    const row = within(unreleased).getByRole('listitem')

    expect(row).toHaveTextContent('acme-org/sdk')
    expect(row).toHaveTextContent('v1.4.2')
    expect(row).toHaveTextContent('+4')
    expect(row).toHaveTextContent('checks passed')
    expect(row).toHaveTextContent('12m ago')
    expect(row).toHaveTextContent('Fix retries on 502 (#45)')
    expect(row).toHaveTextContent('Your clone: on feature-x')
    expect(group(/^Unreleased changes · 1/)).toHaveAttribute('aria-expanded', 'true')
    expect(group(/^Up to date · 1/)).toHaveAttribute('aria-expanded', 'false')
    expect(group(/^No releases yet · 1/)).toHaveAttribute('aria-expanded', 'false')
    expect(group(/^Can’t release here · 1/)).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByText(/magex 1\.29\.5/)).toBeInTheDocument()
    expect(screen.getByText('5m ago')).toHaveAttribute('dateTime', '2026-06-15T11:55:00Z')

    await userEvent.click(within(row).getByRole('button', { name: 'Release acme-org/sdk…' }))
    expect(onRelease).toHaveBeenCalledWith(releaseCandidate())
  })

  it('opens a collapsed group: Release… where there is something to release, and why not where there isn’t', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Unreleased changes' })
    await userEvent.click(group(/^No releases yet/))
    await userEvent.click(group(/^Can’t release here/))
    await userEvent.click(group(/^Up to date/))

    expect(
      within(screen.getByRole('list', { name: 'No releases yet' })).getByRole('button', {
        name: 'Release acme-org/tools…',
      }),
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('list', { name: 'No releases yet' })).getByText('no version yet'),
    ).toBeInTheDocument()

    const cant = within(screen.getByRole('list', { name: 'Can’t release here' }))

    expect(cant.getByText('Archived on GitHub')).toBeInTheDocument()
    expect(cant.queryByRole('button')).not.toBeInTheDocument()
    expect(within(screen.getByRole('list', { name: 'Up to date' })).queryByRole('button')).not.toBeInTheDocument()

    await userEvent.click(group(/^Up to date/))
    expect(screen.queryByRole('list', { name: 'Up to date' })).not.toBeInTheDocument()
  })

  it('searches every group, open while it searches', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Unreleased changes' })
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'OLD')

    expect(screen.getByRole('list', { name: 'Can’t release here' })).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Unreleased changes' })).not.toBeInTheDocument()
    expect(group(/^Can’t release here · 1/)).toBeDisabled()

    await userEvent.clear(screen.getByRole('searchbox', { name: 'Search repositories' }))
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'nothing-like-it')
    expect(screen.getByText('No repository matching “nothing-like-it”.')).toBeInTheDocument()
  })

  it('says when nothing has unreleased changes, and counts the clones it can’t list', async () => {
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate({ group: 'upToDate', clone: null })], withoutGitHub: 12 }),
    )
    renderPage()

    expect(await screen.findByText('No repository has unreleased changes.')).toBeInTheDocument()
    expect(screen.getByText('12 clones without a GitHub origin aren’t listed.')).toBeInTheDocument()
  })

  it('counts one clone it can’t list', async () => {
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate()], withoutGitHub: 1 }),
    )
    renderPage()

    expect(await screen.findByText('1 clone without a GitHub origin isn’t listed.')).toBeInTheDocument()
  })

  it('offers to change the clone root while there are no clones', async () => {
    platform.listReleaseCandidates.mockResolvedValue(releaseCandidates({ repos: [], withoutGitHub: 0 }))
    const { onOpenSettings } = renderPage()

    expect(
      await screen.findByText('No clones under ~/projects yet. Release lists the repositories you have cloned there.'),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Change the clone root' }))
    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('repositories')
  })

  it('keeps the last list when GitHub can’t be reached, saying how old it is', async () => {
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ checkedAt: '2026-06-15T11:46:00Z', error: 'Couldn’t reach GitHub.' }),
    )
    renderPage()

    const note = await screen.findByRole('note')

    expect(note).toHaveTextContent('Couldn’t reach GitHub · 14m old')
    expect(note).toHaveAttribute('title', 'Couldn’t reach GitHub.')
    expect(screen.getByRole('list', { name: 'Unreleased changes' })).toBeInTheDocument()
  })

  it('says how far the first read has got in place of the bare spinner', async () => {
    const answer = heldList()

    renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('Listing your repositories…')

    act(() => {
      emit('releases:progress', { phase: 'clones', done: 0, total: 0 })
    })
    expect(screen.getByRole('status')).toHaveTextContent('Finding your clones…')

    act(() => {
      emit('releases:progress', { phase: 'github', done: 45, total: 151 })
    })
    expect(screen.getByRole('status')).toHaveTextContent('Asking GitHub: 45 of 151 repositories…')

    act(() => {
      emit('releases:progress', { phase: 'compare', done: 1, total: 1 })
    })
    expect(screen.getByRole('status')).toHaveTextContent('Asking GitHub what changed: 1 of 1 repository…')

    act(() => {
      emit('releases:progress', { phase: 'checking', done: 3, total: 12 })
    })
    expect(screen.getByRole('status')).toHaveTextContent('Checking your clones: 3 of 12…')

    act(() => {
      answer(releaseCandidates())
    })

    expect(await screen.findByRole('list', { name: 'Unreleased changes' })).toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('shows the list kept from the last start at once, with its age, while GitHub is read again', async () => {
    platform.listReleaseCandidates.mockResolvedValueOnce(
      releaseCandidates({ kept: true, refreshing: true, checkedAt: '2026-06-15T10:00:00Z' }),
    )
    renderPage()

    expect(await screen.findByRole('list', { name: 'Unreleased changes' })).toBeInTheDocument()

    const status = screen.getByRole('status')

    expect(status).toHaveTextContent('From 2h ago · checking again…')
    expect(screen.queryByText(/Read 2h ago/)).not.toBeInTheDocument()

    act(() => {
      emit('releases:progress', { phase: 'github', done: 45, total: 151 })
    })
    expect(status).toHaveTextContent('From 2h ago · checking again… Asking GitHub: 45 of 151 repositories…')

    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate({ repo: 'acme-org/cli', dir: '~/projects/cli' })] }),
    )
    act(() => {
      emit('releases:updated')
    })

    expect(await screen.findByRole('button', { name: 'Release acme-org/cli…' })).toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByText('5m ago')).toHaveAttribute('dateTime', '2026-06-15T11:55:00Z')
  })

  it('says how old a kept list is when GitHub couldn’t be read again', async () => {
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ kept: true, checkedAt: '2026-06-15T10:00:00Z', error: 'Couldn’t reach GitHub.' }),
    )
    renderPage()

    expect(await screen.findByRole('note')).toHaveTextContent('Couldn’t reach GitHub · 2h old')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('says how far a read it asked for has got, over the list it has', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Unreleased changes' })

    const answer = heldList()

    await userEvent.click(screen.getByRole('button', { name: 'Read GitHub again' }))
    act(() => {
      emit('releases:progress', { phase: 'github', done: 15, total: 151 })
    })

    expect(screen.getByRole('status')).toHaveTextContent('Asking GitHub: 15 of 151 repositories…')
    expect(screen.getByRole('list', { name: 'Unreleased changes' })).toBeInTheDocument()

    act(() => {
      answer(releaseCandidates())
    })

    expect(await screen.findByRole('button', { name: 'Read GitHub again' })).toBeEnabled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('says why the list couldn’t be read at all', async () => {
    platform.listReleaseCandidates.mockRejectedValue(new Error('not signed in to GitHub'))
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Couldn’t list the repositories: not signed in to GitHub',
    )
  })

  it('reads GitHub again on request, and says why when that fails', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Unreleased changes' })
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate({ repo: 'acme-org/cli', dir: '~/projects/cli' })] }),
    )
    await userEvent.click(screen.getByRole('button', { name: 'Read GitHub again' }))

    expect(platform.listReleaseCandidates).toHaveBeenLastCalledWith(true)
    expect(await screen.findByRole('button', { name: 'Release acme-org/cli…' })).toBeInTheDocument()

    platform.listReleaseCandidates.mockRejectedValue(new Error('GitHub timed out'))
    await userEvent.click(screen.getByRole('button', { name: 'Read GitHub again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t read GitHub again: GitHub timed out')
    expect(screen.getByRole('button', { name: 'Release acme-org/cli…' })).toBeInTheDocument()
  })

  it('reads the list again when the shell says a new one landed', async () => {
    renderPage()

    await screen.findByRole('list', { name: 'Unreleased changes' })
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate({ repo: 'acme-org/cli', dir: '~/projects/cli' })] }),
    )
    act(() => {
      emit('releases:updated')
    })

    expect(await screen.findByRole('button', { name: 'Release acme-org/cli…' })).toBeInTheDocument()
    expect(platform.listReleaseCandidates).toHaveBeenLastCalledWith(false)
  })

  it('marks the checks on each newest commit with an icon and words', async () => {
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({
        repos: [
          releaseCandidate({ repo: 'acme-org/a', checks: 'PENDING', clone: null }),
          releaseCandidate({ repo: 'acme-org/b', checks: 'FAILURE', headAt: '', headline: '' }),
        ],
      }),
    )
    renderPage()

    const rows = within(await screen.findByRole('list', { name: 'Unreleased changes' })).getAllByRole('listitem')

    expect(rows[0]).toHaveTextContent('checks running')
    expect(rows[0]).not.toHaveTextContent('Your clone')
    expect(rows[1]).toHaveTextContent('checks failing')
  })

  it.each<[string, Partial<IntegrationTool> | undefined, string, string]>([
    ['missing', { state: 'missing' }, 'magex isn’t on this Mac.', 'Install mage-x in Settings'],
    [
      'too old',
      { state: 'tooOld', minVersion: '1.29.5', copy: toolCopy({ version: { raw: '1.27.0', commit: '', built: '' } }) },
      'magex 1.27.0 is older than 1.29.5, which Release needs.',
      'Update mage-x in Settings',
    ],
    [
      'won’t run',
      { state: 'wontRun', copy: toolCopy({ error: 'bad CPU type' }) },
      '~/.local/bin/magex is there but won’t run: bad CPU type',
      'Open Settings',
    ],
    ['won’t run, silently', { state: 'wontRun' }, '~/.local/bin/magex is there but won’t run.', 'Open Settings'],
    ['not listed yet', undefined, 'magex can’t be used yet.', 'Open Settings'],
  ])('says why magex can’t be used when it is %s, with the fix', async (_, change, text, fix) => {
    const { onOpenSettings } = renderPage({ tool: change === undefined ? undefined : integrationTool(change) })

    expect(screen.getByText(text)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Unreleased changes' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Read GitHub again' })).toBeDisabled()

    await userEvent.click(screen.getByRole('button', { name: fix }))
    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('integrations')
  })

  it('lists the recent releases, each opening its run', async () => {
    const { onOpenRun } = renderPage({
      runs: [
        scriptRun(),
        releaseRun({ id: 8, status: 'running', result: { ...releaseRun().result, outcome: null } }),
        releaseRun(),
        releaseRun({ id: 6, title: 'Release v1.4.2', result: { ...releaseRun().result, intent: null } }),
      ],
    })

    const recent = within(await screen.findByRole('list', { name: 'Recent releases' }))
    const rows = recent.getAllByRole('button')

    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining('v1.4.3acme-org/sdkrunning'),
      expect.stringContaining('v1.4.3acme-org/sdkon GitHub'),
      expect.stringContaining('Release v1.4.2acme-org/sdkon GitHub'),
    ])

    await userEvent.click(rows[1] as HTMLElement)
    expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(7)
  })
})
