import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import { Logo, LogoMark } from './Logo'

describe('Logo', () => {
  it('pairs the mark with the lowercase wordmark, named by the product name alone', () => {
    const { container } = render(
      <h1>
        <Logo />
      </h1>,
    )

    expect(screen.getByRole('heading')).toHaveAccessibleName(brand.name)
    expect(screen.getByText(brand.name)).toHaveClass('lowercase', 'font-bold')
    expect(container.querySelector('svg')).toHaveAttribute('aria-hidden', 'true')
    expect(container.querySelector('svg')).toHaveClass('h-[1.125em]')
  })

  it('sizes with the font size it is given', () => {
    const { container } = render(<Logo className="text-2xl" />)

    expect(container.firstChild).toHaveClass('text-2xl', 'inline-flex')
    expect(container.firstChild).not.toHaveClass('text-base')
  })
})

describe('LogoMark', () => {
  it('draws the brand mark on its own grid, hidden from assistive technology', () => {
    const { container } = render(<LogoMark />)
    const svg = container.querySelector('svg')

    expect(svg).toHaveAttribute('aria-hidden', 'true')
    expect(svg).toHaveAttribute('viewBox', `0 0 ${String(brand.mark.width)} ${String(brand.mark.height)}`)
    expect(svg).toHaveClass('h-[18px]', 'w-auto')
    expect(container.querySelector('path')).toHaveAttribute('d', brand.mark.path)
  })

  it('fills each mark with its own gradient', () => {
    const { container } = render(
      <>
        <LogoMark className="size-4" />
        <LogoMark />
      </>,
    )

    const gradients = [...container.querySelectorAll('linearGradient')].map((g) => g.id)
    const fills = [...container.querySelectorAll('path')].map((p) => p.getAttribute('fill'))

    expect(new Set(gradients).size).toBe(2)
    expect(fills).toEqual(gradients.map((id) => `url(#${id})`))
    expect(container.querySelector('svg')).toHaveClass('size-4')
  })
})
