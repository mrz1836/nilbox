import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import {
  goPreCommitTool,
  integrationsConfig,
  integrationTool,
  integrationTools,
  settings,
  setupSignals,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { integrationsOf } from '../../test/query'
import { ToolsStep } from './ToolsStep'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

describe('ToolsStep', () => {
  it('says why it is suggested, and shows each tool’s card', async () => {
    const onSaved = vi.fn()

    render(
      <ToolsStep
        settings={settings()}
        onSaved={onSaved}
        integrations={integrationsOf(
          integrationTools({ tools: [integrationTool(), goPreCommitTool({ state: 'missing' })] }),
        )}
        signals={setupSignals({ mageX: 3, goModules: 7 })}
      />,
    )

    expect(
      screen.getByText('Suggested because mage-x is installed · 3 clones use mage-x · 7 are Go modules.'),
    ).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: /mage-x/ })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: /go-pre-commit/ })).toBeInTheDocument()

    // Reaching the step counts as the tools being announced, once.
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalled()
    })
    expect(platform.setIntegrationsAnnounced).toHaveBeenCalledExactlyOnceWith(true)
  })

  it('looks for the tools while they load, and says nothing of a reason it hasn’t got', () => {
    render(<ToolsStep settings={null} onSaved={vi.fn()} integrations={integrationsOf(null)} signals={null} />)

    expect(screen.getByRole('status')).toHaveTextContent('Looking for your tools…')
    expect(screen.queryByText(/Suggested because/)).not.toBeInTheDocument()
    expect(platform.setIntegrationsAnnounced).not.toHaveBeenCalled()
  })

  it('announces nothing twice', () => {
    render(
      <ToolsStep
        settings={settings({ integrations: integrationsConfig({ announced: true }) })}
        onSaved={vi.fn()}
        integrations={integrationsOf(integrationTools())}
        signals={setupSignals()}
      />,
    )

    expect(platform.setIntegrationsAnnounced).not.toHaveBeenCalled()
  })

  it('says why the tools couldn’t be listed', () => {
    render(
      <ToolsStep
        settings={settings()}
        onSaved={vi.fn()}
        integrations={integrationsOf(null, { error: 'the finder is gone' })}
        signals={null}
      />,
    )

    expect(screen.getByText('Couldn’t list the tools: the finder is gone')).toBeInTheDocument()
    expect(screen.queryByText('Looking for your tools…')).not.toBeInTheDocument()
  })

  it('logs a store of the announcement that failed', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const locked = new Error('locked')
    platform.setIntegrationsAnnounced.mockRejectedValue(locked)

    render(
      <ToolsStep
        settings={settings()}
        onSaved={vi.fn()}
        integrations={integrationsOf(integrationTools())}
        signals={setupSignals()}
      />,
    )

    await waitFor(() => {
      expect(error).toHaveBeenCalledWith('could not remember that the tools were announced:', locked)
    })
  })
})
