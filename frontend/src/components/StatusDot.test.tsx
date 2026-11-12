import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { StatusDot } from './StatusDot'

// dot renders a dot and returns it.
function dot(props: Parameters<typeof StatusDot>[0]): Element | null {
  const { container } = render(<StatusDot {...props} />)

  return container.firstElementChild
}

describe('StatusDot', () => {
  it('marks itself moving only while it pings or pulses, so its card keeps rendering', () => {
    expect(dot({ color: 'bg-success' })).not.toHaveAttribute('data-moving')
    expect(dot({ color: 'bg-info', ping: true })).toHaveAttribute('data-moving', 'true')
    expect(dot({ color: 'bg-muted', pulse: true })).toHaveAttribute('data-moving', 'true')
  })
})
