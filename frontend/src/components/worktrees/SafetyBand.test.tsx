import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Safety } from '../../lib/worktrees'
import { platform } from '../../test/platform'
import { SafetyBand } from './SafetyBand'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// band is a safety answer, quiet unless overrides say otherwise.
function band(overrides: Partial<Safety> = {}): Safety {
  return { tone: 'success', icon: 'shield', text: ['Safe to remove.'], also: [], command: '', ...overrides }
}

describe('SafetyBand', () => {
  it.each<[Safety['tone'], Safety['icon'], string, string]>([
    ['success', 'shield', 'bg-success/9', 'text-success'],
    ['warning', 'alert', 'bg-warning/10', 'text-warning'],
    ['warning', 'up', 'bg-warning/10', 'text-warning'],
    ['warning', 'ring', 'bg-warning/10', 'text-warning'],
    ['info', 'info', 'bg-info/11', 'text-info'],
    ['danger', 'lock', 'bg-danger/9', 'text-danger'],
    ['danger', 'ghost', 'bg-danger/9', 'text-danger'],
  ])('draws the %s tone with its %s mark', (tone, icon, background, mark) => {
    const { container } = render(<SafetyBand safety={band({ tone, icon })} />)

    const shown = container.firstElementChild
    expect(shown).toHaveAttribute('data-tone', tone)
    expect(shown).toHaveClass(background, 'border-b')
    expect(shown?.querySelector('svg')).toHaveClass(mark)
  })

  it('sets the parts in code as code, with a second, muted line', () => {
    render(
      <SafetyBand
        safety={band({
          tone: 'warning',
          icon: 'up',
          text: ['Its folder is clean, but 2 commits on ', { code: 'feat/x' }, ' aren’t on GitHub.'],
          also: ['Removing the worktree keeps ', { code: 'feat/x' }, '.'],
        })}
      />,
    )

    const [lead, also] = screen.getAllByText('feat/x')
    expect(lead?.tagName).toBe('CODE')
    expect(lead?.parentElement).toHaveTextContent('Its folder is clean, but 2 commits on feat/x aren’t on GitHub.')
    expect(lead?.parentElement).toHaveClass('font-medium')
    expect(also?.parentElement).toHaveTextContent('Removing the worktree keeps feat/x.')
    expect(also?.parentElement).toHaveClass('text-muted')
  })

  it('shows no second line when there’s nothing more to say', () => {
    const { container } = render(<SafetyBand safety={band()} />)

    expect(container.querySelector('.text-muted')).toBeNull()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('offers the command to run first, with Copy', async () => {
    render(
      <SafetyBand safety={band({ tone: 'danger', icon: 'lock', command: 'git -C ~/projects/x worktree unlock y' })} />,
    )

    expect(screen.getByText('git -C ~/projects/x worktree unlock y')).toHaveAttribute(
      'title',
      'git -C ~/projects/x worktree unlock y',
    )
    await userEvent.click(screen.getByRole('button', { name: 'Copy' }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('git -C ~/projects/x worktree unlock y')
  })

  it('takes the classes of where it stands', () => {
    const { container } = render(<SafetyBand safety={band()} className="rounded-lg border" />)

    expect(container.firstElementChild).toHaveClass('rounded-lg', 'border')
    expect(container.firstElementChild).not.toHaveClass('border-b')
  })
})
