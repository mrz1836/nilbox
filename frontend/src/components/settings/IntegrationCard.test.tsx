import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { formatDateTime } from '../../lib/time'
import type {
  InstallPlan,
  InstallProgress,
  InstallResult,
  IntegrationSwitch,
  IntegrationTool,
  IntegrationTools,
  Settings,
  ToolCopy,
} from '../../platform'
import {
  goPreCommitTool,
  installPlan,
  installResult,
  integrationsConfig,
  integrationTool,
  integrationTools,
  scriptRun,
  settings,
  toolCopy,
} from '../../test/fixtures'
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

// idle is the progress of a tool no install runs for.
const idle: InstallProgress = { step: '', done: 0, total: 0 }

// latest130 is mage-x's latest release once 1.30.0 is out.
const latest130 = {
  version: '1.30.0',
  url: 'https://github.com/acme-org/mage-x/releases/tag/v1.30.0',
  checkedAt: '2026-06-15T09:00:00Z',
}

// missingTool is mage-x with no copy, and 1.30.0 to install, with plan changed as given
// and an install as far as progress says.
function missingTool(plan: Partial<InstallPlan> = {}, progress: InstallProgress = idle): IntegrationTool {
  return integrationTool({
    state: 'missing',
    on: false,
    copy: noCopy(),
    install: installPlan({
      action: 'install',
      version: '1.30.0',
      archive: 'mage-x_1.30.0_darwin_arm64.tar.gz',
      replaces: '',
      replacesPath: '',
      foreign: false,
      ...plan,
    }),
    progress,
  })
}

// appCopy is a copy of magex the app installed in ~/.local/bin, on the shell's PATH.
function appCopy(overrides: Partial<ToolCopy> = {}): ToolCopy {
  return toolCopy({
    source: 'app',
    installedAt: '2026-06-15T11:40:00Z',
    archive: 'mage-x_1.29.5_darwin_arm64.tar.gz',
    ...overrides,
  })
}

// appTool is mage-x as the app installed it: found, and as new as its latest release.
function appTool(overrides: Partial<IntegrationTool> = {}): IntegrationTool {
  return integrationTool({ copy: appCopy(), install: installPlan({ foreign: false }), ...overrides })
}

// updateTool is mage-x as the app installed it, with 1.30.0 out, and plan changed as
// given.
function updateTool(plan: Partial<InstallPlan> = {}): IntegrationTool {
  return appTool({
    state: 'update',
    latest: latest130,
    install: installPlan({
      action: 'update',
      version: '1.30.0',
      archive: 'mage-x_1.30.0_darwin_arm64.tar.gz',
      foreign: false,
      ...plan,
    }),
  })
}

// wontRunTool is mage-x as the app installed it, there but not running.
function wontRunTool(): IntegrationTool {
  return appTool({
    state: 'wontRun',
    copy: appCopy({ runs: false, version: { raw: '', commit: '', built: '' }, error: 'killed' }),
    install: installPlan({ replaces: '', foreign: false }),
  })
}

// renderCard renders one tool's card, and returns what it hands its section.
function renderCard(tool: IntegrationTool = integrationTool(), checking = false) {
  const onSaved = vi.fn<(settings: Settings) => void>()
  const reload = vi.fn(() => Promise.resolve())
  const onTools = vi.fn<(tools: IntegrationTools) => void>()

  render(
    <IntegrationCard
      tool={tool}
      checking={checking}
      onSaved={onSaved}
      reload={reload}
      onTools={onTools}
      terminal={null}
    />,
  )

  return { onSaved, reload, onTools, card: within(screen.getByRole('region', { name: tool.name })) }
}

