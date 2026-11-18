import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { Edge, Story } from '../../lib/nextStep'
import { CardStory } from './CardStory'

function story(overrides: Partial<Story> = {}): Story {
  return { text: 'Re-review', detail: '', icon: 'dot', triage: false, ...overrides }
}

describe('CardStory', () => {
  it('says its lead in the edge’s color and its detail after it, muted', () => {
    const { container } = render(<CardStory story={story({ detail: '1 new commit' })} edge="yours" />)

    const lead = container.firstElementChild
    expect(lead).toHaveTextContent('Re-review · 1 new commit')
    expect(lead).toHaveClass('text-accent', 'font-semibold', 'shrink-0')
    expect(screen.getByText('· 1 new commit')).toHaveClass('text-muted')
  })

  it('leaves the detail out when there is none', () => {
    const { container } = render(<CardStory story={story()} edge="yours" />)

    expect(container).toHaveTextContent(/^Re-review$/)
  })

  it.each([
    ['urgent', 'text-warning', 'bg-warning'],
    ['blocked', 'text-danger', 'bg-danger'],
    ['yours', 'text-accent', 'bg-accent'],
    ['ready', 'text-success', 'bg-success'],
    ['agent', 'text-info', 'bg-info'],
    ['waiting', 'text-muted', 'bg-muted'],
    ['none', 'text-muted', 'bg-muted'],
  ] as const)('colors a %s story', (edge: Edge, text, dot) => {
    const { container } = render(<CardStory story={story()} edge={edge} />)

    expect(container.firstElementChild).toHaveClass(text)
    expect(container.querySelector('[data-icon="dot"]')).toHaveClass(dot)
  })

  it('marks something at work or waiting now with a moving dot', () => {
    const { container } = render(<CardStory story={story({ icon: 'ping' })} edge="urgent" />)

    expect(container.querySelector('[data-icon="ping"] [data-moving]')).not.toBeNull()
  })

  it.each(['clock', 'bolt', 'archive'] as const)('marks a story with a %s', (icon) => {
    const { container } = render(<CardStory story={story({ icon })} edge="waiting" />)

    expect(container.querySelector(`[data-icon="${icon}"] svg`)).not.toBeNull()
    expect(container.querySelector('[data-moving]')).toBeNull()
  })

  it('shows what it is given in place of a triage story', () => {
    render(
      <CardStory story={story({ text: '', triage: true })} edge="none">
        <span>Back Mon 9:00</span>
      </CardStory>,
    )

    expect(screen.getByText('Back Mon 9:00')).toBeInTheDocument()
  })

  it('draws a compact card’s story as a chip tinted with its edge, cutting its words short', () => {
    const { container } = render(<CardStory story={story({ detail: '1 new commit' })} edge="yours" chip />)

    const chip = container.firstElementChild
    expect(chip).toHaveAttribute('data-story-chip')
    expect(chip).toHaveClass('text-accent', 'bg-accent/11', 'max-w-60', 'h-5')
    expect(chip).toHaveTextContent('Re-review · 1 new commit')
    expect(screen.getByText('· 1 new commit')).toHaveClass('text-muted')
    expect(screen.getByText('· 1 new commit').parentElement).toHaveClass('truncate')
  })

  it('leaves a compact card’s chip untinted while it waits on others, and its detail out when there is none', () => {
    const { container } = render(<CardStory story={story({ text: 'Waiting for review' })} edge="waiting" chip />)

    const chip = container.firstElementChild
    expect(chip).toHaveTextContent(/^Waiting for review$/)
    expect(chip?.className).not.toMatch(/\bbg-/)
  })
})
