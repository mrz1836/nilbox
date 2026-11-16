import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { AutopilotConfig, Settings } from '../../platform'
import { at } from '../../test/clock'
import { autopilotEngine, settings } from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import { AutopilotSection, lastCheckedText } from './AutopilotSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// withAutopilot is the fixture's settings with Autopilot's changed.
function withAutopilot(change: Partial<AutopilotConfig>): Settings {
  return settings({ autopilot: { ...settings().autopilot, ...change } })
}

// Harness keeps the settings as a settings page does: each one stored becomes the one shown.
function Harness({ initial, onSaved }: { initial: Settings; onSaved: (settings: Settings) => void }) {
  const [current, setCurrent] = useState(initial)

  return (
    <AutopilotSection
      settings={current}
      onSaved={(next) => {
        onSaved(next)
        setCurrent(next)
      }}
    />
  )
}

function renderSection(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<Harness initial={current} onSaved={onSaved} />, { wrapper: at() })

  return { onSaved }
}

// labels lists a select's options as they read.
function labels(name: string): string[] {
  return within(screen.getByRole('combobox', { name }))
    .getAllByRole('option')
    .map((o) => o.textContent)
}

describe('AutopilotSection', () => {
  it('shows the settings as stored, the defaults marked', async () => {
    renderSection()

    expect(screen.getByRole('switch', { name: 'Pause Autopilot' })).not.toBeChecked()
    expect(screen.getByRole('radio', { name: 'Ask first' })).toBeChecked()
    expect(screen.getByRole('combobox', { name: 'Agents at once' })).toHaveValue('2')
    expect(labels('Agents at once')).toEqual(['1 agent', '2 agents (default)', '3 agents'])
    expect(screen.getByRole('combobox', { name: 'Agent runs a day' })).toHaveValue('20')
    expect(screen.getByRole('combobox', { name: 'GitHub actions a day' })).toHaveValue('50')
    expect(screen.getByRole('combobox', { name: 'Daily cap' })).toHaveValue('off')
    expect(screen.queryByRole('spinbutton')).not.toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Keep the Mac awake while agents work' })).not.toBeChecked()
    expect(
      screen.getByRole('switch', { name: 'Hold agent starts while their provider reports an outage' }),
    ).toBeChecked()
    expect(screen.getByRole('combobox', { name: 'Ask before catching up after' })).toHaveValue('24')
    expect(labels('Ask before catching up after')).toContain('1 week')
    expect(screen.getByRole('combobox', { name: 'History kept' })).toHaveValue('90')
    expect(screen.getByRole('combobox', { name: 'Run logs kept' })).toHaveValue('30')

    expect(await screen.findByText('Last checked today, 11:59 AM · 34 ms · 7 matches · 1 start')).toBeInTheDocument()
  })

  it('offers a stored value that isn’t among the choices', () => {
    renderSection(withAutopilot({ runsPerDay: 37 }))

    expect(screen.getByRole('combobox', { name: 'Agent runs a day' })).toHaveValue('37')
    expect(labels('Agent runs a day')).toContain('37 runs')
  })

  it('pauses Autopilot, and stores the settings the setter returns', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(screen.getByRole('switch', { name: 'Pause Autopilot' }))

    await waitFor(() => {
      expect(platform.setAutopilotPaused).toHaveBeenCalledExactlyOnceWith(true)
    })
    expect(onSaved).toHaveBeenCalledWith(withAutopilot({ paused: true }))
    expect(screen.getByRole('switch', { name: 'Pause Autopilot' })).toBeChecked()
  })

  it('stores the mode new routines start in', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(screen.getByRole('radio', { name: 'Dry run' }))

    await waitFor(() => {
      expect(platform.setAutopilotDefaultMode).toHaveBeenCalledExactlyOnceWith('dryRun')
    })
    expect(onSaved).toHaveBeenCalledWith(withAutopilot({ defaultMode: 'dryRun' }))
  })

  it.each([
    ['Agents at once', '3', 'setAutopilotAgentsAtOnce', 3, { agentsAtOnce: 3 }],
    ['Agent runs a day', '50', 'setAutopilotRunsPerDay', 50, { runsPerDay: 50 }],
    ['GitHub actions a day', '10', 'setAutopilotActionsPerDay', 10, { actionsPerDay: 10 }],
    ['Ask before catching up after', '168', 'setAutopilotCatchUpHours', 168, { catchUpHours: 168 }],
    ['Run logs kept', '60', 'setAutopilotLogDays', 60, { logDays: 60 }],
  ] as const)('stores %s as it is chosen', async (name, value, setter, sent, change) => {
    const { onSaved } = renderSection()

    await userEvent.selectOptions(screen.getByRole('combobox', { name }), value)

    await waitFor(() => {
      expect(platform[setter]).toHaveBeenCalledExactlyOnceWith(sent)
    })
    expect(onSaved).toHaveBeenCalledWith(withAutopilot(change))
  })

  it('stores both switches of what holds an agent back', async () => {
    renderSection()

    await userEvent.click(screen.getByRole('switch', { name: 'Keep the Mac awake while agents work' }))
    await userEvent.click(
      screen.getByRole('switch', { name: 'Hold agent starts while their provider reports an outage' }),
    )

    await waitFor(() => {
      expect(platform.setAutopilotHoldOnOutage).toHaveBeenCalledExactlyOnceWith(false)
    })
    expect(platform.setAutopilotKeepAwake).toHaveBeenCalledExactlyOnceWith(true)
  })

  it('lowers the run logs with the history, and offers none longer than it', async () => {
    const { onSaved } = renderSection()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'History kept' }), '14')

    await waitFor(() => {
      expect(platform.setAutopilotHistoryDays).toHaveBeenCalledExactlyOnceWith(14)
    })
    expect(onSaved).toHaveBeenCalledWith(withAutopilot({ historyDays: 14, logDays: 14 }))
    expect(screen.getByRole('combobox', { name: 'Run logs kept' })).toHaveValue('14')
    const logs = within(screen.getByRole('combobox', { name: 'Run logs kept' }))

    expect(logs.getByRole('option', { name: '30 days (default)' })).toBeDisabled()
    expect(logs.getByRole('option', { name: '7 days' })).toBeEnabled()
  })

  it('turns the daily cap on in a unit, stores an amount typed in range, and turns it off keeping its unit', async () => {
    const { onSaved } = renderSection()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Daily cap' }), 'tokens')

    await waitFor(() => {
      expect(platform.setAutopilotDailyCap).toHaveBeenLastCalledWith({ amount: 1_000_000, unit: 'tokens' })
    })

    const amount = screen.getByRole('spinbutton', { name: 'Daily cap in tokens' })

    // Out of range, it says why and stores nothing.
    await userEvent.clear(amount)
    await userEvent.type(amount, '5{Enter}')
    expect(screen.getByText('Choose from 100,000 to 50,000,000 tokens.')).toBeInTheDocument()
    expect(amount).toHaveAttribute('aria-invalid', 'true')
    expect(platform.setAutopilotDailyCap).toHaveBeenCalledOnce()

    await userEvent.clear(amount)
    await userEvent.type(amount, '2000000')
    fireEvent.blur(amount)

    await waitFor(() => {
      expect(platform.setAutopilotDailyCap).toHaveBeenLastCalledWith({ amount: 2_000_000, unit: 'tokens' })
    })
    expect(onSaved).toHaveBeenLastCalledWith(withAutopilot({ dailyCap: { amount: 2_000_000, unit: 'tokens' } }))

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Daily cap' }), 'off')

    await waitFor(() => {
      expect(platform.setAutopilotDailyCap).toHaveBeenLastCalledWith({ amount: 0, unit: 'tokens' })
    })
  })

  it('says why a change was refused, and shows it undone', async () => {
    platform.setAutopilotAgentsAtOnce.mockRejectedValueOnce(new Error('Choose from 1 to 3 agents at once.'))
    renderSection()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Agents at once' }), '3')

    expect(await screen.findByText('Choose from 1 to 3 agents at once.')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Agents at once' })).toHaveValue('2')
    expect(screen.getByRole('combobox', { name: 'Agents at once' })).toHaveAccessibleDescription(
      /Choose from 1 to 3 agents at once\./,
    )
  })

  it('asks before it clears the history, and Cancel clears nothing', async () => {
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Clear history…' }))
    expect(screen.getByRole('dialog', { name: 'Clear Autopilot’s history?' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.clearAutopilotHistory).not.toHaveBeenCalled()
  })

  it('clears the history once asked, and says what it removed', async () => {
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Clear history…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))

    expect(platform.clearAutopilotHistory).toHaveBeenCalledOnce()
    expect(
      await screen.findByText(
        'Removed 4 matches from the history. Kept 1 match without its details, since it still keeps a routine from acting twice.',
      ),
    ).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says why the history couldn’t be cleared', async () => {
    platform.clearAutopilotHistory.mockRejectedValueOnce(new Error('Autopilot’s history couldn’t be cleared.'))
    renderSection()

    await userEvent.click(screen.getByRole('button', { name: 'Clear history…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Autopilot’s history couldn’t be cleared.')
  })

  it('reads the engine again whenever Autopilot changes', async () => {
    renderSection()

    expect(await screen.findByText(/^Last checked today, 11:59 AM/)).toBeInTheDocument()

    platform.getAutopilotEngine.mockResolvedValue(autopilotEngine({ elsewhere: true, holds: false }))
    act(() => {
      emit('autopilot:updated')
    })

    expect(
      await screen.findByText(
        `Autopilot is running in another copy of ${brand.name}. This one shows them and starts nothing.`,
      ),
    ).toBeInTheDocument()
  })
})

describe('lastCheckedText', () => {
  const now = new Date('2026-06-15T12:00:00Z')

  it('says what the last cycle did, and when', () => {
    expect(lastCheckedText(now, autopilotEngine())).toBe('Last checked today, 11:59 AM · 34 ms · 7 matches · 1 start')

    const engine = autopilotEngine()
    const earlier = { ...engine, lastCycle: engine.lastCycle && { ...engine.lastCycle, at: '2026-06-12T08:05:00Z' } }

    expect(lastCheckedText(now, earlier)).toBe('Last checked Jun 12, 2026, 8:05 AM · 34 ms · 7 matches · 1 start')
  })

  it('says it waits for the first refresh, that it hasn’t checked, or that another copy runs it', () => {
    const engine = autopilotEngine()
    const waiting = { ...engine, lastCycle: engine.lastCycle && { ...engine.lastCycle, waiting: 'refresh' } }

    expect(lastCheckedText(now, waiting)).toBe('Waiting for the first refresh.')
    expect(lastCheckedText(now, autopilotEngine({ lastCycle: null }))).toBe('Not checked yet.')
    expect(lastCheckedText(now, autopilotEngine({ elsewhere: true }))).toBe(
      `Autopilot is running in another copy of ${brand.name}. This one shows them and starts nothing.`,
    )
    expect(lastCheckedText(now, undefined)).toBe('')
  })
})
