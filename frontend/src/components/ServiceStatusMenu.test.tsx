import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ServiceStatusState } from '../lib/useServiceStatus'
import type { ServiceStatus } from '../platform'
import { renderAt } from '../test/clock'
import { claudeOutage, serviceIncident, serviceProvider, serviceStatus } from '../test/fixtures'
import { platform } from '../test/platform'
import { ServiceStatusMenu } from './ServiceStatusMenu'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function renderMenu(status: ServiceStatus | null, state: Partial<ServiceStatusState> = {}) {
  const check = vi.fn()
  const onChooseWatch = vi.fn()

  renderAt(<ServiceStatusMenu service={{ status, checking: false, check, ...state }} onChooseWatch={onChooseWatch} />)

  return { check, onChooseWatch }
}

describe('ServiceStatusMenu', () => {
  it('shows nothing while everything watched works, is unchecked, or the check is off', () => {
    for (const status of [
      null,
      serviceStatus(),
      serviceStatus({ state: 'unknown' }, serviceProvider({ state: 'unknown' })),
      serviceStatus(
        { state: 'off' },
        serviceProvider({ state: 'off' }),
        claudeOutage({ state: 'off', affected: [], incidents: [] }),
      ),
    ]) {
      const { unmount } = render(<ServiceStatusMenu service={{ status, checking: false, check: () => undefined }} />)

      expect(screen.queryByRole('button')).not.toBeInTheDocument()
      unmount()
    }
  })

  it('warns of an outage in red, named for the provider and its state', () => {
    renderMenu(serviceIncident())

    const button = screen.getByRole('button', { name: 'Service status: Partial outage on Claude' })
    expect(button).toHaveClass('text-danger')
    expect(button).toHaveAttribute('title', 'Service status: Partial outage on Claude')
  })

  it('warns of degraded performance or maintenance in amber', () => {
    renderMenu(serviceIncident(serviceProvider({ state: 'maintenance' })))

    expect(screen.getByRole('button', { name: 'Service status: Maintenance on GitHub' })).toHaveClass('text-warning')
  })

  it('opens a card for each provider in trouble, the worst first', async () => {
    renderMenu(
      serviceStatus(
        { state: 'major' },
        serviceProvider({
          state: 'minor',
          affected: [{ id: 'github.actions', name: 'Actions', state: 'minor' }],
        }),
        claudeOutage(),
      ),
    )

    await userEvent.click(screen.getByRole('button', { name: /^Service status/ }))

    const panel = screen.getByRole('dialog', { name: 'Service status' })
    const cards = within(panel).getAllByRole('region')
    expect(cards.map((c) => within(c).getByRole('heading').textContent)).toEqual(['Claude', 'GitHub'])
    expect(within(cards[1] ?? panel).getByText('Affects: Actions')).toBeInTheDocument()
  })

  it('shows what a provider’s trouble affects, its incidents, and the others on its page', async () => {
    renderMenu(serviceIncident())

    await userEvent.click(screen.getByRole('button', { name: /^Service status/ }))

    const card = screen.getByRole('region', { name: 'Claude' })
    expect(within(card).getByText('Partial outage')).toBeInTheDocument()
    expect(within(card).getByText('Affects: Claude API')).toBeInTheDocument()
    expect(within(card).getByRole('list', { name: 'Claude incidents' })).toHaveTextContent(
      'Elevated errors on Claude APIIdentified',
    )
    expect(within(card).getByText('1 other incident doesn’t touch what you watch')).toBeInTheDocument()

    await userEvent.click(within(card).getByRole('button', { name: 'Elevated errors on Claude API' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://status.claude.com/incidents/x1')

    await userEvent.click(within(card).getByRole('button', { name: 'Status page' }))
    expect(platform.openExternal).toHaveBeenLastCalledWith('https://status.claude.com/')
  })

  it('names maintenance, and leaves out a stage the page didn’t give', async () => {
    renderMenu(
      serviceIncident(
        serviceProvider({
          state: 'maintenance',
          incidents: [
            {
              name: 'Database upgrade',
              stage: 'in_progress',
              state: 'maintenance',
              url: 'https://www.githubstatus.com/incidents/m1',
              parts: ['Git Operations'],
              unscoped: false,
              maintenance: true,
            },
            {
              name: 'Elevated errors',
              stage: 'other',
              state: 'major',
              url: 'https://www.githubstatus.com/incidents/x2',
              parts: [],
              unscoped: true,
              maintenance: false,
            },
          ],
        }),
      ),
    )

    await userEvent.click(screen.getByRole('button', { name: /^Service status/ }))

    const incidents = screen.getByRole('list', { name: 'GitHub incidents' })
    expect(
      within(incidents)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Database upgradeMaintenance · Under way', 'Elevated errors'])
  })

  it('says when it was checked, checks again, and opens what to watch', async () => {
    const { check, onChooseWatch } = renderMenu(serviceIncident())

    await userEvent.click(screen.getByRole('button', { name: /^Service status/ }))
    expect(screen.getByText('Checked 1m ago')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    expect(check).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Choose what to watch' }))
    expect(onChooseWatch).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says while it checks, and when a provider’s last check failed', async () => {
    renderMenu(serviceIncident(claudeOutage({ error: 'Couldn’t reach Claude’s status page.' })), { checking: true })

    await userEvent.click(screen.getByRole('button', { name: /^Service status/ }))

    expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent(
      'Couldn’t reach Claude’s status page. This is the last reading.',
    )
  })
})
