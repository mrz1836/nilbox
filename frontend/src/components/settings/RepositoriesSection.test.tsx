import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { CloneLocation, Settings } from '../../platform'
import { agents, defaultPrompts, locationPreview, locationState, profile, prompts, settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { marker } from './CloneLocations'
import { RepositoriesSection } from './RepositoriesSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// stored are the other locations most tests start with: a folder, a single clone, and a
// folder that isn't there.
const stored: CloneLocation[] = [
  { path: '~/work', kind: 'folder' },
  { path: '~/Desktop/exp', kind: 'clone' },
  { path: '/Volumes/ext/src', kind: 'folder' },
]

function renderSection(current: Settings = settings({ cloneLocations: stored })) {
  const onSaved = vi.fn()

  render(
    <RepositoriesSection
      settings={current}
      onSaved={onSaved}
      agents={agents()}
      profiles={[profile()]}
      prompts={prompts()}
      defaultPrompts={defaultPrompts()}
      launchDefaults={[]}
      onLaunchChanged={() => Promise.resolve()}
    />,
  )

  return { onSaved }
}

function locations() {
  return within(screen.getByRole('list', { name: 'Other repository locations' }))
}

function field() {
  return screen.getByRole('textbox', { name: 'Folder to add' })
}

beforeEach(() => {
  platform.checkCloneLocations.mockResolvedValue([
    locationState(),
    locationState({ path: '~/work', primary: false, clones: 4 }),
    locationState({ path: '~/Desktop/exp', kind: 'clone', primary: false, repo: 'acme-org/example-repo' }),
    locationState({ path: '/Volumes/ext/src', primary: false, state: 'missing', clones: 0 }),
  ])
})

describe('RepositoriesSection', () => {
  it('lists the other locations with what each is now', async () => {
    renderSection()

    expect(await locations().findByText('folder · 4 clones')).toBeInTheDocument()
    expect(locations().getByText('clone · acme-org/example-repo')).toBeInTheDocument()
    expect(locations().getByText('Not found')).toBeInTheDocument()
    expect(locations().getAllByRole('button', { name: 'Check again' })).toHaveLength(1)
    expect(
      screen.getByText(/Removing a location leaves its clones and their worktrees as they are/),
    ).toBeInTheDocument()
  })

  it('says so while there are none', () => {
    renderSection(settings())

    expect(screen.getByText('None yet: only the clone root is looked in.')).toBeInTheDocument()
  })

  it('picks the clone root in a folder dialog and saves it at once', async () => {
    platform.chooseFolder.mockResolvedValue('/srv/code')
    platform.setCloneRoot.mockResolvedValue(settings({ cloneRoot: '/srv/code' }))
    const { onSaved } = renderSection()

    // The clone root's Choose… comes first; the other locations' sits in their card.
    const [rootChoose] = screen.getAllByRole('button', { name: 'Choose…' })
    await userEvent.click(rootChoose as HTMLElement)

    expect(platform.chooseFolder).toHaveBeenCalledWith({ title: 'Choose the clone root', button: 'Use' })
    await waitFor(() => {
      expect(platform.setCloneRoot).toHaveBeenCalledWith('/srv/code')
    })
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ cloneRoot: '/srv/code' }))
    })
    expect(screen.getByRole('textbox', { name: 'Clone root' })).toHaveValue('/srv/code')
  })

  it('adds a folder with clones in it at once, saying which clone locations it holds', async () => {
    const added = settings({ cloneLocations: [{ path: '~/projects2', kind: 'folder' }] })
    platform.previewCloneLocation.mockResolvedValue(locationPreview({ path: '~/work2', replaces: ['~/work2/web'] }))
    platform.addCloneLocations.mockResolvedValue(added)
    const { onSaved } = renderSection()

    await userEvent.type(field(), ' ~/work2 {Enter}')

    expect(platform.previewCloneLocation).toHaveBeenCalledWith('~/work2')
    await waitFor(() => {
      expect(platform.addCloneLocations).toHaveBeenCalledWith(['~/work2'])
    })
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(added)
    })
    expect(
      await screen.findByText('Added ~/work2. It holds ~/work2/web, so that clone is no longer listed on its own.'),
    ).toBeInTheDocument()
    expect(field()).toHaveValue('')
  })

  it('suggests the folders inside one with no clones directly inside', async () => {
    platform.previewCloneLocation.mockResolvedValue(
      locationPreview({
        path: '~/src',
        clones: 0,
        inside: [
          { path: '~/src/acme-org', clones: 4 },
          { path: '~/src/tools', clones: 2 },
        ],
      }),
    )
    renderSection()

    await userEvent.type(field(), '~/src{Enter}')

    expect(
      await screen.findByText('~/src has no clones directly inside; ~/src/acme-org (4) and ~/src/tools (2) do.'),
    ).toBeInTheDocument()
    expect(platform.addCloneLocations).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Add those' }))

    expect(platform.addCloneLocations).toHaveBeenCalledWith(['~/src/acme-org', '~/src/tools'])
  })

  it('adds a folder with no clones anyway when asked', async () => {
    platform.previewCloneLocation.mockResolvedValue(locationPreview({ path: '~/empty', clones: 0, partial: true }))
    renderSection()

    await userEvent.type(field(), '~/empty{Enter}')

    expect(
      await screen.findByText(
        '~/empty has no clones directly inside, nor in the folders inside it. Only its first folders were looked in.',
      ),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Add ~/empty anyway' }))

    expect(platform.addCloneLocations).toHaveBeenCalledWith(['~/empty'])
  })

  it('offers the folder of a clone that holds other clones, or the clone alone', async () => {
    platform.previewCloneLocation.mockResolvedValue(
      locationPreview({ path: '~/Desktop/new', kind: 'clone', clones: 0, parent: { path: '~/Desktop', clones: 3 } }),
    )
    renderSection(settings())

    await userEvent.type(field(), '~/Desktop/new{Enter}')

    expect(
      await screen.findByText('~/Desktop/new is a clone. Its folder, ~/Desktop, holds 3 other clones.'),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Add only this clone' }))
    expect(platform.addCloneLocations).toHaveBeenCalledWith(['~/Desktop/new'])

    await userEvent.type(field(), '~/Desktop/new{Enter}')
    await userEvent.click(await screen.findByRole('button', { name: 'Add ~/Desktop' }))
    expect(platform.addCloneLocations).toHaveBeenLastCalledWith(['~/Desktop'])
  })

  it('puts the offer away on Cancel', async () => {
    platform.previewCloneLocation.mockResolvedValue(locationPreview({ path: '~/src', clones: 0 }))
    renderSection()

    await userEvent.type(field(), '~/src{Enter}')
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }))

    expect(screen.queryByText(/has no clones directly inside/)).not.toBeInTheDocument()
    expect(platform.addCloneLocations).not.toHaveBeenCalled()
  })

  it('refuses a folder the locations already cover, as the field’s error', async () => {
    platform.previewCloneLocation.mockResolvedValue(
      locationPreview({ path: '~/projects/api', kind: 'clone', covered: '~/projects/api is already in ~/projects.' }),
    )
    renderSection()

    await userEvent.type(field(), '~/projects/api{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('~/projects/api is already in ~/projects.')
    expect(field()).toHaveAccessibleDescription('~/projects/api is already in ~/projects.')
    expect(platform.addCloneLocations).not.toHaveBeenCalled()
  })

  it('shows why a folder can’t be one', async () => {
    platform.previewCloneLocation.mockRejectedValue(
      new Error('~/.hidden is hidden, and the app never looks in hidden folders.'),
    )
    renderSection()

    await userEvent.type(field(), '~/.hidden{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('~/.hidden is hidden')
  })

  it('looks at a folder picked in the folder dialog', async () => {
    platform.chooseFolder.mockResolvedValue('/srv/repos')
    renderSection()

    const card = screen.getByRole('region', { name: 'Other repository locations' })
    await userEvent.click(within(card).getByRole('button', { name: 'Choose…' }))

    expect(platform.chooseFolder).toHaveBeenCalledWith({ title: 'Choose a repository location', button: 'Add' })
    await waitFor(() => {
      expect(platform.previewCloneLocation).toHaveBeenCalledWith('/srv/repos')
    })
  })

  it('removes a location', async () => {
    platform.removeCloneLocation.mockResolvedValue(settings({ cloneLocations: stored.slice(1) }))
    const { onSaved } = renderSection()

    await userEvent.click(locations().getByRole('button', { name: 'Remove ~/work' }))

    expect(platform.removeCloneLocation).toHaveBeenCalledWith('~/work')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ cloneLocations: stored.slice(1) }))
    })
  })

  it('shows an add the backend refused as the field’s error', async () => {
    platform.addCloneLocations.mockRejectedValue(new Error('~/work2 is already in ~/work.'))
    const { onSaved } = renderSection()

    await userEvent.type(field(), '~/work2{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('~/work2 is already in ~/work.')
    expect(onSaved).not.toHaveBeenCalled()
    expect(field()).toHaveValue('~/work2')
  })

  it('checks a location again, and reads every location again once it can', async () => {
    platform.addCloneLocations.mockResolvedValue(settings({ cloneLocations: stored }))
    renderSection()

    await userEvent.click(await locations().findByRole('button', { name: 'Check again' }))

    expect(platform.addCloneLocations).toHaveBeenCalledWith(['/Volumes/ext/src'])
    await waitFor(() => {
      expect(platform.checkCloneLocations).toHaveBeenCalledTimes(2)
    })
  })

  it('checks a location again, saying why that failed', async () => {
    platform.addCloneLocations.mockRejectedValue(new Error('/Volumes/ext/src doesn’t exist.'))
    renderSection()

    await userEvent.click(await locations().findByRole('button', { name: 'Check again' }))

    expect(platform.addCloneLocations).toHaveBeenCalledWith(['/Volumes/ext/src'])
    expect(await locations().findByRole('alert')).toHaveTextContent('/Volumes/ext/src doesn’t exist.')
  })
})

