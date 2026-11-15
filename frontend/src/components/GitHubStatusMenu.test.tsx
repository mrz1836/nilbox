import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { GitHubStatusState } from '../lib/useGitHubStatus'
import type { GitHubStatus } from '../platform'
import { at } from '../test/clock'
import { githubIncident, githubStatus } from '../test/fixtures'
import { platform } from '../test/platform'
import { GitHubStatusMenu } from './GitHubStatusMenu'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function renderMenu(status: GitHubStatus | null, state: Partial<GitHubStatusState> = {}) {
  const check = vi.fn()

  render(<GitHubStatusMenu github={{ status, checking: false, check, ...state }} />, { wrapper: at() })

  return { check }
}

describe('GitHubStatusMenu', () => {
  it('shows nothing while GitHub is fine, unchecked, or the check is off', () => {
    for (const status of [null, githubStatus(), githubStatus({ state: 'unknown' }), githubStatus({ state: 'off' })]) {
      const { unmount } = render(<GitHubStatusMenu github={{ status, checking: false, check: () => undefined }} />)

      expect(screen.queryByRole('button')).not.toBeInTheDocument()
      unmount()
    }
  })

  it('warns of an outage in red, named for what the page says', () => {
    renderMenu(githubIncident())

    const button = screen.getByRole('button', { name: 'GitHub status: Partial system outage' })
    expect(button).toHaveClass('text-danger')
    expect(button).toHaveAttribute('title', 'GitHub status: Partial system outage')
  })

  it('warns of a minor incident or maintenance in amber', () => {
    renderMenu(githubStatus({ state: 'maintenance', description: 'Service Under Maintenance' }))

    expect(screen.getByRole('button', { name: 'GitHub status: Service under maintenance' })).toHaveClass('text-warning')
  })

  it('opens the incidents, what they affect, and when it was checked', async () => {
    renderMenu(githubIncident())

    await userEvent.click(screen.getByRole('button', { name: /GitHub status/ }))

    const panel = screen.getByRole('dialog', { name: 'GitHub status' })
    expect(within(panel).getByText('Partial system outage')).toBeInTheDocument()
    expect(within(panel).getByText(/may lag or fail\. Checked 1m ago\./)).toBeInTheDocument()
    expect(within(panel).getByRole('region', { name: 'Incidents' })).toHaveTextContent(
      'Incident with ActionsInvestigating',
    )
    expect(within(panel).getByRole('region', { name: 'Affected' })).toHaveTextContent('ActionsPartial outage')

    await userEvent.click(within(panel).getByRole('button', { name: 'Incident with Actions' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://www.githubstatus.com/incidents/x1')
  })

  it('checks again, and opens the status page', async () => {
    const { check } = renderMenu(githubIncident())

    await userEvent.click(screen.getByRole('button', { name: /GitHub status/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    expect(check).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Status page' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://www.githubstatus.com/')
  })

  it('says while it checks, and when the last check failed', async () => {
    renderMenu(githubIncident({ error: 'Couldn’t reach GitHub’s status page.' }), { checking: true })

    await userEvent.click(screen.getByRole('button', { name: /GitHub status/ }))

    expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent(
      'Couldn’t reach GitHub’s status page. This is the last reading.',
    )
  })
})
