import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { account, avatarDataURL, signedOut } from '../test/fixtures'
import { Avatar } from './Avatar'

// picture returns the rendered image; it is decorative, so it has no accessible role.
function picture(container: HTMLElement): HTMLImageElement | null {
  return container.querySelector('img')
}

describe('Avatar', () => {
  it('shows the cached picture', () => {
    const { container } = render(<Avatar account={account()} size="md" />)

    expect(picture(container)).toHaveAttribute('src', avatarDataURL)
    expect(picture(container)).toHaveAttribute('alt', '')
    expect(picture(container)).toHaveClass('size-9')
  })

  it("falls back to the name's initial when the picture cannot be shown", () => {
    const { container, rerender } = render(<Avatar account={account({ name: '  octo user' })} size="sm" />)

    const img = picture(container)
    expect(img).not.toBeNull()
    fireEvent.error(img as HTMLImageElement)

    expect(picture(container)).toBeNull()
    expect(screen.getByText('O')).toHaveClass('size-6')

    // A new picture gets a fresh try.
    rerender(<Avatar account={account({ avatar: 'data:image/png;base64,bmV3' })} size="sm" />)
    expect(picture(container)).toHaveAttribute('src', 'data:image/png;base64,bmV3')
  })

  it("uses the login's initial when the profile has no name or picture", () => {
    render(<Avatar account={account({ login: 'zed-user', name: ' ', avatar: '' })} size="lg" />)

    expect(screen.getByText('Z')).toHaveClass('size-14')
  })

  it('shows a person before there is an account', () => {
    const { container, rerender } = render(<Avatar account={null} size="sm" />)

    expect(container.querySelector('svg')).not.toBeNull()
    expect(container).toHaveTextContent('')

    rerender(<Avatar account={signedOut} size="sm" />)
    expect(container.querySelector('svg')).not.toBeNull()
  })
})
