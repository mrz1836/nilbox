import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { IntegrationSwitch, IntegrationTools, Settings } from '../../platform'
import { goPreCommitTool, integrationsConfig, integrationTool, integrationTools, settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ScriptsCard } from './ScriptsCard'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// off is a tool switched off.
const off = { switch: 'off' as const, on: false }

// stored is the settings with the Scripts switch set to set.
function stored(set: IntegrationSwitch = 'auto'): Settings {
  return settings({
    integrations: integrationsConfig({ switches: { ...integrationsConfig().switches, scripts: set } }),
  })
}

// renderCard renders the card as the settings page holds it: the settings it saves come
// back as the stored ones.
function renderCard(saved: Settings = stored(), tools: IntegrationTools | null = integrationTools()) {
  const onSaved = vi.fn<(settings: Settings) => void>()

  function Card() {
    const [current, setCurrent] = useState(saved)

    return (
      <ScriptsCard
        settings={current}
        onSaved={(next) => {
          onSaved(next)
          setCurrent(next)
        }}
        tools={tools}
      />
    )
  }

  render(<Card />)

  return { onSaved, card: within(screen.getByRole('region', { name: 'Scripts' })) }
}

describe('ScriptsCard', () => {
  it('is on automatically while a tool is on, and says what it is for', () => {
    const { card } = renderCard()

    expect(
      card.getByText('Run your repositories’ t3.json scripts, VS Code tasks, and magex targets from the Scripts tab.'),
    ).toBeInTheDocument()
    expect(card.getByRole('switch', { name: 'Scripts' })).toBeChecked()
    expect(card.getByText('On · automatic')).toBeInTheDocument()
    expect(card.getByText('Automatic turns it on while mage-x or go-pre-commit is on.')).toBeInTheDocument()
    expect(card.getByRole('checkbox', { name: 'Show magex targets' })).toBeChecked()
    expect(card.getByRole('checkbox', { name: 'Read t3.json and VS Code tasks' })).toBeChecked()
  })

  it('is off automatically while no tool is on, and before the tools are listed', () => {
    const { card } = renderCard(stored(), integrationTools({ tools: [integrationTool(off), goPreCommitTool(off)] }))

    expect(card.getByRole('switch', { name: 'Scripts' })).not.toBeChecked()
    expect(card.getByText('Off · automatic')).toBeInTheDocument()
  })

  it('waits for the tools before an automatic switch can be flipped', async () => {
    const { card } = renderCard(stored(), null)
    const toggle = card.getByRole('switch', { name: 'Scripts' })

    expect(toggle).toBeDisabled()
    expect(toggle).toHaveAttribute('title', 'Waiting for the tools to be listed')
    expect(card.getByText('Automatic')).toBeInTheDocument()

    await userEvent.click(toggle)
    expect(platform.setIntegrationEnabled).not.toHaveBeenCalled()
  })

  it('lets a switch the user set be flipped before the tools are listed', async () => {
    const { card } = renderCard(stored('on'), null)
    const toggle = card.getByRole('switch', { name: 'Scripts' })

    expect(toggle).toBeEnabled()
    expect(toggle).toBeChecked()
    await userEvent.click(toggle)

    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('scripts', 'off')
  })

  it('stores the switch flipped, for good', async () => {
    const { card, onSaved } = renderCard()

    await userEvent.click(card.getByRole('switch', { name: 'Scripts' }))

    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('scripts', 'off')
    expect(onSaved).toHaveBeenCalledOnce()
    expect(card.getByText('Off')).toBeInTheDocument()
    expect(card.queryByText(/Automatic turns it on/)).not.toBeInTheDocument()
  })

  it('shows a switch the user set, and turns it on', async () => {
    const { card } = renderCard(stored('off'))

    expect(card.getByText('Off')).toBeInTheDocument()
    await userEvent.click(card.getByRole('switch', { name: 'Scripts' }))

    expect(platform.setIntegrationEnabled).toHaveBeenCalledExactlyOnceWith('scripts', 'on')
    expect(card.getByText('On')).toBeInTheDocument()
  })

  it('stores which sources Repo scripts reads', async () => {
    const { card, onSaved } = renderCard()

    await userEvent.click(card.getByRole('checkbox', { name: 'Show magex targets' }))
    expect(platform.setScriptSources).toHaveBeenLastCalledWith(false, true)
    expect(card.getByRole('checkbox', { name: 'Show magex targets' })).not.toBeChecked()

    await userEvent.click(card.getByRole('checkbox', { name: 'Read t3.json and VS Code tasks' }))
    expect(platform.setScriptSources).toHaveBeenLastCalledWith(false, false)
    expect(onSaved).toHaveBeenCalledTimes(2)
  })

  it('says why a change was refused, beside what changed', async () => {
    platform.setScriptSources.mockRejectedValueOnce(new Error('database is locked'))
    platform.setIntegrationEnabled.mockRejectedValueOnce(new Error('disk is full'))
    const { card } = renderCard()

    await userEvent.click(card.getByRole('checkbox', { name: 'Read t3.json and VS Code tasks' }))
    expect(await card.findByRole('alert')).toHaveTextContent('database is locked')
    expect(card.getByRole('checkbox', { name: 'Read t3.json and VS Code tasks' })).toBeChecked()
    expect(card.getByRole('group', { name: 'Repo scripts lists' })).toHaveAccessibleDescription('database is locked')

    await userEvent.click(card.getByRole('switch', { name: 'Scripts' }))
    expect(await card.findByRole('alert')).toHaveTextContent('disk is full')
    expect(card.getByRole('switch', { name: 'Scripts' })).toHaveAccessibleDescription(/disk is full$/)
  })
})
