import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { Info } from '../../platform'
import { appInfo, restored, settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { WelcomeStep, type WelcomeStepProps } from './WelcomeStep'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// showWelcome shows Welcome for info, as change adjusts its props.
function showWelcome(info: Info | null, change: Partial<WelcomeStepProps> = {}) {
  const props: WelcomeStepProps = {
    headingId: 'setup-heading',
    headingRef: createRef(),
    info,
    rerun: false,
    tools: false,
    theme: 'system',
    onThemeChange: vi.fn(() => Promise.resolve({ ok: true as const, data: undefined })),
    onStart: vi.fn(),
    onSkip: vi.fn(),
    onRestored: vi.fn(() => Promise.resolve()),
    onReview: vi.fn(),
    onFinishNow: vi.fn(),
    ...change,
  }

  render(<WelcomeStep {...props} />)

  return props
}

describe('WelcomeStep', () => {
  it('welcomes a new install, says what lies ahead, and sets off', async () => {
    const props = showWelcome(appInfo())

    expect(screen.getByRole('heading', { name: `Welcome to ${brand.name}` })).toBeInTheDocument()
    expect(screen.getByText(brand.tagline)).toBeInTheDocument()
    expect(screen.getByText('4 quick steps · about 2 minutes')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Let’s go' }))
    expect(props.onStart).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Skip setup' }))
    expect(props.onSkip).toHaveBeenCalledOnce()
  })

  it('counts the Tools step when it shows', () => {
    showWelcome(appInfo(), { tools: true })

    expect(screen.getByText('5 quick steps · about 3 minutes')).toBeInTheDocument()
  })

  it('picks a theme', async () => {
    const props = showWelcome(appInfo())

    await userEvent.click(screen.getByRole('radio', { name: /Light/ }))

    expect(props.onThemeChange).toHaveBeenCalledWith('light')
  })

  it('says what came back after a fresh start the user asked for', () => {
    showWelcome(
      appInfo({
        dataReset: { path: '~/data/example.db.reset-20261115T090000Z', reason: 'requested' },
        restored: restored(),
      }),
    )

    expect(screen.getByRole('heading', { name: 'Fresh start' })).toBeInTheDocument()
    expect(
      screen.getByText('Your 12 prompts, 3 launch defaults, and 2 scripts came back. Everything else starts fresh.'),
    ).toBeInTheDocument()
  })

  it('says everything starts fresh when nothing came back', () => {
    showWelcome(appInfo({ dataReset: { path: '~/data/example.db.reset-20261115T090000Z', reason: 'requested' } }))

    expect(screen.getByText('Everything starts fresh.')).toBeInTheDocument()
  })

  it('says the data couldn’t be read, where it went, and what came back', () => {
    showWelcome(
      appInfo({
        dataReset: { path: '~/data/example.db.broken-20261115T090000Z', reason: 'corrupt' },
        restored: restored({ launchDefaults: 0, scripts: 0, prompts: 1 }),
      }),
    )

    expect(screen.getByRole('heading', { name: `Welcome back to ${brand.name}` })).toBeInTheDocument()
    expect(screen.getByText(/couldn’t read its data, so it set it aside at/)).toHaveTextContent(
      'It brought back your prompt.',
    )
    expect(screen.getByText('~/data/example.db.broken-20261115T090000Z')).toBeInTheDocument()
  })

  it('says only where damaged data went when nothing came back', () => {
    showWelcome(appInfo({ dataReset: { path: '~/data/example.db.broken-20261115T090000Z', reason: 'history' } }))

    expect(screen.getByText(/couldn’t read its data/)).not.toHaveTextContent('brought back')
  })

  it('welcomes back a setup run again', () => {
    showWelcome(appInfo(), { rerun: true })

    expect(screen.getByRole('heading', { name: 'Welcome back' })).toBeInTheDocument()
    expect(screen.getByText(/doubles as a check of your setup/)).toBeInTheDocument()
  })

  it('waits for the app info as a new install', () => {
    showWelcome(null)

    expect(screen.getByRole('heading', { name: `Welcome to ${brand.name}` })).toBeInTheDocument()
  })

  it('restores a backup, then offers to review the steps or finish now', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/Users/example/Desktop/backup.json')
    platform.getSettings.mockResolvedValue(settings({ theme: 'dark' }))
    const props = showWelcome(appInfo())

    await userEvent.click(screen.getByRole('button', { name: 'Restore from a backup…' }))

    const dialog = within(await screen.findByRole('dialog'))
    await userEvent.click(await dialog.findByRole('button', { name: /Import/ }))

    expect(await screen.findByText('Your setup is back: review the steps, or finish now.')).toBeInTheDocument()
    expect(props.onThemeChange).toHaveBeenCalledWith('dark')
    expect(props.onRestored).toHaveBeenCalledWith(settings({ theme: 'dark' }))

    await userEvent.click(screen.getByRole('button', { name: 'Review the steps' }))
    expect(props.onReview).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Finish now' }))
    expect(props.onFinishNow).toHaveBeenCalledOnce()
  })

  it('keeps the theme when the backup brings the same one, and goes on when the settings can’t be read', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/Users/example/Desktop/backup.json')
    platform.getSettings.mockRejectedValue(new Error('the settings are gone'))
    const props = showWelcome(appInfo())

    await userEvent.click(screen.getByRole('button', { name: 'Restore from a backup…' }))
    await userEvent.click(await within(await screen.findByRole('dialog')).findByRole('button', { name: /Import/ }))

    expect(await screen.findByText(/Your setup is back/)).toBeInTheDocument()
    expect(props.onRestored).not.toHaveBeenCalled()
    expect(props.onThemeChange).not.toHaveBeenCalled()
  })

  it('cancels a restore', async () => {
    platform.chooseFileToOpen.mockResolvedValue('/Users/example/Desktop/backup.json')
    showWelcome(appInfo())

    await userEvent.click(screen.getByRole('button', { name: 'Restore from a backup…' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: 'Let’s go' })).toBeInTheDocument()
  })

  it('does nothing when no backup is chosen, and says why the dialog couldn’t open', async () => {
    showWelcome(appInfo())

    await userEvent.click(screen.getByRole('button', { name: 'Restore from a backup…' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    platform.chooseFileToOpen.mockRejectedValue(new Error('no window'))
    await userEvent.click(screen.getByRole('button', { name: 'Restore from a backup…' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t open the file dialog: no window')
  })
})
