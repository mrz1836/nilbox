import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { ItemDetails } from '../../platform'
import { details, item } from '../../test/fixtures'
import { SizeGlyph } from './SizeGlyph'

function renderGlyph(d: Partial<ItemDetails> | null) {
  return render(<SizeGlyph item={item({ details: d === null ? null : details(d) })} />)
}

// cells reads the bar's five cells, in order.
function cells(container: HTMLElement): (string | null)[] {
  return [...container.querySelectorAll('[data-size-bar] [data-cell]')].map((cell) => cell.getAttribute('data-cell'))
}

describe('SizeGlyph', () => {
  it.each([
    ['XS with only deletions', 0, 5, 'XS', ['removed', 'off', 'off', 'off', 'off']],
    ['M with both', 120, 30, 'M', ['added', 'added', 'removed', 'off', 'off']],
    ['L with both', 236, 23, 'L', ['added', 'added', 'added', 'added', 'off']],
    ['XL', 1284, 312, 'XL', ['added', 'added', 'added', 'added', 'removed']],
    ['XL with one line added', 1, 600, 'XL', ['added', 'removed', 'removed', 'removed', 'removed']],
    ['no lines at all', 0, 0, 'XS', ['off', 'off', 'off', 'off', 'off']],
  ])('lights the cells of %s', (_name, additions, deletions, grade, want) => {
    const { container } = renderGlyph({ additions, deletions })

    expect(cells(container)).toEqual(want)
    expect(screen.getByText(grade)).toBeInTheDocument()
  })

  it('colors the lit cells for lines added and removed', () => {
    const { container } = renderGlyph({ additions: 120, deletions: 30 })
    const [first, , third, fourth] = container.querySelectorAll('[data-size-bar] [data-cell]')

    expect(first).toHaveClass('bg-success')
    expect(third).toHaveClass('bg-danger')
    expect(fourth).toHaveClass('bg-border')
  })

  it('shows the comments only when there are some', () => {
    const { container, rerender } = renderGlyph({ comments: 4 })

    expect(screen.getByText('4')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByText('4 comments')).toHaveClass('sr-only')

    rerender(<SizeGlyph item={item({ details: details({ comments: 0 }) })} />)
    expect(container).not.toHaveTextContent('comment')
    expect(container.querySelector('svg')).toBeNull()
  })

  it('says the size in words to a screen reader', () => {
    renderGlyph({ additions: 120, deletions: 30, changedFiles: 6, commits: 3 })

    expect(screen.getByText('Size M: 120 lines added and 30 removed across 6 files in 3 commits')).toHaveClass(
      'sr-only',
    )
  })

  it('is a placeholder bar while the details load', () => {
    const { container } = renderGlyph(null)

    expect(container.querySelector('.skeleton')).not.toBeNull()
    expect(container.textContent).toBe('')
  })
})
