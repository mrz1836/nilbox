import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { SetupStep } from '../../platform'
import { SetupRail } from './SetupRail'

const steps: SetupStep[] = ['github', 'clones', 'agents', 'notifications', 'tools']

// railOn shows the rail on agents, with the steps before it reached and GitHub
// needing attention.
function railOn(onGo = vi.fn()) {
  render(
    <SetupRail
      steps={steps}
      current="agents"
      reached={new Set<SetupStep>(['welcome', 'github', 'clones', 'agents'])}
      attention={new Set<SetupStep>(['github'])}
      onGo={onGo}
    />,
  )

  return within(screen.getByRole('navigation', { name: 'Setup steps' }))
}

describe('SetupRail', () => {
  it('shows how each step stands', () => {
    const rail = railOn()

    expect(rail.getByRole('button', { name: /Connect GitHub, needs attention/ })).toBeInTheDocument()
    expect(rail.getByRole('button', { name: /Find your clones, done/ })).toBeInTheDocument()
    expect(rail.getByText('Pick your agent').closest('[aria-current="step"]')).not.toBeNull()
    expect(rail.getByText('Stay in the loop').parentElement).toHaveTextContent('Stay in the loop, not yet')
    expect(rail.getByText('Suggested')).toBeInTheDocument()
    expect(rail.queryByRole('button', { name: /Stay in the loop/ })).not.toBeInTheDocument()
  })

  it('goes back to a step already reached', async () => {
    const onGo = vi.fn()
    const rail = railOn(onGo)

    await userEvent.click(rail.getByRole('button', { name: /Find your clones/ }))

    expect(onGo).toHaveBeenCalledWith('clones')
  })
})
