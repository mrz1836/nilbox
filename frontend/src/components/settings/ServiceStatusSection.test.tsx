import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { Settings } from '../../platform'
import { renderAt } from '../../test/clock'
import { claudeOutage, serviceProvider, serviceStatus, settings, statusWatch } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ServiceStatusSection } from './ServiceStatusSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// renderSection renders the section as the settings page holds it: the settings it saves
// come back as the stored ones.
function renderSection(saved: Settings = settings()) {
  const onSaved = vi.fn<(settings: Settings) => void>()

  function Section() {
    const [current, setCurrent] = useState(saved)

    return (
      <ServiceStatusSection
        settings={current}
        onSaved={(next) => {
          onSaved(next)
          setCurrent(next)
        }}
      />
    )
  }

  renderAt(<Section />)

  return { onSaved }
}

// card is a provider's card, by its name.
function card(name: string) {
  return within(screen.getByRole('region', { name }))
}

describe('ServiceStatusSection', () => {
  it('checks every half hour by default, and turns the check off', async () => {
    const { onSaved } = renderSection()

    const every = screen.getByRole('combobox', { name: 'Check every' })
    expect(every).toHaveValue('1800')
    expect(
      within(every)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual([
      'Off',
      'Every 5 minutes',
      'Every 10 minutes',
      'Every 15 minutes',
      'Every 30 minutes (default)',
      'Every hour',
    ])
    expect(every).toHaveAccessibleDescription(/outside any API, so it costs none of your GitHub budget/)

    await userEvent.selectOptions(every, '0')

    expect(platform.setRefreshInterval).toHaveBeenCalledExactlyOnceWith('serviceStatus', 0)
    expect(onSaved).toHaveBeenCalledOnce()
    expect(onSaved.mock.lastCall?.[0].refresh.serviceStatus).toBe(0)
  })

  it('lists a card for each status page in the catalog, with its parts and the defaults ticked', () => {
    renderSection()

    expect(screen.getAllByRole('region').map((r) => r.getAttribute('data-setting'))).toEqual([
      'GitHub status page',
      'Claude status page',
      'OpenAI status page',
    ])
    expect(card('GitHub').getByRole('switch', { name: 'GitHub' })).toBeChecked()
    expect(card('GitHub').getByRole('checkbox', { name: 'Pull Requests' })).toBeChecked()
    expect(card('GitHub').getByRole('checkbox', { name: 'Copilot' })).not.toBeChecked()
    expect(card('Claude').getByRole('checkbox', { name: 'Claude Code' })).toBeChecked()
    expect(card('Claude').getByRole('checkbox', { name: 'claude.ai' })).not.toBeChecked()
  })

  it('groups a page’s parts as the page groups them', () => {
    renderSection()

    const openai = card('OpenAI')
    expect(
      openai.getAllByRole('group').map((g) => [
        g.getAttribute('aria-label'),
        within(g)
          .getAllByRole('checkbox')
          .map((c) => c.closest('label')?.textContent),
      ]),
    ).toEqual([
      ['OpenAI Codex', ['Codex API', 'Codex CLI', 'Codex Web']],
      ['OpenAI APIs', ['Responses']],
      ['OpenAI ChatGPT', ['Login']],
    ])
    expect(
      card('GitHub')
        .getAllByRole('group')
        .map((g) => g.getAttribute('aria-label')),
    ).toEqual(['GitHub parts'])
  })

  it('ticks a part, which Go judges again with no request', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(card('Claude').getByRole('checkbox', { name: 'claude.ai' }))

    expect(platform.setStatusPart).toHaveBeenCalledExactlyOnceWith('claude.ai', true)
    expect(platform.checkServiceStatus).not.toHaveBeenCalled()
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(card('Claude').getByRole('checkbox', { name: 'claude.ai' })).toBeChecked()
    // The status is read again once the save is in, which can land a render after onSaved.
    await waitFor(() => {
      expect(platform.getServiceStatus).toHaveBeenCalledTimes(2)
    })
  })

  it('turns a page off, which greys its parts', async () => {
    renderSection()

    await userEvent.click(card('OpenAI').getByRole('switch', { name: 'OpenAI' }))

    expect(platform.setStatusProvider).toHaveBeenCalledExactlyOnceWith('openai', false)
    expect(card('OpenAI').getByRole('switch', { name: 'OpenAI' })).not.toBeChecked()
    expect(card('OpenAI').getByRole('checkbox', { name: 'Codex CLI' })).toBeDisabled()
    expect(card('GitHub').getByRole('checkbox', { name: 'Pull Requests' })).toBeEnabled()
  })

  it('greys every card while the check is off', () => {
    renderSection(settings({ refresh: { ...settings().refresh, serviceStatus: 0 } }))

    expect(screen.getByRole('region', { name: 'GitHub' })).toHaveClass('opacity-60')
    expect(card('GitHub').getByRole('switch', { name: 'GitHub' })).toBeDisabled()
    expect(card('Claude').getByRole('checkbox', { name: 'Claude API' })).toBeDisabled()
    expect(card('GitHub').getByText('Not checked: the check is off')).toBeInTheDocument()
  })

  it('says a page on with nothing ticked isn’t checked', async () => {
    platform.getServiceStatus.mockResolvedValue(
      serviceStatus({}, serviceProvider({ id: 'claude', name: 'Claude', state: 'off' })),
    )
    const watch = statusWatch()
    watch.watch = watch.watch.map((w) => (w.provider === 'claude' ? { ...w, parts: [] } : w))
    renderSection(settings({ serviceStatus: watch }))

    await waitFor(() => {
      expect(platform.getServiceStatus).toHaveBeenCalled()
    })
    expect(card('Claude').getByText('Not checked: tick a part to watch')).toBeInTheDocument()
  })

  it('shows where each page stands now, and the incidents elsewhere on it', async () => {
    platform.getServiceStatus.mockResolvedValue(
      serviceStatus(
        { state: 'major' },
        claudeOutage(),
        serviceProvider({ id: 'openai', name: 'OpenAI', others: 2, unlisted: ['openai.login'] }),
      ),
    )
    renderSection()

    expect(await card('Claude').findByText('Partial outage · checked 1m ago')).toBeInTheDocument()
    expect(card('Claude').getByText('1 other incident on the page now')).toBeInTheDocument()
    expect(card('GitHub').getByText('Operational · checked 1m ago')).toBeInTheDocument()
    expect(card('OpenAI').getByText('2 other incidents on the page now')).toBeInTheDocument()
    expect(card('OpenAI').getByText(/The page no longer lists Login, so it isn’t watched/)).toBeInTheDocument()
    expect(card('GitHub').queryByText(/other incident/)).not.toBeInTheDocument()

    await userEvent.click(card('Claude').getByRole('button', { name: 'Status page' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://status.claude.com/')
  })

  it('says when a page isn’t read, isn’t read yet, or its last check failed', async () => {
    const failed = claudeOutage({
      state: 'unknown',
      affected: [],
      incidents: [],
      others: 0,
      checkedAt: '',
      error: 'Couldn’t reach Claude’s status page.',
    })
    platform.getServiceStatus.mockResolvedValue(
      serviceStatus({}, failed, serviceProvider({ id: 'openai', name: 'OpenAI', state: 'off' })),
    )
    const watch = statusWatch()
    watch.watch = watch.watch.map((w) => (w.provider === 'openai' ? { ...w, on: false } : w))
    renderSection(settings({ serviceStatus: watch }))

    expect(await card('Claude').findByText('Not checked yet')).toBeInTheDocument()
    expect(card('Claude').getByText('Couldn’t reach Claude’s status page.')).toBeInTheDocument()
    expect(card('OpenAI').getByText('Off: its page isn’t read')).toBeInTheDocument()
  })

  it('says why a change was refused, on its card', async () => {
    platform.setStatusPart.mockRejectedValueOnce(new Error('database is locked'))
    renderSection()

    await userEvent.click(card('GitHub').getByRole('checkbox', { name: 'Webhooks' }))

    expect(await card('GitHub').findByRole('alert')).toHaveTextContent('database is locked')
    // The box shows the tick at once and takes it back once the refusal is in, which can land
    // a render after the alert.
    await waitFor(() => {
      expect(card('GitHub').getByRole('checkbox', { name: 'Webhooks' })).not.toBeChecked()
    })
    expect(card('Claude').queryByRole('alert')).not.toBeInTheDocument()
  })
})
