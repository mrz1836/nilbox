import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CardPicturesContext, type Pictures } from '../../lib/pictures'
import type { InboxItem } from '../../platform'
import { avatarDataURL, item } from '../../test/fixtures'
import { CardAvatar, faceOf, initialsOf, KindMark } from './CardAvatar'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

function renderAvatar(overrides: Partial<InboxItem> = {}, pictures?: Pictures) {
  const avatar = <CardAvatar item={item(overrides)} />

  return render(pictures === undefined ? avatar : <CardPicturesContext value={pictures}>{avatar}</CardPicturesContext>)
}

// botPicture is a stand-in for a bot's picture, another than a person's.
const botPicture = 'data:image/png;base64,Ym90'

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

  it('rings the user’s own items in the accent color, picture and all', () => {
    const { container, rerender } = renderAvatar({ authored: true }, { 'octo-user': avatarDataURL })

    expect(container.querySelector('[data-avatar]')).toHaveClass('ring-2', 'ring-accent')
    expect(container.querySelector('img')).toHaveClass('ring-2', 'ring-accent')

    rerender(<CardAvatar item={item()} />)
    expect(container.querySelector('[data-avatar]')).not.toHaveClass('ring-2')
  })

  // jsdom never decodes an image, so a test sends the load, or the error, by hand.
  it('shows a person’s picture in a circle once it decodes, the initials until then and should it fail', () => {
    const { container } = renderAvatar({ author: 'contributor-a' }, { 'contributor-a': avatarDataURL })

    const img = container.querySelector('img')
    expect(img).toHaveAttribute('src', avatarDataURL)
    expect(img).toHaveAttribute('alt', '')
    expect(img).not.toBeVisible()
    expect(container.querySelector('[data-avatar="initials"]')).toHaveTextContent('CA')

    fireEvent.load(img as HTMLImageElement)
    expect(img).toBeVisible()
    expect(img).toHaveClass('size-7', 'rounded-full')
    expect(container.querySelector('[data-avatar]')).toBeNull()
    expect(screen.getByRole('img', { name: 'Pull request' })).toBeInTheDocument()

    const { container: broken } = renderAvatar({ author: 'contributor-b' }, { 'contributor-b': avatarDataURL })
    fireEvent.error(broken.querySelector('img') as HTMLImageElement)
    expect(broken.querySelector('img')).toBeNull()
    expect(broken.querySelector('[data-avatar="initials"]')).toHaveTextContent('CB')
  })

  it('shows a bot’s picture in a rounded square, by its login with [bot], and the bot mark until it decodes and should it fail', () => {
    const { container } = renderAvatar(
      { author: 'dependabot', authorIsBot: true },
      { dependabot: avatarDataURL, 'dependabot[bot]': botPicture },
    )

    const img = container.querySelector('img')
    expect(img, 'never a person’s picture of the same name').toHaveAttribute('src', botPicture)
    expect(container.querySelector('[data-avatar="bot"]')).toBeInTheDocument()

    fireEvent.load(img as HTMLImageElement)
    expect(img).toHaveClass('size-7', 'rounded-lg')
    expect(container.querySelector('[data-avatar="bot"]')).toBeNull()

    const { container: broken } = renderAvatar(
      { author: 'renovate', authorIsBot: true },
      { 'renovate[bot]': botPicture },
    )
    fireEvent.error(broken.querySelector('img') as HTMLImageElement)
    expect(broken.querySelector('img')).toBeNull()
    expect(broken.querySelector('[data-avatar="bot"]')).toHaveClass('rounded-lg')
  })

  it('shows initials and the bot mark with no picture for the author', () => {
    const { container } = renderAvatar({ author: 'contributor-a' }, { 'contributor-b': avatarDataURL })

    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('[data-avatar="initials"]')).toHaveTextContent('CA')

    const { container: botAvatar } = renderAvatar(
      { author: 'dependabot', authorIsBot: true },
      { dependabot: avatarDataURL },
    )
    expect(botAvatar.querySelector('img'), 'a person’s picture is never a bot’s').toBeNull()
    expect(botAvatar.querySelector('[data-avatar="bot"]')).toBeInTheDocument()
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

describe('KindMark', () => {
  it.each([
    [{}, 'Pull request', 'text-success'],
    [{ isDraft: true }, 'Draft pull request', 'text-muted'],
    [{ kind: 'issue' } as const, 'Issue', 'text-success'],
  ])('marks %o as its kind in a compact card’s column', (overrides, label, color) => {
    render(<KindMark item={item(overrides)} />)

    const mark = screen.getByRole('img', { name: label })
    expect(mark).toHaveAttribute('title', label)
    expect(mark).toHaveClass(color, 'relative', 'z-10')
  })
})
