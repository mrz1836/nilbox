import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { IntegrationSwitch, IntegrationTool, Settings, ToolCopy } from '../../platform'
import { goPreCommitTool, integrationsConfig, integrationTool, settings, toolCopy } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { IntegrationCard } from './IntegrationCard'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// noCopy is the copy of a tool that is missing: nothing.
function noCopy(): ToolCopy {
  return toolCopy({
    path: '',
    displayPath: '',
    runs: false,
    version: { raw: '', commit: '', built: '' },
    onPath: false,
  })
}

// renderCard renders one tool's card, and returns what it hands its section.
function renderCard(tool: IntegrationTool = integrationTool(), checking = false) {
  const onSaved = vi.fn<(settings: Settings) => void>()
  const reload = vi.fn(() => Promise.resolve())

  render(<IntegrationCard tool={tool} checking={checking} onSaved={onSaved} reload={reload} />)

  return { onSaved, reload, card: within(screen.getByRole('region', { name: tool.name })) }
}

// withSwitch is the settings as stored once the mage-x switch is set.
function withSwitch(state: IntegrationSwitch): Settings {
  return settings({
    integrations: integrationsConfig({ switches: { ...integrationsConfig().switches, 'mage-x': state } }),
  })
}

// withPath is the settings as stored once a copy of mage-x is chosen.
function withPath(path: string): Settings {
  return settings({ integrations: integrationsConfig({ paths: { ...integrationsConfig().paths, 'mage-x': path } }) })
}

// openDetails opens a card's details.
async function openDetails() {
  await userEvent.click(screen.getByRole('button', { name: 'Details' }))
}

