import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { project } from '../test/fixtures'
import { FieldPill, ProgressBar, ProjectLink } from './ItemSignals'

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

describe('ProjectLink', () => {
  const roadmap = project({
    fields: [
      { name: 'Phase', value: 'Beta', color: '' },
      { name: 'Size', value: 'XL', color: '' },
      { name: 'Priority', value: 'High', color: '#fa4549' },
    ],
  })

  it('features the priority, then the size, as outlined pills, and says them', () => {
    render(<ProjectLink project={roadmap} />)

    const link = screen.getByRole('button', {
      name: 'Project Roadmap: In progress, Priority High, Size XL. Open it on GitHub',
    })
    expect(within(link).getByText('High').parentElement).toHaveStyle({ borderColor: '#fa4549b3' })
    expect(within(link).getByText('XL').parentElement).toHaveClass('border-border')
    expect(within(link).queryByText('Beta')).not.toBeInTheDocument()
  })

  it('leaves the fields out when whatever is beside it lists them', () => {
    render(<ProjectLink project={roadmap} featuring={false} />)

    const link = screen.getByRole('button', { name: 'Project Roadmap: In progress. Open it on GitHub' })
    expect(within(link).queryByText('High')).not.toBeInTheDocument()
  })
})

describe('FieldPill', () => {
  it('is cut short on a card, and runs the length of its line when wide', () => {
    const field = { name: 'Pillar', value: '19 Web surfaces', color: '#2da44e' }
    const { container, rerender } = render(<FieldPill field={field} />)

    expect(container.firstElementChild).toHaveClass('max-w-24')

    rerender(<FieldPill field={field} wide />)
    expect(container.firstElementChild).toHaveClass('max-w-full')
    expect(container.firstElementChild).not.toHaveClass('max-w-24')
  })
})
