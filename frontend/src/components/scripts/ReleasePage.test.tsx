import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { SettingsSection } from '../../lib/settingsIndex'
import type { OnSection } from '../../lib/useSections'
import { useReleaseCandidates, type ReleaseCandidatesState } from '../../lib/useReleaseCandidates'
import type {
  IntegrationTool,
  ReleaseCandidate,
  ReleaseCandidates,
  ReleaseCloneState,
  ReleaseSort,
  RepoRules,
  ScriptRun,
  Settings,
} from '../../platform'
import { at } from '../../test/clock'
import {
  integrationTool,
  releaseCandidate,
  releaseCandidates,
  releaseRun,
  scriptRun,
  settings,
  toolCopy,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import { ReleasePage, type ReleasePageProps } from './ReleasePage'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

interface PageProps {
  tool: IntegrationTool | undefined
  runs?: readonly ScriptRun[] | null
  where?: string
  onRelease: (candidate: ReleaseCandidate) => void
  onOpenRun: (id: number) => void
  onOpenSettings: (section: SettingsSection) => void
}

// Page is the page as the tab shows it, reading the list itself.
function Page({ runs = [], ...props }: PageProps) {
  return <ReleasePage candidates={useReleaseCandidates(true)} runs={runs} where="under ~/projects" {...props} />
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

// listed is the page's list as read, holding repos, with no read of GitHub under way.
function listed(repos: ReleaseCandidate[]): ReleaseCandidatesState {
  return {
    list: releaseCandidates({ repos }),
    error: '',
    refreshing: false,
    refreshError: '',
    progress: null,
    refresh: () => Promise.resolve(),
  }
}

// sortRows are four repositories with unreleased changes whose newest commits, commit
// counts, and names each order them differently, two of them named in capitals.
function sortRows(): ReleaseCandidate[] {
  return [
    releaseCandidate({ repo: 'acme-org/Web-kit', dir: '~/projects/web-kit', headAt: '2026-06-15T11:00:00Z', ahead: 1 }),
    releaseCandidate({ repo: 'acme-org/api', dir: '~/projects/api', headAt: '2026-06-14T09:00:00Z', ahead: 7 }),
    releaseCandidate({
      repo: 'acme-org/Sdk-kit',
      dir: '~/projects/sdk-kit',
      headAt: '2026-06-12T09:00:00Z',
      ahead: 12,
    }),
    releaseCandidate({ repo: 'acme-org/zeta', dir: '~/projects/zeta', headAt: '2026-06-10T09:00:00Z', ahead: 3 }),
  ]
}

// renderSorted shows the page over sortRows, with props on top, and returns onReleaseSort
// and a way to render the page again with the same props.
function renderSorted(props: Partial<ReleasePageProps> = {}) {
  const onReleaseSort = vi.fn<(sort: ReleaseSort) => void>()
  const page = () => (
    <ReleasePage
      candidates={listed(sortRows())}
      tool={integrationTool()}
      runs={[]}
      where="under ~/projects"
      onRelease={vi.fn()}
      onOpenRun={vi.fn()}
      onOpenSettings={vi.fn()}
      onReleaseSort={onReleaseSort}
      {...props}
    />
  )
  const view = render(page(), { wrapper: at() })

  return {
    onReleaseSort,
    rerender: () => {
      view.rerender(page())
    },
  }
}

// renderListed shows the page over repos as read, with props on top.
function renderListed(repos: ReleaseCandidate[], props: Partial<ReleasePageProps> = {}) {
  renderSorted({ candidates: listed(repos), ...props })
}

// cloneState is a clone on master with nothing in the way, as overrides change it.
function cloneState(overrides: Partial<ReleaseCloneState> = {}): ReleaseCloneState {
  return {
    branch: 'master',
    detached: false,
    dirty: 0,
    notPushed: 0,
    behind: false,
    localOnly: [],
    unreadable: false,
    ...overrides,
  }
}

// sorts is the page's choice of order.
function sorts() {
  return within(screen.getByRole('radiogroup', { name: 'Sort the repositories' }))
}

// unreleased is the text of each row in the Unreleased changes list, top to bottom.
function unreleased(): string[] {
  return within(screen.getByRole('list', { name: 'Unreleased changes' }))
    .getAllByRole('listitem')
    .map((row) => row.textContent)
}

// holding matches the rows unreleased reads when they hold the repositories named, in that
// order, each by its name without its owner.
function holding(...names: string[]): unknown[] {
  return names.map((name): unknown => expect.stringContaining(name))
}

describe('ReleasePage', () => {
  it('keeps each group open or folded as the user left it, across starts', async () => {
    const onSection = vi.fn<OnSection>()
    render(
      <ReleasePage
        candidates={{
          list: releaseCandidates(),
          error: '',
          refreshing: false,
          refreshError: '',
          progress: null,
          refresh: () => Promise.resolve(),
        }}
        tool={integrationTool()}
        runs={[]}
        where="under ~/projects"
        onRelease={vi.fn()}
        onOpenRun={vi.fn()}
        onOpenSettings={vi.fn()}
        sections={{ 'release.unreleased': false, 'release.upToDate': true }}
        onSection={onSection}
      />,
      { wrapper: at() },
    )

    expect(group(/^Unreleased changes · 1: 1 ready$/)).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('list', { name: 'Unreleased changes' })).not.toBeInTheDocument()
    expect(group(/^Up to date · 1/)).toHaveAttribute('aria-expanded', 'true')

    await userEvent.click(group(/^No releases yet · 1: 1 without checks$/))

    expect(onSection).toHaveBeenCalledExactlyOnceWith('release.noReleases', true)
    expect(group(/^No releases yet · 1: 1 without checks$/)).toHaveAttribute('aria-expanded', 'true')
  })

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
    expect(group(/^Unreleased changes · 1: 1 ready$/)).toHaveAttribute('aria-expanded', 'true')
    expect(group(/^Up to date · 1/)).toHaveAttribute('aria-expanded', 'false')
    expect(group(/^No releases yet · 1: 1 without checks$/)).toHaveAttribute('aria-expanded', 'false')
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

  it('lists the muted repositories folded apart, each with Unmute', async () => {
    const muted = releaseCandidate({
      repo: 'acme-org/quiet',
      dir: '~/projects/quiet',
      group: 'muted',
      version: '',
      ahead: 0,
      commits: [],
      headline: '',
      headAt: '',
      checks: '',
      clone: null,
    })
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [...releaseCandidates().repos, muted] }),
    )
    const stored = settings({ repoRules: { pinned: [], muted: [] } })
    platform.setRepoRule.mockResolvedValue(stored)
    const saved = vi.fn<(settings: Settings) => void>()
    const rules: RepoRules = { pinned: [], muted: ['acme-org'] }

    render(
      <ReleasePage
        candidates={{
          list: releaseCandidates({ repos: [releaseCandidate(), muted] }),
          error: '',
          refreshing: false,
          refreshError: '',
          progress: null,
          refresh: () => Promise.resolve(),
        }}
        tool={integrationTool()}
        runs={[]}
        where="under ~/projects"
        onRelease={vi.fn()}
        onOpenRun={vi.fn()}
        onOpenSettings={vi.fn()}
        repoRules={rules}
        onSettingsSaved={saved}
      />,
      { wrapper: at() },
    )

    expect(group(/^Muted · 1/)).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(group(/^Muted · 1/))

    const row = within(within(screen.getByRole('list', { name: 'Muted' })).getByRole('listitem'))
    expect(row.getByTitle('acme-org/quiet')).toHaveTextContent('acme-org/quiet')
    expect(row.getByText('~/projects/quiet')).toBeInTheDocument()
    expect(row.queryByRole('button', { name: /^Release/ })).not.toBeInTheDocument()

    await userEvent.click(row.getByRole('button', { name: 'Unmute acme-org' }))

    expect(platform.setRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org', '')
    expect(saved).toHaveBeenCalledWith(stored)
  })

  it('says a repository unmuted already is being read again, and why an unmute failed', async () => {
    const muted = releaseCandidate({ repo: 'acme-org/quiet', dir: '~/projects/quiet', group: 'muted', clone: null })
    platform.setRepoRule.mockRejectedValue(new Error('database is locked'))
    const candidates = {
      list: releaseCandidates({ repos: [muted, releaseCandidate({ repo: 'beta/x', dir: '~/x', group: 'muted' })] }),
      error: '',
      refreshing: false,
      refreshError: '',
      progress: null,
      refresh: () => Promise.resolve(),
    }

    render(
      <ReleasePage
        candidates={candidates}
        tool={integrationTool()}
        runs={[]}
        where="under ~/projects"
        onRelease={vi.fn()}
        onOpenRun={vi.fn()}
        onOpenSettings={vi.fn()}
        sections={{ 'release.muted': true }}
        repoRules={{ pinned: [], muted: ['beta/x'] }}
      />,
      { wrapper: at() },
    )

    const list = within(screen.getByRole('list', { name: 'Muted' }))
    expect(list.getByText('Unmuted · reading it again…')).toBeInTheDocument()

    await userEvent.click(list.getByRole('button', { name: 'Unmute beta/x' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t unmute beta/x: database is locked')
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
    expect(group(/^Unreleased changes · 0$/)).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('12 clones without a GitHub origin aren’t listed.')).toBeInTheDocument()
  })

  it('counts one clone it can’t list', async () => {
    platform.listReleaseCandidates.mockResolvedValue(
      releaseCandidates({ repos: [releaseCandidate()], withoutGitHub: 1 }),
    )
    renderPage()

    expect(await screen.findByText('1 clone without a GitHub origin isn’t listed.')).toBeInTheDocument()
  })

  it('offers to change the repository locations while there are no clones', async () => {
    platform.listReleaseCandidates.mockResolvedValue(releaseCandidates({ repos: [], withoutGitHub: 0 }))
    const { onOpenSettings } = renderPage()

    expect(
      await screen.findByText('No clones under ~/projects yet. Release lists the repositories you have cloned there.'),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Change the repository locations' }))
    expect(onOpenSettings).toHaveBeenCalledExactlyOnceWith('repositories')
  })

  it('names the other repository locations while there are no clones', async () => {
    platform.listReleaseCandidates.mockResolvedValue(releaseCandidates({ repos: [], withoutGitHub: 0 }))
    renderPage({ where: 'in ~/projects and 2 other locations' })

    expect(
      await screen.findByText(
        'No clones in ~/projects and 2 other locations yet. Release lists the repositories you have cloned there.',
      ),
    ).toBeInTheDocument()
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

  describe('the sort', () => {
    it('lists each group with the newest commit first until another order is picked', () => {
      renderSorted()

      expect(sorts().getByRole('radio', { name: 'Newest' })).toBeChecked()
      expect(sorts().getByRole('radio', { name: 'A–Z' })).not.toBeChecked()
      expect(sorts().getByRole('radio', { name: 'Newest' }).closest('label')).toHaveAttribute(
        'title',
        'The newest commit first',
      )
      expect(unreleased()).toEqual(holding('Web-kit', 'api', 'Sdk-kit', 'zeta'))
    })

    it('starts on A–Z when the settings remember it', () => {
      renderSorted({ releaseSort: 'name' })

      expect(sorts().getByRole('radio', { name: 'A–Z' })).toBeChecked()
      expect(sorts().getByRole('radio', { name: 'Newest' })).not.toBeChecked()
      expect(unreleased()).toEqual(holding('api', 'Sdk-kit', 'Web-kit', 'zeta'))
    })

    it('lists each group by name in any case, or by the most commits, and stores each choice', async () => {
      const { onReleaseSort } = renderSorted()

      await userEvent.click(sorts().getByRole('radio', { name: 'A–Z' }))
      expect(sorts().getByRole('radio', { name: 'A–Z' })).toBeChecked()
      expect(unreleased()).toEqual(holding('api', 'Sdk-kit', 'Web-kit', 'zeta'))

      await userEvent.click(sorts().getByRole('radio', { name: 'Commits' }))
      expect(sorts().getByRole('radio', { name: 'Commits' })).toBeChecked()
      expect(unreleased()).toEqual(holding('Sdk-kit', 'api', 'zeta', 'Web-kit'))

      expect(onReleaseSort.mock.calls).toEqual([['name'], ['commits']])
    })

    it('keeps the pinned repositories first in every order', async () => {
      renderSorted({ repoRules: { pinned: ['acme-org/zeta'], muted: [] } })

      expect(unreleased()).toEqual(holding('zeta', 'Web-kit', 'api', 'Sdk-kit'))

      await userEvent.click(sorts().getByRole('radio', { name: 'A–Z' }))
      expect(unreleased()).toEqual(holding('zeta', 'api', 'Sdk-kit', 'Web-kit'))

      await userEvent.click(sorts().getByRole('radio', { name: 'Commits' }))
      expect(unreleased()).toEqual(holding('zeta', 'Sdk-kit', 'api', 'Web-kit'))
    })

    it('keeps the order while it searches, with nowhere to store it', async () => {
      platform.listReleaseCandidates.mockResolvedValue(releaseCandidates({ repos: sortRows() }))
      renderPage()

      await screen.findByRole('list', { name: 'Unreleased changes' })
      await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'kit')
      expect(unreleased()).toEqual(holding('Web-kit', 'Sdk-kit'))

      await userEvent.click(sorts().getByRole('radio', { name: 'A–Z' }))
      expect(unreleased()).toEqual(holding('Sdk-kit', 'Web-kit'))
    })

    it('keeps the order picked while the settings store it', async () => {
      const { rerender } = renderSorted()

      await userEvent.click(sorts().getByRole('radio', { name: 'Commits' }))
      rerender()

      expect(sorts().getByRole('radio', { name: 'Commits' })).toBeChecked()
      expect(unreleased()).toEqual(holding('Sdk-kit', 'api', 'zeta', 'Web-kit'))
    })
  })

  describe('the rows', () => {
    it('counts an unreleased group’s rows by readiness in its heading, what needs a look first', () => {
      renderListed([
        releaseCandidate({ repo: 'acme-org/ready', clone: null }),
        releaseCandidate({ repo: 'acme-org/dirty', clone: cloneState({ dirty: 2 }) }),
        releaseCandidate({ repo: 'acme-org/red', checks: 'FAILURE', clone: null }),
      ])

      expect(group(/^Unreleased changes · 3: 1 failing, 1 blocked, 1 ready$/)).toHaveAttribute('aria-expanded', 'true')
    })

    it('counts nothing in the heading of a group without Release…', () => {
      renderListed(
        [
          releaseCandidate({ repo: 'acme-org/web', group: 'upToDate', clone: null }),
          releaseCandidate({ repo: 'acme-org/red', group: 'upToDate', checks: 'FAILURE', clone: null }),
        ],
        { sections: { 'release.upToDate': true } },
      )

      expect(group(/^Up to date · 2$/)).toHaveAttribute('aria-expanded', 'true')
      expect(screen.getAllByRole('listitem')).toHaveLength(2)
    })

    it('shows only the blocker of a clone that blocks a release', () => {
      renderListed([releaseCandidate({ clone: cloneState({ dirty: 2, behind: true }) })])

      const row = within(screen.getByRole('list', { name: 'Unreleased changes' })).getByRole('listitem')

      expect(row).toHaveTextContent('Your clone: 2 uncommitted')
      expect(row).not.toHaveTextContent('behind')
      expect(group(/^Unreleased changes · 1: 1 blocked$/)).toBeInTheDocument()
    })

    it('badges a headline three of a group’s listed rows share, and drops it when a search leaves two', async () => {
      const shared = 'Bump the shared workflows (#12)'

      renderListed(
        [
          releaseCandidate({ repo: 'acme-org/api' }),
          releaseCandidate({ repo: 'acme-org/sdk-go', headline: shared }),
          releaseCandidate({ repo: 'acme-org/sdk-js', headline: shared }),
          releaseCandidate({ repo: 'acme-org/web', headline: shared }),
          releaseCandidate({ repo: 'acme-org/docs', group: 'upToDate', headline: shared, clone: null }),
        ],
        { sections: { 'release.upToDate': true } },
      )

      const rows = within(screen.getByRole('list', { name: 'Unreleased changes' })).getAllByRole('listitem')
      const badges = screen.getAllByTitle(
        '3 of the listed repositories have this as their newest commit: acme-org/sdk-go, acme-org/sdk-js, acme-org/web',
      )

      expect(badges).toHaveLength(3)
      expect(badges.map((b) => b.closest('li'))).toEqual(rows.slice(1))
      expect(rows[0]).toHaveTextContent('acme-org/api')
      expect(rows[0]).not.toHaveTextContent('×')
      expect(within(screen.getByRole('list', { name: 'Up to date' })).getByRole('listitem')).not.toHaveTextContent('×')

      await userEvent.type(screen.getByRole('searchbox', { name: 'Search repositories' }), 'sdk')

      expect(unreleased()).toEqual(holding('sdk-go', 'sdk-js'))
      expect(screen.getAllByText(shared)).toHaveLength(2)
      expect(screen.queryByText(/^×/)).not.toBeInTheDocument()
    })

    it('marks the repositories a rule pins, naming the rule, and no other', () => {
      renderSorted({ repoRules: { pinned: ['acme-org/zeta'], muted: [] } })

      const list = within(screen.getByRole('list', { name: 'Unreleased changes' }))
      const pin = list.getByRole('img', { name: 'Pinned with acme-org/zeta' })

      expect(pin.closest('li')).toHaveTextContent('acme-org/zeta')
      expect(list.getAllByRole('img')).toHaveLength(1)
      expect(list.getAllByRole('listitem')).toHaveLength(4)
    })

    it('marks every repository an org’s rule pins with the org', () => {
      renderSorted({ repoRules: { pinned: ['acme-org'], muted: [] } })

      expect(
        within(screen.getByRole('list', { name: 'Unreleased changes' })).getAllByRole('img', {
          name: 'Pinned with acme-org',
        }),
      ).toHaveLength(4)
    })
  })

  it('lists the recent releases, each opening its run', async () => {
    const { onOpenRun } = renderPage({
      runs: [
        scriptRun(),
        releaseRun({ id: 8, status: 'running', result: { ...releaseRun().result, outcome: null } }),
        releaseRun(),
        releaseRun({ id: 6, title: 'Release v1.4.2', result: { ...releaseRun().result, intent: null } }),
        releaseRun({
          id: 5,
          result: { ...releaseRun().result, outcome: { kind: 'released', follow: { state: 'published' } } },
        }),
      ],
    })

    const recent = within(await screen.findByRole('list', { name: 'Recent releases' }))
    const rows = recent.getAllByRole('button')

    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining('v1.4.3acme-org/sdkrunning'),
      expect.stringContaining('v1.4.3acme-org/sdkon GitHub'),
      expect.stringContaining('Release v1.4.2acme-org/sdkon GitHub'),
      expect.stringContaining('v1.4.3acme-org/sdkpublished'),
    ])

    await userEvent.click(rows[1] as HTMLElement)
    expect(onOpenRun).toHaveBeenCalledExactlyOnceWith(7)
  })
})