describe('IntegrationCard', () => {
  it('shows the tool, what it is for, its switch, and the copy in use', () => {
    const { card } = renderCard()

    expect(card.getByRole('heading', { name: 'mage-x' })).toBeInTheDocument()
    expect(card.getByText('Release new versions of your repositories in two clicks.')).toBeInTheDocument()
    expect(card.getByRole('switch', { name: 'mage-x' })).toBeChecked()
    expect(card.getByRole('switch', { name: 'mage-x' })).toHaveAccessibleDescription(
      'On · found automatically Release new versions of your repositories in two clicks.',
    )
    expect(card.getByText('magex 1.29.5 · ~/.local/bin/magex · GitHub release')).toBeInTheDocument()
    expect(card.queryByText('Checking…')).not.toBeInTheDocument()
    expect(card.queryByText(/is available/)).not.toBeInTheDocument()
    expect(card.queryByText(/isn’t there any more/)).not.toBeInTheDocument()
    // Installing comes later: nothing here installs, updates, or removes a copy.
    expect(card.queryByRole('button', { name: /install|update|trash/i })).not.toBeInTheDocument()
  })

  it.each<[string, IntegrationTool, string]>([
    ['a copy from Homebrew', goPreCommitTool(), 'go-pre-commit 1.12.3 · /opt/homebrew/bin/go-pre-commit · Homebrew'],
    [
      'a copy go install put there',
      integrationTool({ copy: toolCopy({ displayPath: '~/go/bin/magex', source: 'goInstall' }) }),
      'magex 1.29.5 · ~/go/bin/magex · go install',
    ],
    [
      'a link to a development build',
      integrationTool({
        copy: toolCopy({
          link: true,
          target: '~/projects/mage-x/bin/magex',
          source: 'development',
          version: { raw: 'dev', commit: '', built: '' },
        }),
      }),
      'magex dev · ~/.local/bin/magex → ~/projects/mage-x/bin/magex · development build',
    ],
    [
      'a copy too old to drive',
      goPreCommitTool({
        state: 'tooOld',
        copy: toolCopy({ displayPath: '~/go/bin/go-pre-commit', version: { raw: '1.11.0', commit: '', built: '' } }),
      }),
      `go-pre-commit 1.11.0 is older than 1.12.0, which ${brand.name} needs`,
    ],
    [
      'a copy that won’t run',
      integrationTool({
        state: 'wontRun',
        on: false,
        copy: toolCopy({ runs: false, version: { raw: '', commit: '', built: '' }, error: 'exec format error' }),
      }),
      '~/.local/bin/magex is there but won’t run: exec format error',
    ],
    ['no copy at all', integrationTool({ state: 'missing', on: false, copy: noCopy() }), 'Not on this Mac'],
  ])('says what was found of %s', (_, tool, line) => {
    const { card } = renderCard(tool)

    expect(card.getByText(line)).toBeInTheDocument()
    expect(card.queryByRole('button', { name: /install/i })).not.toBeInTheDocument()
  })

  it('offers a newer release, with what is new in it', async () => {
    const url = 'https://github.com/acme-org/mage-x/releases/tag/v1.30.0'
    const { card } = renderCard(
      integrationTool({ state: 'update', latest: { version: '1.30.0', url, checkedAt: '2026-06-15T09:00:00Z' } }),
    )

    expect(card.getByText('magex 1.29.5 · ~/.local/bin/magex · GitHub release')).toBeInTheDocument()
    expect(card.getByText(/^1\.30\.0 is available ·/)).toBeInTheDocument()
    expect(card.queryByRole('button', { name: /^update/i })).not.toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'What’s new' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(url)
  })

  it.each<[IntegrationSwitch, boolean, string]>([
    ['auto', true, 'On · found automatically'],
    ['auto', false, 'Off'],
    ['on', true, 'On'],
    ['off', false, 'Off'],
  ])('captions a switch set to %s that is on: %s', (set, on, caption) => {
    const { card } = renderCard(integrationTool({ switch: set, on }))
    const toggle = card.getByRole('switch', { name: 'mage-x' })

    expect(card.getByText(caption)).toBeInTheDocument()
    expect(toggle).toHaveProperty('checked', on)
    expect(toggle).toHaveAccessibleDescription(new RegExp(`^${caption} Release`))
  })

  it('stores the opposite of what the switch shows, for good, and shows it at once', async () => {
    let finish: (stored: Settings) => void = () => undefined
    platform.setIntegrationEnabled.mockReturnValue(
      new Promise<Settings>((resolve) => {
        finish = resolve
      }),
    )
    const { card, onSaved, reload } = renderCard()
    const toggle = card.getByRole('switch', { name: 'mage-x' })

    await userEvent.click(toggle)

    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('mage-x', 'off')
    expect(toggle).not.toBeChecked()
    expect(toggle).toBeDisabled()
    expect(card.getByText('Off')).toBeInTheDocument()

    finish(withSwitch('off'))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(withSwitch('off'))
    })
    expect(reload).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(toggle).toBeEnabled()
    })
  })

  it('turns a tool that is off on for good', async () => {
    const { card, onSaved } = renderCard(integrationTool({ switch: 'auto', on: false, state: 'missing' }))

    await userEvent.click(card.getByRole('switch', { name: 'mage-x' }))

    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('mage-x', 'on')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(withSwitch('on'))
    })
  })

  it('shows why a flip was refused, with the switch as it was', async () => {
    platform.setIntegrationEnabled.mockRejectedValue(new Error('database is locked'))
    const { card, onSaved, reload } = renderCard()
    const toggle = card.getByRole('switch', { name: 'mage-x' })

    await userEvent.click(toggle)

    expect(await card.findByRole('alert')).toHaveTextContent('database is locked')
    expect(toggle).toBeChecked()
    expect(toggle).toHaveAccessibleDescription(/two clicks\. database is locked$/)
    expect(card.getByText('On · found automatically')).toBeInTheDocument()
    expect(onSaved).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
  })

  it('says it is checking beside what the last look found', () => {
    renderCard(integrationTool({ checking: true }))
    renderCard(goPreCommitTool(), true)

    for (const name of ['mage-x', 'go-pre-commit']) {
      const card = within(screen.getByRole('region', { name }))

      expect(card.getByText('Checking…')).toBeInTheDocument()
      expect(card.getByText(/· (GitHub release|Homebrew)$/)).toBeInTheDocument()
    }
  })

  it('says when the copy chosen is gone, and which one is used instead', () => {
    const { card } = renderCard(integrationTool({ chosen: '~/bin/magex', chosenMissing: true }))

    expect(card.getByText(/^The copy you chose/)).toHaveTextContent(
      `The copy you chose, ~/bin/magex, isn’t there any more, so ${brand.name} uses the one your shell finds.`,
    )
  })
})

