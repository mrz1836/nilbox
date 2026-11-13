import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProgressBar } from './ItemSignals'

describe('ProgressBar', () => {
  // fill is how far the bar is filled.
  function fill(container: HTMLElement): string {
    return (container.querySelector('.bg-success') as HTMLElement).style.width
  }

  it('fills as far as done goes, never past full, and stays empty with nothing to do', () => {
    const { container, rerender } = render(<ProgressBar done={1} total={4} />)

    expect(fill(container)).toBe('25%')
    expect(container.firstElementChild).toHaveAttribute('aria-hidden', 'true')
    expect(container.firstElementChild).toHaveClass('w-10')

    rerender(<ProgressBar done={5} total={4} className="w-24" />)
    expect(fill(container)).toBe('100%')
    expect(container.firstElementChild).toHaveClass('w-24')

    rerender(<ProgressBar done={0} total={0} />)
    expect(fill(container)).toBe('0%')
  })
})
