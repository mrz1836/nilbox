import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { everyWords, RefreshingSection } from './RefreshingSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderSection(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<RefreshingSection settings={current} onSaved={onSaved} />)

  return { onSaved }
}

// labels lists a select's options as they read.
function labels(name: string): string[] {
  return within(screen.getByRole('combobox', { name }))
    .getAllByRole('option')
    .map((o) => o.textContent)
}

describe('RefreshingSection', () => {
  it('offers each interval Go allows, the stored one chosen and the defaults marked', () => {
    renderSection(
      settings({
        refresh: { inbox: 900, checks: 0, releaseChecks: 120, worktrees: 300, notifications: 60, serviceStatus: 1800 },
      }),
    )

    expect(screen.getByRole('combobox', { name: 'Inbox' })).toHaveValue('900')
    expect(screen.getByRole('combobox', { name: 'Running checks' })).toHaveValue('0')
    expect(screen.getByRole('combobox', { name: 'Release checks' })).toHaveValue('120')
    expect(screen.getByRole('combobox', { name: 'Worktrees' })).toHaveValue('300')
    expect(labels('Inbox')).toEqual([
      'Every 2 minutes',
      'Every 5 minutes (default)',
      'Every 10 minutes',
      'Every 15 minutes',
      'Every 30 minutes',
      'Every hour',
    ])
    expect(labels('Running checks')).toEqual([
      'Off',
      'Every 30 seconds',
      'Every 45 seconds (default)',
      'Every minute',
      'Every 2 minutes',
    ])
    expect(screen.getByRole('combobox', { name: 'Running checks' })).toHaveAccessibleDescription(
      /in requests of up to 25, about a point each, until their checks end/,
    )
    expect(labels('Release checks')).toEqual([
      'Off',
      'Every 30 seconds',
      'Every 45 seconds (default)',
      'Every minute',
      'Every 2 minutes',
    ])
    expect(screen.getByRole('combobox', { name: 'Release checks' })).toHaveAccessibleDescription(
      /Release page’s repositories whose newest commit’s checks are still running, .*about two points.*Read GitHub again reads every repository at once/,
    )
    expect(screen.getByRole('combobox', { name: 'GitHub notifications' })).toHaveValue('60')
    expect(labels('GitHub notifications')).toEqual([
      'Every minute',
      'Every 2 minutes',
      'Every 5 minutes (default)',
      'Every 10 minutes',
      'Every 15 minutes',
      'Every 30 minutes',
      'Every hour',
    ])
    expect(screen.getByRole('combobox', { name: 'GitHub notifications' })).toHaveAccessibleDescription(
      /free when nothing changed, and never sooner than GitHub asks/,
    )
  })

  it('stores a chosen interval at once', async () => {
    const { onSaved } = renderSection()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Running checks' }), '30')

    expect(platform.setRefreshInterval).toHaveBeenCalledExactlyOnceWith('checks', 30)
    expect(onSaved).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        refresh: { inbox: 300, checks: 30, releaseChecks: 45, worktrees: 300, notifications: 300, serviceStatus: 1800 },
      }),
    )
  })

  it('leaves the service status check to its own section', () => {
    renderSection()

    expect(screen.queryByRole('combobox', { name: /status/i })).not.toBeInTheDocument()
    expect(screen.getAllByRole('combobox')).toHaveLength(5)
  })

  it('stores the Release page’s checks follow apart, and turns it off', async () => {
    const { onSaved } = renderSection()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Release checks' }), '0')

    expect(platform.setRefreshInterval).toHaveBeenCalledExactlyOnceWith('releaseChecks', 0)
    expect(onSaved).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        refresh: { inbox: 300, checks: 45, releaseChecks: 0, worktrees: 300, notifications: 300, serviceStatus: 1800 },
      }),
    )
  })

  it('says why an interval was refused, beside it', async () => {
    platform.setRefreshInterval.mockRejectedValueOnce(new Error('database is locked'))
    renderSection()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Worktrees' }), '1800')

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    // The change shows at once and goes back as its save ends, which can be a render
    // after the refusal shows.
    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Worktrees' })).toHaveValue('300')
    })
  })

  it('turns noticing pushes from this Mac off and on, on by default', async () => {
    const { onSaved } = renderSection()

    const watch = screen.getByRole('switch', { name: 'Notice pushes from this Mac' })
    expect(watch).toBeChecked()
    expect(watch).toHaveAccessibleDescription(/nothing in a clone changes, and nothing is asked of GitHub/)

    await userEvent.click(watch)

    expect(platform.setWatchPushes).toHaveBeenCalledExactlyOnceWith(false)
    expect(onSaved).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ watchPushes: false }))
    expect(platform.setRefreshInterval).not.toHaveBeenCalled()
  })

  it('says why the switch was refused, beside it alone', async () => {
    platform.setWatchPushes.mockRejectedValueOnce(new Error('database is locked'))
    renderSection(settings({ watchPushes: false }))

    const watch = screen.getByRole('switch', { name: 'Notice pushes from this Mac' })
    await userEvent.click(watch)

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    // The switch goes back to what is stored once the refused save's transition ends.
    await waitFor(() => {
      expect(watch).not.toBeChecked()
    })
    expect(watch).toHaveAccessibleDescription(/database is locked/)
    expect(screen.getByRole('combobox', { name: 'Inbox' })).not.toHaveAccessibleDescription(/database is locked/)
  })

  it('words every interval', () => {
    expect([0, 30, 60, 120, 300, 3600, 7200].map(everyWords)).toEqual([
      'Off',
      'Every 30 seconds',
      'Every minute',
      'Every 2 minutes',
      'Every 5 minutes',
      'Every hour',
      'Every 2 hours',
    ])
  })
})
