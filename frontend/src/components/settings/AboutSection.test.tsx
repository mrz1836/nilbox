import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { AboutSection } from './AboutSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

describe('AboutSection', () => {
  it('runs the setup again', async () => {
    const onRunSetup = vi.fn(() => Promise.resolve({ ok: true as const, data: undefined }))
    render(<AboutSection onRunSetup={onRunSetup} />)

    await userEvent.click(screen.getByRole('button', { name: 'Run setup again' }))

    expect(onRunSetup).toHaveBeenCalledOnce()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says why the setup couldn’t run again', async () => {
    const onRunSetup = vi.fn(() => Promise.resolve({ ok: false as const, error: 'the settings are locked' }))
    render(<AboutSection onRunSetup={onRunSetup} />)

    await userEvent.click(screen.getByRole('button', { name: 'Run setup again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('the settings are locked')
  })

  it('offers no setup without a way to run it', () => {
    render(<AboutSection />)

    expect(screen.queryByRole('button', { name: 'Run setup again' })).not.toBeInTheDocument()
  })

  it('asks before a reset', async () => {
    render(<AboutSection />)

    await userEvent.click(screen.getByRole('button', { name: `Reset ${brand.name}…` }))
    expect(screen.getByRole('dialog', { name: `Reset ${brand.name}?` })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