describe('IntegrationCard details', () => {
  it('opens on request, with the copy in use, its version, commit, and build date', async () => {
    const { card } = renderCard(
      integrationTool({
        copy: toolCopy({
          link: true,
          target: '~/projects/mage-x/bin/magex',
          source: 'development',
          version: { raw: '1.29.6-dev', commit: '9a61f25', built: '2026-11-05T04:17:38Z' },
        }),
      }),
    )
    const toggle = card.getByRole('button', { name: 'Details' })

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(card.queryByText('Path')).not.toBeInTheDocument()

    await openDetails()

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(document.getElementById(toggle.getAttribute('aria-controls') ?? '')).toBeInTheDocument()
    const terms = card.getAllByRole('term').map((t) => t.textContent)
    const values = card.getAllByRole('definition').map((d) => d.textContent)
    expect(terms).toEqual(['Path', 'Source', 'Version'])
    expect(values).toEqual([
      '~/.local/bin/magex → ~/projects/mage-x/bin/magex',
      'development build',
      '1.29.6-dev · commit 9a61f25 · built 2026-11-05T04:17:38Z',
    ])
    expect(card.queryByRole('list', { name: 'Other copies' })).not.toBeInTheDocument()

    await userEvent.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(card.queryByText('Path')).not.toBeInTheDocument()
  })

  it('shows the version alone when the copy printed nothing more', async () => {
    const { card } = renderCard()

    await openDetails()

    expect(card.getAllByRole('definition').map((d) => d.textContent)).toEqual([
      '~/.local/bin/magex',
      'GitHub release',
      '1.29.5',
    ])
  })

  it('shows no version for a copy that won’t run, and no copy for a missing tool', async () => {
    renderCard(
      integrationTool({
        state: 'wontRun',
        copy: toolCopy({ runs: false, version: { raw: '', commit: '', built: '' }, error: 'killed' }),
      }),
    )
    renderCard(goPreCommitTool({ state: 'missing', on: false, copy: noCopy() }))

    for (const button of screen.getAllByRole('button', { name: 'Details' })) {
      await userEvent.click(button)
    }

    const broken = within(screen.getByRole('region', { name: 'mage-x' }))
    const missing = within(screen.getByRole('region', { name: 'go-pre-commit' }))
    expect(broken.getAllByRole('term').map((t) => t.textContent)).toEqual(['Path', 'Source'])
    expect(missing.queryByRole('term')).not.toBeInTheDocument()
    expect(missing.getByRole('textbox', { name: 'Use a different copy' })).toBeInTheDocument()
  })

  it('lists the other copies, warning of the one the terminal runs instead', async () => {
    const { card } = renderCard(
      integrationTool({
        others: [
          toolCopy({
            path: '/Users/example/go/bin/magex',
            displayPath: '~/go/bin/magex',
            source: 'goInstall',
            version: { raw: '1.28.0', commit: '', built: '' },
            shadowing: true,
          }),
          toolCopy({
            path: '/opt/homebrew/bin/magex',
            displayPath: '/opt/homebrew/bin/magex',
            source: 'homebrew',
            version: { raw: '1.29.1', commit: '', built: '' },
          }),
          toolCopy({
            path: '/usr/local/bin/magex',
            displayPath: '/usr/local/bin/magex',
            runs: false,
            version: { raw: '', commit: '', built: '' },
            error: 'permission denied',
          }),
        ],
      }),
    )

    await openDetails()

    const others = within(card.getByRole('list', { name: 'Other copies' })).getAllByRole('listitem')

    expect(others.map((li) => li.textContent)).toEqual([
      'Your terminal runs ~/go/bin/magex 1.28.0, not this one.',
      '/opt/homebrew/bin/magex 1.29.1 (Homebrew)',
      '/usr/local/bin/magex (GitHub release) won’t run: permission denied',
    ])
  })

  it('uses a copy typed in, and reads the tools again', async () => {
    const { card, onSaved, reload } = renderCard()

    await openDetails()

    const field = card.getByRole('textbox', { name: 'Use a different copy' })
    const use = card.getByRole('button', { name: 'Use' })
    expect(use).toBeDisabled()

    await userEvent.type(field, '  ~/bin/magex ')
    await userEvent.click(use)

    expect(platform.setIntegrationPath).toHaveBeenCalledExactlyOnceWith('mage-x', '~/bin/magex')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(withPath('~/bin/magex'))
    })
    expect(reload).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(field).toHaveValue('')
    })
  })

  it('shows why a copy was refused, until the path changes', async () => {
    platform.setIntegrationPath.mockRejectedValue(new Error('There’s nothing at ~/bin/magex.'))
    const { card, onSaved } = renderCard()

    await openDetails()

    const field = card.getByRole('textbox', { name: 'Use a different copy' })
    await userEvent.type(field, '~/bin/magex{Enter}')

    expect(await card.findByRole('alert')).toHaveTextContent('There’s nothing at ~/bin/magex.')
    expect(field).toHaveAttribute('aria-invalid', 'true')
    expect(field).toHaveAccessibleDescription('There’s nothing at ~/bin/magex.')
    expect(field).toHaveValue('~/bin/magex')
    expect(onSaved).not.toHaveBeenCalled()

    await userEvent.type(field, '2')

    expect(card.queryByRole('alert')).not.toBeInTheDocument()
    expect(field).toHaveAttribute('aria-invalid', 'false')
  })

  it('uses a copy chosen in the file dialog', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/Users/example/bin/magex')
    const { card, onSaved } = renderCard()

    await openDetails()
    await userEvent.click(card.getByRole('button', { name: 'Choose…' }))

    expect(platform.chooseFileToOpen).toHaveBeenCalledExactlyOnceWith({
      title: 'Choose a copy of magex',
      button: 'Use',
      filters: [],
    })
    expect(platform.setIntegrationPath).toHaveBeenCalledExactlyOnceWith('mage-x', '/Users/example/bin/magex')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(withPath('/Users/example/bin/magex'))
    })
  })

  it('keeps the copy in use when the file dialog is cancelled', async () => {
    const { card } = renderCard()

    await openDetails()
    await userEvent.click(card.getByRole('button', { name: 'Choose…' }))

    expect(platform.chooseFileToOpen).toHaveBeenCalledOnce()
    expect(platform.setIntegrationPath).not.toHaveBeenCalled()
  })

  it('says why the file dialog could not open', async () => {
    platform.chooseFileToOpen.mockRejectedValue(new Error('the dialog is already open'))
    const { card } = renderCard()

    await openDetails()
    await userEvent.click(card.getByRole('button', { name: 'Choose…' }))

    expect(await card.findByRole('alert')).toHaveTextContent('the dialog is already open')
    expect(platform.setIntegrationPath).not.toHaveBeenCalled()
  })

  it('goes back to the copy the shell finds', async () => {
    const { card, onSaved } = renderCard(integrationTool({ chosen: '~/bin/magex' }))

    await openDetails()

    expect(card.getByText(/^You chose/)).toHaveTextContent('You chose ~/bin/magex.')

    await userEvent.click(card.getByRole('button', { name: 'Use the one your shell finds' }))

    expect(platform.setIntegrationPath).toHaveBeenCalledExactlyOnceWith('mage-x', '')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(withPath(''))
    })
  })

  it('offers no way back while the shell’s copy is in use', async () => {
    const { card } = renderCard()

    await openDetails()

    expect(card.queryByRole('button', { name: 'Use the one your shell finds' })).not.toBeInTheDocument()
  })

  it('links to the tool’s releases on GitHub', async () => {
    const { card } = renderCard()

    await openDetails()
    await userEvent.click(card.getByRole('button', { name: 'Releases on GitHub' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/mage-x/releases')
  })
})
