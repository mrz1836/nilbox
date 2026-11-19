import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { StatusDot } from './StatusDot'

// dot renders a dot and returns it.
function dot(props: Parameters<typeof StatusDot>[0]): Element | null {
  const { container } = render(<StatusDot {...props} />)

  return container.firstElementChild
}

describe('StatusDot', () => {
  it('moves only while it pings or pulses', () => {
    expect(dot({ color: 'bg-success' })?.querySelector('[class*="animate-"]')).toBeNull()
    expect(dot({ color: 'bg-info', ping: true })?.querySelector('.motion-safe\\:animate-ping')).not.toBeNull()
    expect(dot({ color: 'bg-muted', pulse: true })?.querySelector('.motion-safe\\:animate-pulse')).not.toBeNull()
  })
})