// dialog is the open dialog named name.
function dialog(name: string) {
  return within(screen.getByRole('dialog', { name }))
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
    // A tool that is found has no button, and no copy the app didn't install offers more.
    expect(card.queryByRole('button', { name: /install|update|trash/i })).not.toBeInTheDocument()
    expect(card.queryByText(/your PATH/)).not.toBeInTheDocument()
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
    ['a copy the app installed', appTool(), `magex 1.29.5 · ~/.local/bin/magex · installed by ${brand.name}`],
  ])('says what was found of %s', (_, tool, line) => {
    const { card } = renderCard(tool)

    expect(card.getByText(line)).toBeInTheDocument()
  })

  it('offers a newer release, with what is new in it', async () => {
    const { card } = renderCard(integrationTool({ state: 'update', latest: latest130 }))

    expect(card.getByText('magex 1.29.5 · ~/.local/bin/magex · GitHub release')).toBeInTheDocument()
    expect(card.getByText(/^1\.30\.0 is available ·/)).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'What’s new' }))

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith(latest130.url)
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
    const { card, onSaved } = renderCard(integrationTool({ switch: 'off', on: false }))

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
    // Only a copy the app installed can be reinstalled or moved to the Trash from here.
    expect(card.queryByText(/^Installed/)).not.toBeInTheDocument()
    expect(card.queryByRole('button', { name: /reinstall|trash/i })).not.toBeInTheDocument()
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
      'Your terminal runs ~/go/bin/magex 1.28.0, not this one.Move it to the Trash',
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

