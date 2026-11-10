import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { IntegrationTools, Settings } from '../../platform'
import { goPreCommitTool, installPlan, installResult, integrationTool, integrationTools } from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import { IntegrationsSection } from './IntegrationsSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const shellWarning =
  'Couldn’t read your shell’s environment, so runs use a basic PATH, and your ssh agent may be missing.'

function renderSection() {
  const onSaved = vi.fn<(settings: Settings) => void>()

  render(<IntegrationsSection onSaved={onSaved} />)

  return { onSaved }
}

// cards lists the tools' cards, by name, in the order the section shows them.
function cards(): string[] {
  return screen.getAllByRole('region').map((r) => within(r).getByRole('heading').textContent)
}

describe('IntegrationsSection', () => {
  it('says what the tools are, then shows a card for each, in order', async () => {
    renderSection()

    expect(
      screen.getByText(
        `Tools ${brand.name} runs for you. Each one is found on this Mac, or installed from its GitHub release. ${brand.name} never uses go install or sudo.`,
      ),
    ).toBeInTheDocument()
    expect(screen.getByText('Looking for the tools…')).toBeInTheDocument()

    await waitFor(() => {
      expect(cards()).toEqual(['mage-x', 'go-pre-commit'])
    })
    expect(screen.queryByText('Looking for the tools…')).not.toBeInTheDocument()
    expect(screen.queryByText(shellWarning)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeEnabled()
  })

  it('shows what the last run found while it looks again', async () => {
    platform.listIntegrations.mockResolvedValue(
      integrationTools({
        tools: [integrationTool({ checking: true }), goPreCommitTool({ checking: true, state: 'missing' })],
        shellChecked: false,
        shellRead: false,
      }),
    )
    renderSection()

    const mageX = within(await screen.findByRole('region', { name: 'mage-x' }))
    expect(mageX.getByText('magex 1.29.5 · ~/.local/bin/magex · GitHub release')).toBeInTheDocument()
    expect(mageX.getByText('Checking…')).toBeInTheDocument()
    expect(screen.getAllByText('Checking…')).toHaveLength(2)
    // The shell is still being read, so nothing is said about it yet.
    expect(screen.queryByText(shellWarning)).not.toBeInTheDocument()
  })

  it('reads the tools again when a look for them ends', async () => {
    renderSection()

    await screen.findByRole('region', { name: 'mage-x' })
    platform.listIntegrations.mockResolvedValue(integrationTools({ tools: [integrationTool({ state: 'missing' })] }))
    act(() => {
      emit('tools:updated')
    })

    await waitFor(() => {
      expect(cards()).toEqual(['mage-x'])
    })
    expect(screen.getByText('Not on this Mac')).toBeInTheDocument()
  })

  it('says why the tools could not be listed', async () => {
    platform.listIntegrations.mockRejectedValue(new Error('database is locked'))
    renderSection()

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t list the tools: database is locked')
    expect(screen.queryByText('Looking for the tools…')).not.toBeInTheDocument()
    expect(screen.queryByRole('region')).not.toBeInTheDocument()
  })

  it('checks again, busy until it is done, showing what it found', async () => {
    let finish: (tools: IntegrationTools) => void = () => undefined
    platform.checkIntegrationsAgain.mockReturnValue(
      new Promise<IntegrationTools>((resolve) => {
        finish = resolve
      }),
    )
    renderSection()

    await screen.findByRole('region', { name: 'mage-x' })
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(platform.checkIntegrationsAgain).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()
    // Each card says it is checking, beside what it showed.
    expect(within(screen.getByRole('region', { name: 'go-pre-commit' })).getByText('Checking…')).toBeInTheDocument()

    act(() => {
      finish(integrationTools({ tools: [integrationTool({ state: 'update' })] }))
    })

    expect(await screen.findByRole('button', { name: 'Check again' })).toBeEnabled()
    expect(cards()).toEqual(['mage-x'])
    expect(screen.queryByText('Checking…')).not.toBeInTheDocument()
  })

  it('says why checking again failed', async () => {
    platform.checkIntegrationsAgain.mockRejectedValue(new Error('the shell timed out'))
    renderSection()

    await screen.findByRole('region', { name: 'mage-x' })
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the shell timed out')
    expect(cards()).toEqual(['mage-x', 'go-pre-commit'])
  })

  it('says when the shell’s environment couldn’t be read, with a way to try again', async () => {
    platform.listIntegrations.mockResolvedValue(integrationTools({ shellChecked: true, shellRead: false }))
    platform.checkIntegrationsAgain.mockResolvedValue(integrationTools())
    renderSection()

    const warning = await screen.findByRole('note')
    expect(warning).toHaveTextContent(shellWarning)
    // The warning opens the section, before what the tools are.
    expect(warning.compareDocumentPosition(screen.getByText(/^Tools /))).toBe(Node.DOCUMENT_POSITION_FOLLOWING)

    await userEvent.click(within(warning).getByRole('button', { name: 'Check again' }))

    expect(platform.checkIntegrationsAgain).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(screen.queryByText(shellWarning)).not.toBeInTheDocument()
    })
  })

  it('flips a tool’s switch for good, and shows the tools as read again', async () => {
    const { onSaved } = renderSection()
    const toggle = await screen.findByRole('switch', { name: 'go-pre-commit' })

    platform.listIntegrations.mockResolvedValue(
      integrationTools({ tools: [integrationTool(), goPreCommitTool({ switch: 'off', on: false })] }),
    )
    await userEvent.click(toggle)

    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('go-pre-commit', 'off')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(onSaved.mock.lastCall?.[0].integrations.switches['go-pre-commit']).toBe('off')
    await waitFor(() => {
      expect(toggle).toBeEnabled()
    })
    expect(toggle).not.toBeChecked()
    expect(within(screen.getByRole('region', { name: 'go-pre-commit' })).getByText('Off')).toBeInTheDocument()
    expect(platform.listIntegrations).toHaveBeenCalledTimes(2)
  })

  it('installs a tool, and shows the tools as the install left them', async () => {
    platform.listIntegrations.mockResolvedValue(
      integrationTools({
        tools: [
          integrationTool({
            state: 'missing',
            on: false,
            install: installPlan({
              action: 'install',
              version: '1.30.0',
              replaces: '',
              replacesPath: '',
              foreign: false,
            }),
          }),
        ],
      }),
    )
    platform.installIntegration.mockResolvedValue(
      installResult({ tools: integrationTools({ tools: [integrationTool()] }) }),
    )
    renderSection()

    const card = within(await screen.findByRole('region', { name: 'mage-x' }))
    await userEvent.click(card.getByRole('button', { name: 'Install mage-x 1.30.0' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Install' }))

    expect(await card.findByText('Installed mage-x 1.30.0 in ~/.local/bin')).toBeInTheDocument()
    expect(card.getByText('magex 1.29.5 · ~/.local/bin/magex · GitHub release')).toBeInTheDocument()
    expect(card.queryByRole('button', { name: /^install/i })).not.toBeInTheDocument()
    expect(platform.installIntegration).toHaveBeenCalledExactlyOnceWith('mage-x')
    // The install returned the tools, so the list is not read again.
    expect(platform.listIntegrations).toHaveBeenCalledOnce()
  })
})
