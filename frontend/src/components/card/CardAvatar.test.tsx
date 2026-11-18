import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { InboxItem } from '../../platform'
import { item } from '../../test/fixtures'
import { CardAvatar, faceOf, initialsOf } from './CardAvatar'

function renderAvatar(overrides: Partial<InboxItem> = {}) {
  return render(<CardAvatar item={item(overrides)} />)
}

describe('initialsOf', () => {
  it.each([
    ['contributor-a', 'CA'],
    ['outside-dev', 'OD'],
    ['octo_user.name', 'OU'],
    ['octocat', 'OC'],
    ['x', 'X'],
    ['-', ''],
  ])('stands %s for its initials', (login, want) => {
    expect(initialsOf(login)).toBe(want)
  })
})

describe('faceOf', () => {
  it('picks the same backing for a login every time, from the theme’s colors', () => {
    expect(faceOf('contributor-a')).toBe(faceOf('contributor-a'))
    expect(faceOf('contributor-a')).toMatch(/^bg-(accent|success|warning|info|danger|muted)\/\d+ text-/)

    const picked = new Set(['contributor-a', 'contributor-b', 'outside-dev', 'octo-user', 'gen-bot'].map(faceOf))
    expect(picked.size).toBeGreaterThan(1)
  })
})

describe('CardAvatar', () => {
  it('shows a person’s initials in a circle, with the kind of item at its corner', () => {
    const { container } = renderAvatar({ author: 'contributor-a' })

    const face = container.querySelector('[data-avatar="initials"]')
    expect(face).toHaveTextContent('CA')
    expect(face).toHaveClass('rounded-full', ...faceOf('contributor-a').split(' '))
    expect(face).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByRole('img', { name: 'Pull request' })).toHaveClass('text-success')
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('shows a bot as a rounded square with the bot mark', () => {
    const { container } = renderAvatar({ author: 'dependabot[bot]', authorIsBot: true })

    const face = container.querySelector('[data-avatar="bot"]')
    expect(face).toHaveClass('rounded-lg')
    expect(face?.querySelector('svg')).not.toBeNull()
    expect(container.querySelector('[data-avatar="initials"]')).toBeNull()
  })

  it('rings the user’s own items in the accent color', () => {
    const { container, rerender } = renderAvatar({ authored: true })

    expect(container.querySelector('[data-avatar]')).toHaveClass('ring-2', 'ring-accent')

    rerender(<CardAvatar item={item()} />)
    expect(container.querySelector('[data-avatar]')).not.toHaveClass('ring-2')
  })

  it.each([
    [{ isDraft: true }, 'Draft pull request', 'text-muted'],
    [{ kind: 'issue' } as const, 'Issue', 'text-success'],
  ])('badges %o as its kind', (overrides, label, color) => {
    renderAvatar(overrides)

    const badge = screen.getByRole('img', { name: label })
    expect(badge).toHaveAttribute('title', label)
    expect(badge).toHaveClass(color, 'z-10')
  })
})
