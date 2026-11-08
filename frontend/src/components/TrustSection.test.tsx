import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Settings } from '../platform'
import { settings } from '../test/fixtures'
import { TrustSection } from './TrustSection'

const platform = vi.hoisted(() => ({
  setTrustedBots: vi.fn(),
  resetTrustedBots: vi.fn(),
}))

vi.mock('../platform', () => platform)

function renderTrust(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<TrustSection settings={current} onSaved={onSaved} />)

  return { onSaved }
}

function bots() {
  return within(screen.getByRole('list', { name: 'Trusted bots' }))
}

beforeEach(() => {
  // Each change comes back with the settings as stored.
  platform.setTrustedBots.mockResolvedValue(settings({ trustedBots: ['dependabot', 'my-release-bot'] }))
  platform.resetTrustedBots.mockResolvedValue(settings())
})

describe('TrustSection', () => {
  it('lists the trusted bots, marking the defaults', () => {
    renderTrust(settings({ trustedBots: ['dependabot', 'my-release-bot'] }))

    expect(
      bots()
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['dependabotdefault', 'my-release-bot'])
    expect(screen.getByRole('button', { name: 'Restore the defaults' })).toBeInTheDocument()
    expect(screen.getByText(/a person with a similar login never is/)).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'How trust works' })).toBeInTheDocument()
  })

  it('offers no restore while the list is the default', () => {
    renderTrust()

    expect(screen.queryByRole('button', { name: 'Restore the defaults' })).not.toBeInTheDocument()
  })

  it('trusts another bot, written as GitHub shows it', async () => {
    const { onSaved } = renderTrust()

    await userEvent.type(screen.getByRole('textbox', { name: 'Bot to trust' }), ' My-Release-Bot[bot] ')
    await userEvent.click(screen.getByRole('button', { name: 'Trust bot' }))

    expect(platform.setTrustedBots).toHaveBeenCalledWith(['dependabot', 'github-actions', 'renovate', 'my-release-bot'])
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ trustedBots: ['dependabot', 'my-release-bot'] }))
    })
    expect(screen.getByRole('textbox', { name: 'Bot to trust' })).toHaveValue('')
  })

  it('says when a bot is already trusted, or the login is refused', async () => {
    platform.setTrustedBots.mockRejectedValue(new Error('not a bot login: "two words"'))
    renderTrust()

    const input = screen.getByRole('textbox', { name: 'Bot to trust' })

    await userEvent.type(input, 'app/Dependabot')
    await userEvent.click(screen.getByRole('button', { name: 'Trust bot' }))

    expect(screen.getByRole('alert')).toHaveTextContent('dependabot is already trusted.')
    expect(platform.setTrustedBots).not.toHaveBeenCalled()

    await userEvent.clear(input)
    await userEvent.type(input, 'two words')
    await userEvent.click(screen.getByRole('button', { name: 'Trust bot' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('not a bot login')
    expect(input).toHaveAttribute('aria-invalid', 'true')
  })

  it('stops trusting a bot, and restores the defaults', async () => {
    const { onSaved } = renderTrust(settings({ trustedBots: ['dependabot', 'my-release-bot'] }))

    await userEvent.click(bots().getByRole('button', { name: 'Stop trusting my-release-bot' }))

    expect(platform.setTrustedBots).toHaveBeenCalledWith(['dependabot'])
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })

    await userEvent.click(screen.getByRole('button', { name: 'Restore the defaults' }))

    expect(platform.resetTrustedBots).toHaveBeenCalledOnce()
  })

  it('explains an empty list', () => {
    renderTrust(settings({ trustedBots: [] }))

    expect(screen.getByText(/No bot is trusted/)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Trusted bots' })).not.toBeInTheDocument()
  })
})