describe('IntegrationCard installs', () => {
  it.each<[string, IntegrationTool, string]>([
    ['a missing tool', missingTool(), 'Install mage-x 1.30.0'],
    ['a missing tool before its release is known', missingTool({ version: '', archive: '' }), 'Install mage-x'],
    ['a newer release', updateTool(), 'Update to 1.30.0'],
    ['a newer release not looked up yet', updateTool({ version: '' }), 'Update mage-x'],
    [
      'a copy too old to drive',
      integrationTool({ state: 'tooOld', install: installPlan({ action: 'update', version: '1.30.0' }) }),
      'Update to 1.30.0',
    ],
    ['a copy that won’t run', wontRunTool(), 'Reinstall'],
  ])('offers to install %s', (_, tool, label) => {
    const { card } = renderCard(tool)

    expect(card.getByRole('button', { name: label })).toBeEnabled()
  })

  it('says why Homebrew’s copy can’t be updated here, with the command to copy', async () => {
    const { card } = renderCard(goPreCommitTool({ state: 'update' }))

    expect(card.getByText('Homebrew manages this copy. Update it with brew upgrade go-pre-commit.')).toBeInTheDocument()
    expect(card.queryByRole('button', { name: /^update/i })).not.toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Copy the command' }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('brew upgrade go-pre-commit')
  })

  it('says why a missing tool can’t be installed now', () => {
    const blocked = `Sign in to GitHub first, so ${brand.name} can find the latest release.`
    const { card } = renderCard(missingTool({ version: '', archive: '', blocked }))

    expect(card.getByText(blocked)).toBeInTheDocument()
    expect(card.queryByRole('button', { name: /^install/i })).not.toBeInTheDocument()
    expect(card.queryByRole('button', { name: 'Copy the command' })).not.toBeInTheDocument()
  })

  it('says nothing of what blocks an install while the tool is found', () => {
    const { card } = renderCard(goPreCommitTool())

    expect(card.queryByText(/^Homebrew manages/)).not.toBeInTheDocument()
    expect(card.queryByRole('button', { name: 'Copy the command' })).not.toBeInTheDocument()
  })

  it.each<[string, InstallProgress, string]>([
    ['downloading', { step: 'downloading', done: 4_100_000, total: 9_000_000 }, 'Downloading… 4.1 of 9.0 MB'],
    [
      'downloading, rounded',
      { step: 'downloading', done: 1_234_567, total: 12_345_678 },
      'Downloading… 1.2 of 12.3 MB',
    ],
    ['downloading, of a size not known', { step: 'downloading', done: 52_000, total: 0 }, 'Downloading…'],
    ['verifying', { step: 'verifying', done: 9_000_000, total: 9_000_000 }, 'Checking the checksum…'],
    ['trying', { step: 'trying', done: 0, total: 0 }, 'Trying it…'],
    ['installing', { step: 'installing', done: 0, total: 0 }, 'Installing…'],
  ])('shows an install that is %s, and holds the button', (_, progress, line) => {
    const { card } = renderCard(missingTool({}, progress))

    expect(card.getByText(line)).toBeInTheDocument()
    expect(card.queryByText('Not on this Mac')).not.toBeInTheDocument()
    expect(card.getByRole('button', { name: 'Install mage-x 1.30.0' })).toBeDisabled()
  })

  it('asks first, and does nothing when cancelled', async () => {
    const { card } = renderCard(missingTool())

    await userEvent.click(card.getByRole('button', { name: 'Install mage-x 1.30.0' }))
    await userEvent.click(dialog('Install mage-x 1.30.0?').getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.installIntegration).not.toHaveBeenCalled()
    expect(platform.setIntegrationEnabled).not.toHaveBeenCalled()
  })

  it.each<
    [
      string,
      IntegrationTool,
      string,
      string,
      string,
      'installIntegration' | 'updateIntegration' | 'reinstallIntegration',
      string,
    ]
  >([
    [
      'install',
      missingTool(),
      'Install mage-x 1.30.0',
      'Install mage-x 1.30.0?',
      'Install',
      'installIntegration',
      'Installed mage-x 1.30.0 in ~/.local/bin',
    ],
    [
      'update',
      updateTool(),
      'Update to 1.30.0',
      'Update mage-x to 1.30.0?',
      'Update',
      'updateIntegration',
      'Updated magex to 1.30.0',
    ],
    [
      'reinstall',
      wontRunTool(),
      'Reinstall',
      'Reinstall mage-x 1.29.5?',
      'Reinstall',
      'reinstallIntegration',
      'Reinstalled mage-x 1.29.5',
    ],
  ])('runs the %s the user confirms, and says what it did', async (_, tool, label, title, confirm, call, message) => {
    const { card, onTools, onSaved } = renderCard(tool)

    await userEvent.click(card.getByRole('button', { name: label }))
    await userEvent.click(dialog(title).getByRole('button', { name: confirm }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform[call]).toHaveBeenCalledExactlyOnceWith('mage-x')
    expect(await card.findByText(message)).toBeInTheDocument()
    expect(onTools).toHaveBeenCalledExactlyOnceWith(integrationTools())
    expect(onSaved).not.toHaveBeenCalled()
    expect(
      [platform.installIntegration, platform.updateIntegration, platform.reinstallIntegration].filter(
        (installer) => installer.mock.calls.length > 0,
      ),
    ).toHaveLength(1)
  })

  it('holds the button while the install runs', async () => {
    let finish: (result: InstallResult) => void = () => undefined
    platform.updateIntegration.mockReturnValue(
      new Promise<InstallResult>((resolve) => {
        finish = resolve
      }),
    )
    const { card } = renderCard(updateTool())
    const button = card.getByRole('button', { name: 'Update to 1.30.0' })

    await userEvent.click(button)
    await userEvent.click(dialog('Update mage-x to 1.30.0?').getByRole('button', { name: 'Update' }))

    expect(button).toBeDisabled()
    expect(card.getByRole('switch', { name: 'mage-x' })).toBeDisabled()

    finish(installResult({ message: 'Updated magex to 1.30.0' }))

    expect(await card.findByText('Updated magex to 1.30.0')).toBeInTheDocument()
    await waitFor(() => {
      expect(button).toBeEnabled()
    })
  })

  it('says why an install failed, until the next change', async () => {
    const failed = 'The download’s checksum doesn’t match the release’s. Nothing on your Mac changed.'
    platform.updateIntegration.mockRejectedValue(new Error(failed))
    const { card, onTools } = renderCard(updateTool())

    await userEvent.click(card.getByRole('button', { name: 'Update to 1.30.0' }))
    await userEvent.click(dialog('Update mage-x to 1.30.0?').getByRole('button', { name: 'Update' }))

    expect(await card.findByRole('alert')).toHaveTextContent(failed)
    expect(onTools).not.toHaveBeenCalled()

    await userEvent.click(card.getByRole('switch', { name: 'mage-x' }))

    expect(card.queryByText(failed)).not.toBeInTheDocument()
  })

  it('says what the install did until the next change', async () => {
    const { card } = renderCard(updateTool())

    await userEvent.click(card.getByRole('button', { name: 'Update to 1.30.0' }))
    await userEvent.click(dialog('Update mage-x to 1.30.0?').getByRole('button', { name: 'Update' }))

    expect(await card.findByText('Updated magex to 1.30.0')).toBeInTheDocument()

    await userEvent.click(card.getByRole('switch', { name: 'mage-x' }))

    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('mage-x', 'off')
    expect(card.queryByText('Updated magex to 1.30.0')).not.toBeInTheDocument()
  })
})

describe('IntegrationCard turning a missing tool on', () => {
  it('asks to install it, and stores nothing when cancelled', async () => {
    const { card } = renderCard(missingTool())
    const toggle = card.getByRole('switch', { name: 'mage-x' })

    await userEvent.click(toggle)
    await userEvent.click(dialog('Install mage-x 1.30.0?').getByRole('button', { name: 'Cancel' }))

    expect(toggle).not.toBeChecked()
    expect(card.getByText('Off')).toBeInTheDocument()
    expect(platform.setIntegrationEnabled).not.toHaveBeenCalled()
    expect(platform.installIntegration).not.toHaveBeenCalled()
  })

  it('turns it on, then installs it, once the user confirms', async () => {
    const { card, onSaved, reload, onTools } = renderCard(missingTool())

    await userEvent.click(card.getByRole('switch', { name: 'mage-x' }))
    await userEvent.click(dialog('Install mage-x 1.30.0?').getByRole('button', { name: 'Install' }))

    expect(await card.findByText('Installed mage-x 1.30.0 in ~/.local/bin')).toBeInTheDocument()
    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('mage-x', 'on')
    expect(platform.installIntegration).toHaveBeenCalledExactlyOnceWith('mage-x')
    expect(platform.setIntegrationEnabled.mock.invocationCallOrder[0]).toBeLessThan(
      platform.installIntegration.mock.invocationCallOrder[0] ?? 0,
    )
    expect(onSaved).toHaveBeenCalledExactlyOnceWith(withSwitch('on'))
    expect(reload).toHaveBeenCalledOnce()
    expect(onTools).toHaveBeenCalledExactlyOnceWith(integrationTools())
  })

  it('installs nothing when the switch can’t be stored', async () => {
    platform.setIntegrationEnabled.mockRejectedValue(new Error('database is locked'))
    const { card } = renderCard(missingTool())
    const toggle = card.getByRole('switch', { name: 'mage-x' })

    await userEvent.click(toggle)
    await userEvent.click(dialog('Install mage-x 1.30.0?').getByRole('button', { name: 'Install' }))

    expect(await card.findByRole('alert')).toHaveTextContent('database is locked')
    expect(toggle).toHaveAccessibleDescription(/database is locked$/)
    expect(toggle).not.toBeChecked()
    expect(platform.installIntegration).not.toHaveBeenCalled()
  })

  it('only turns it on when it can’t be installed now', async () => {
    const { card, onSaved } = renderCard(missingTool({ blocked: 'A run is using magex. Update it when the run ends.' }))

    await userEvent.click(card.getByRole('switch', { name: 'mage-x' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('mage-x', 'on')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(withSwitch('on'))
    })
    expect(platform.installIntegration).not.toHaveBeenCalled()
  })
})

describe('IntegrationCard PATH note', () => {
  it.each<[string, string, string]>([
    ['~/.local/bin/magex', '~/.local/bin', 'export PATH="$HOME/.local/bin:$PATH"'],
    ['~/magex', '~', 'export PATH="$HOME:$PATH"'],
    ['/opt/tools/bin/magex', '/opt/tools/bin', 'export PATH="/opt/tools/bin:$PATH"'],
    ['~/my "$tools"/magex', '~/my "$tools"', 'export PATH="$HOME/my \\"\\$tools\\":$PATH"'],
  ])('says how to put %s on the PATH, with the line to copy', async (displayPath, folder, line) => {
    const { card } = renderCard(appTool({ copy: appCopy({ displayPath, onPath: false }) }))

    expect(card.getByText(/^Your terminal won’t find/)).toHaveTextContent(
      `Your terminal won’t find magex yet. Add ${folder} to your PATH: put this line in ~/.zprofile.`,
    )
    expect(card.getByText(line)).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Copy the command' }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(line)
  })

  it('says nothing of the PATH for a copy the terminal finds, or one the app didn’t install', () => {
    renderCard(appTool())
    renderCard(goPreCommitTool({ copy: toolCopy({ onPath: false }) }))

    expect(screen.queryByText(/your PATH/)).not.toBeInTheDocument()
  })
})

describe('IntegrationCard details of a copy the app installed', () => {
  it('says when and from what it was installed, and reinstalls it once the user confirms', async () => {
    const { card, onTools } = renderCard(appTool())

    await openDetails()

    expect(card.getAllByRole('definition').map((d) => d.textContent)).toEqual([
      '~/.local/bin/magex',
      `installed by ${brand.name}`,
      '1.29.5',
    ])
    expect(card.getByText(/^Installed/)).toHaveTextContent(
      `Installed ${formatDateTime('2026-06-15T11:40:00Z')} from mage-x_1.29.5_darwin_arm64.tar.gz`,
    )

    await userEvent.click(card.getByRole('button', { name: 'Reinstall' }))
    await userEvent.click(dialog('Reinstall mage-x 1.29.5?').getByRole('button', { name: 'Reinstall' }))

    expect(platform.reinstallIntegration).toHaveBeenCalledExactlyOnceWith('mage-x')
    expect(await card.findByText('Reinstalled mage-x 1.29.5')).toBeInTheDocument()
    expect(onTools).toHaveBeenCalledExactlyOnceWith(integrationTools())
  })

  it.each<[string, IntegrationTool]>([
    [
      'it can’t be done now',
      appTool({ install: installPlan({ blocked: 'A run is using magex. Update it when the run ends.' }) }),
    ],
    ['the card updates it instead', updateTool()],
  ])('offers no reinstall when %s', async (_, tool) => {
    const { card } = renderCard(tool)

    await openDetails()

    expect(card.queryByRole('button', { name: 'Reinstall' })).not.toBeInTheDocument()
    expect(card.getByRole('button', { name: 'Move to the Trash' })).toBeEnabled()
  })

  it('holds its controls while an install runs', async () => {
    const { card } = renderCard(appTool({ progress: { step: 'trying', done: 0, total: 0 } }))

    await openDetails()

    expect(card.getByText('Trying it…')).toBeInTheDocument()
    expect(card.getByRole('button', { name: 'Reinstall' })).toBeDisabled()
    expect(card.getByRole('button', { name: 'Move to the Trash' })).toBeDisabled()
    expect(card.getByRole('button', { name: 'Choose…' })).toBeDisabled()
  })

  it('moves it to the Trash once the user confirms, showing the tools as it left them', async () => {
    const left = integrationTools({ tools: [missingTool()] })
    platform.trashToolCopy.mockResolvedValue(left)
    const { card, onTools } = renderCard(appTool())

    await openDetails()
    await userEvent.click(card.getByRole('button', { name: 'Move to the Trash' }))

    const confirmation = screen.getByRole('dialog', { name: 'Move ~/.local/bin/magex to the Trash?' })
    expect(confirmation).toHaveAccessibleDescription('Finder’s Put Back can restore it.')

    await userEvent.click(within(confirmation).getByRole('button', { name: 'Move to the Trash' }))

    expect(platform.trashToolCopy).toHaveBeenCalledExactlyOnceWith('mage-x', '/Users/example/.local/bin/magex')
    await waitFor(() => {
      expect(onTools).toHaveBeenCalledExactlyOnceWith(left)
    })
  })

  it('moves the copy the terminal runs instead to the Trash', async () => {
    const { card, onTools } = renderCard(
      integrationTool({
        others: [
          toolCopy({
            path: '/Users/example/go/bin/magex',
            displayPath: '~/go/bin/magex',
            source: 'goInstall',
            version: { raw: '1.28.0', commit: '', built: '' },
            shadowing: true,
          }),
        ],
      }),
    )

    await openDetails()
    await userEvent.click(card.getByRole('button', { name: 'Move it to the Trash' }))
    await userEvent.click(
      dialog('Move ~/go/bin/magex to the Trash?').getByRole('button', { name: 'Move to the Trash' }),
    )

    expect(platform.trashToolCopy).toHaveBeenCalledExactlyOnceWith('mage-x', '/Users/example/go/bin/magex')
    await waitFor(() => {
      expect(onTools).toHaveBeenCalledOnce()
    })
  })

  it('offers to show the copy in Finder when it can’t be moved to the Trash', async () => {
    const refused = `${brand.name} isn’t allowed to control Finder, so it can’t move this to the Trash.`
    platform.trashToolCopy.mockRejectedValue(new Error(refused))
    const { card, onTools } = renderCard(appTool())

    await openDetails()
    await userEvent.click(card.getByRole('button', { name: 'Move to the Trash' }))
    await userEvent.click(
      dialog('Move ~/.local/bin/magex to the Trash?').getByRole('button', { name: 'Move to the Trash' }),
    )

    expect(await card.findByRole('alert')).toHaveTextContent(refused)
    expect(onTools).not.toHaveBeenCalled()

    await userEvent.click(card.getByRole('button', { name: 'Reveal in Finder' }))

    expect(platform.revealToolCopy).toHaveBeenCalledExactlyOnceWith('mage-x', '/Users/example/.local/bin/magex')
  })
})

// selfUpdating is mage-x from a GitHub release in ~/.local/bin, with 1.30.0 out, which
// updates itself, its last update and its progress as given.
function selfUpdating(selfUpdate: Partial<IntegrationTool['selfUpdate']> = {}, progress = idle): IntegrationTool {
  return integrationTool({
    state: 'update',
    latest: latest130,
    install: installPlan({
      action: 'update',
      version: '1.30.0',
      archive: 'mage-x_1.30.0_darwin_arm64.tar.gz',
      foreign: false,
      updater: '~/.local/bin/magex update:install',
    }),
    progress,
    selfUpdate: { run: 0, running: false, updated: false, failed: false, message: '', ...selfUpdate },
  })
}

// updating is the progress of a tool whose own updater runs.
const updating: InstallProgress = { step: 'updating', done: 0, total: 0 }

// renderSelfUpdate renders mage-x's card as tool shows it, and returns a way to show the
// card as the tools are read again.
function renderSelfUpdate(tool: IntegrationTool) {
  const card = (shown: IntegrationTool) => (
    <IntegrationCard
      tool={shown}
      checking={false}
      onSaved={vi.fn()}
      reload={() => Promise.resolve()}
      onTools={vi.fn()}
      terminal="iterm2"
    />
  )
  const { rerender } = render(card(tool))

  return {
    card: within(screen.getByRole('region', { name: 'mage-x' })),
    show: (next: IntegrationTool) => {
      rerender(card(next))
    },
  }
}

describe('IntegrationCard updates a tool runs itself', () => {
  it('asks first, saying what the tool’s own updater does, and never asks to replace its copy', async () => {
    const tool = selfUpdating()
    const { card } = renderSelfUpdate({ ...tool, install: { ...tool.install, foreign: true } })

    await userEvent.click(card.getByRole('button', { name: 'Update to 1.30.0' }))

    const asked = dialog('Update mage-x to 1.30.0?')
    expect(asked.getByText(/^magex updates itself:/)).toHaveTextContent(
      `magex updates itself: ${brand.name} runs ~/.local/bin/magex update:install, which downloads 1.30.0 from the acme-org/mage-x release on GitHub, checks it against the release’s SHA-256 checksums, and swaps it in for the copy. Its output shows here as it runs.`,
    )
    expect(asked.getByText(/^Replaces/)).toHaveTextContent('Replaces magex 1.29.5 at ~/.local/bin/magex.')
    expect(asked.queryByText(/wasn’t installed by/)).not.toBeInTheDocument()
    expect(asked.queryByRole('button', { name: 'Replace' })).not.toBeInTheDocument()

    await userEvent.click(asked.getByRole('button', { name: 'Cancel' }))

    expect(platform.updateIntegration).not.toHaveBeenCalled()
  })

  it('names the latest release before it is known', async () => {
    const tool = selfUpdating()
    const { card } = renderSelfUpdate({ ...tool, install: { ...tool.install, version: '' } })

    await userEvent.click(card.getByRole('button', { name: 'Update mage-x' }))

    expect(dialog('Update mage-x?').getByText(/which downloads its latest release from/)).toBeInTheDocument()
  })

  it('starts the run, shows it, then says what it did once the tool was looked for again', async () => {
    platform.updateIntegration.mockResolvedValue(installResult({ message: '', run: 7 }))
    platform.getScriptRun.mockResolvedValue(
      scriptRun({ id: 7, kind: 'update', title: 'Update magex', status: 'running', dir: '~/.local/bin' }),
    )
    const { card, show } = renderSelfUpdate(selfUpdating())

    await userEvent.click(card.getByRole('button', { name: 'Update to 1.30.0' }))
    await userEvent.click(dialog('Update mage-x to 1.30.0?').getByRole('button', { name: 'Update' }))

    expect(await card.findByRole('heading', { name: 'Update magex' })).toBeInTheDocument()
    expect(platform.getScriptRun).toHaveBeenCalledWith(7)
    expect(card.queryByText(/^Updated/)).not.toBeInTheDocument()

    show(selfUpdating({ run: 7, running: true }, updating))

    expect(card.getByText('magex is updating itself…')).toBeInTheDocument()
    expect(card.queryByRole('button', { name: 'Show the run' })).not.toBeInTheDocument()
    expect(card.getByRole('button', { name: 'Update to 1.30.0' })).toBeDisabled()

    show(
      integrationTool({
        copy: toolCopy({ version: { raw: '1.30.0', commit: '', built: '' } }),
        selfUpdate: { run: 7, running: false, updated: true, failed: false, message: 'Updated magex to 1.30.0' },
      }),
    )

    expect(card.getByText('Updated magex to 1.30.0')).toBeInTheDocument()
    expect(card.getByText(/^magex 1\.30\.0 · ~\/\.local\/bin\/magex/)).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Close the run' }))

    expect(card.queryByRole('heading', { name: 'Update magex' })).not.toBeInTheDocument()
    expect(card.queryByText('Updated magex to 1.30.0')).not.toBeInTheDocument()
  })

  it('says why the tool’s own update didn’t update it', async () => {
    const failed = 'magex couldn’t update itself: update failed: checksum mismatch.'
    platform.updateIntegration.mockResolvedValue(installResult({ message: '', run: 3 }))
    const { card, show } = renderSelfUpdate(selfUpdating())

    await userEvent.click(card.getByRole('button', { name: 'Update to 1.30.0' }))
    await userEvent.click(dialog('Update mage-x to 1.30.0?').getByRole('button', { name: 'Update' }))
    await waitFor(() => {
      expect(platform.getScriptRun).toHaveBeenCalledWith(3)
    })

    show(selfUpdating({ run: 3, running: false, updated: false, failed: true, message: failed }))

    expect(card.getByRole('alert')).toHaveTextContent(failed)
  })

  it('says, without an error, that the tool was already up to date', async () => {
    const current = 'magex is already up to date (1.29.5)'
    platform.updateIntegration.mockResolvedValue(installResult({ message: '', run: 6 }))
    const { card, show } = renderSelfUpdate(selfUpdating())

    await userEvent.click(card.getByRole('button', { name: 'Update to 1.30.0' }))
    await userEvent.click(dialog('Update mage-x to 1.30.0?').getByRole('button', { name: 'Update' }))
    await waitFor(() => {
      expect(platform.getScriptRun).toHaveBeenCalledWith(6)
    })

    show(selfUpdating({ run: 6, running: false, updated: false, failed: false, message: current }))

    expect(card.getByText(current)).toBeInTheDocument()
    expect(card.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('offers no run to show before its ID is known', () => {
    const { card } = renderSelfUpdate(selfUpdating({ run: 0, running: true }, updating))

    expect(card.getByText('magex is updating itself…')).toBeInTheDocument()
    expect(card.queryByRole('button', { name: 'Show the run' })).not.toBeInTheDocument()
  })

  it('follows no run an update didn’t start', async () => {
    platform.updateIntegration.mockResolvedValue(installResult({ message: '', run: 0 }))
    const { card } = renderSelfUpdate(selfUpdating())

    await userEvent.click(card.getByRole('button', { name: 'Update to 1.30.0' }))
    await userEvent.click(dialog('Update mage-x to 1.30.0?').getByRole('button', { name: 'Update' }))

    await waitFor(() => {
      expect(platform.updateIntegration).toHaveBeenCalledOnce()
    })
    expect(card.queryByRole('button', { name: 'Close the run' })).not.toBeInTheDocument()
    expect(platform.getScriptRun).not.toHaveBeenCalled()
  })

  it('shows a run it didn’t start on request, and forgets it at the next change', async () => {
    const { card } = renderSelfUpdate(selfUpdating({ run: 4, running: true }, updating))

    expect(card.getByText('magex is updating itself…')).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Show the run' }))

    expect(platform.getScriptRun).toHaveBeenCalledWith(4)
    expect(card.queryByRole('button', { name: 'Show the run' })).not.toBeInTheDocument()

    await userEvent.click(card.getByRole('switch', { name: 'mage-x' }))

    await waitFor(() => {
      expect(card.queryByRole('button', { name: 'Close the run' })).not.toBeInTheDocument()
    })
    expect(card.getByRole('button', { name: 'Show the run' })).toBeInTheDocument()
  })

  it('says nothing of a run it isn’t following', () => {
    const { card } = renderSelfUpdate(
      selfUpdating({ run: 5, running: false, updated: true, message: 'Updated magex to 1.30.0' }),
    )

    expect(card.queryByText('Updated magex to 1.30.0')).not.toBeInTheDocument()
    expect(card.queryByRole('button', { name: 'Show the run' })).not.toBeInTheDocument()
    expect(platform.getScriptRun).not.toHaveBeenCalled()
  })
})
