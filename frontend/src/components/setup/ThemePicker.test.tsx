import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ThemePicker } from './ThemePicker'

describe('ThemePicker', () => {
  it('chooses a theme from its tiles', async () => {
    const onThemeChange = vi.fn(() => Promise.resolve({ ok: true as const, data: undefined }))
    render(<ThemePicker theme="system" onThemeChange={onThemeChange} />)

    expect(screen.getByRole('radio', { name: /System/ })).toBeChecked()

    await userEvent.click(screen.getByRole('radio', { name: /Dark/ }))

    expect(onThemeChange).toHaveBeenCalledWith('dark')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says why a theme could not be stored', async () => {
    const onThemeChange = vi.fn(() => Promise.resolve({ ok: false as const, error: 'the disk is full' }))
    render(<ThemePicker theme="light" onThemeChange={onThemeChange} />)

    await userEvent.click(screen.getByRole('radio', { name: /System/ }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t store the theme: the disk is full')
  })
})
