import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { NotificationsSection } from './NotificationsSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderSection(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<NotificationsSection settings={current} onSaved={onSaved} />)

  return { onSaved, slider: screen.getByRole('slider', { name: 'Toasts stay for' }) }
}

// drag moves the slider's knob as a person does: an input event for each length it passes
// (React's onChange), then the change event as it is let go.
function drag(slider: HTMLElement, ...lengths: number[]) {
  for (const length of lengths) {
    fireEvent.input(slider, { target: { value: String(length) } })
  }

  fireEvent.change(slider)
}

describe('NotificationsSection', () => {
  it('shows how long toasts stay on a slider from 5 to 60 seconds', () => {
    const { slider } = renderSection(settings({ toastSeconds: 20 }))

    expect(slider).toHaveValue('20')
    expect(slider).toHaveAttribute('min', '5')
    expect(slider).toHaveAttribute('max', '60')
    expect(slider).toHaveAttribute('step', '1')
    expect(slider).toHaveAttribute('aria-valuetext', '20 seconds')
    expect(slider).toHaveAccessibleDescription(/before it closes itself/)
    expect(screen.getByText('20 s')).toBeInTheDocument()
  })

  it('follows the knob as it moves and stores the length once it is let go', async () => {
    const { onSaved, slider } = renderSection()

    fireEvent.input(slider, { target: { value: '30' } })
    fireEvent.input(slider, { target: { value: '45' } })

    expect(screen.getByText('45 s')).toBeInTheDocument()
    expect(slider).toHaveAttribute('aria-valuetext', '45 seconds')
    expect(platform.setToastSeconds).not.toHaveBeenCalled()

    fireEvent.change(slider)

    // The length shows at once, while it is stored.
    expect(slider).toHaveValue('45')
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ toastSeconds: 45 }))
    })
    expect(platform.setToastSeconds).toHaveBeenCalledExactlyOnceWith(45)
  })

  it('stores nothing when the knob is let go where it started', async () => {
    const { onSaved, slider } = renderSection(settings({ toastSeconds: 12 }))

    drag(slider, 40, 12)
    // A save would reach the setter once the queue moves on, so let it.
    await Promise.resolve()

    expect(platform.setToastSeconds).not.toHaveBeenCalled()
    expect(onSaved).not.toHaveBeenCalled()
    expect(slider).toHaveValue('12')
  })

  it('goes back to the stored length, and says why, when one is refused', async () => {
    platform.setToastSeconds.mockRejectedValueOnce(new Error('database is locked'))
    const { slider } = renderSection(settings({ toastSeconds: 12 }))

    drag(slider, 8)

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    // The knob goes back once the refused save has settled, which can be a render after the
    // alert shows.
    await waitFor(() => {
      expect(slider).toHaveValue('12')
    })
    expect(screen.getByText('12 s')).toBeInTheDocument()
    expect(slider).toHaveAccessibleDescription(/database is locked/)
  })
})