describe('marker', () => {
  it('words how a location reads now', () => {
    const extra = { primary: false }

    expect(marker(undefined)).toEqual({ text: '', warn: false, recheck: false })
    expect(marker(locationState({ ...extra, clones: 1 })).text).toBe('folder · 1 clone')
    expect(marker(locationState({ ...extra, kind: 'clone', repo: '' })).text).toBe('clone')
    expect(marker(locationState({ ...extra, state: 'nowAClone', clones: 2 }))).toEqual({
      text: 'folder · 2 clones · now a clone itself',
      warn: true,
      recheck: true,
    })
    expect(marker(locationState({ ...extra, state: 'notAClone', kind: 'clone' })).text).toBe('No longer a clone')
    expect(marker(locationState({ ...extra, state: 'notFolder' })).text).toBe('Not a folder')
    expect(marker(locationState({ ...extra, state: 'unreadable' })).text).toBe('Can’t be read')
    expect(marker(locationState({ ...extra, state: 'notAnswering' }))).toEqual({
      text: 'Not answering',
      warn: true,
      recheck: true,
    })
    expect(marker(locationState({ ...extra, state: 'covered', coveredBy: '~/projects' }))).toEqual({
      text: 'Already in ~/projects',
      warn: true,
      recheck: false,
    })
  })
})
