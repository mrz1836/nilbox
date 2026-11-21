import { render } from '@testing-library/react'
import { useId } from 'react'
import { describe, expect, it } from 'vitest'

import { domShape } from './dom'

// Labelled is a field and its label, with an ID of its own each time it renders.
function Labelled({ classes = 'b a' }: { classes?: string }) {
  const id = useId()

  return (
    <p className={classes} title="t">
      <label htmlFor={id}>Name</label>
      <input id={id} aria-describedby={`${id}-help other`} />
      ~/{'example-repo'}
    </p>
  )
}

describe('domShape', () => {
  it('writes each element and run of text, its attributes and classes sorted and its IDs numbered', () => {
    const { container } = render(<Labelled />)

    expect(domShape(container)).toBe(
      [
        '<div>',
        '  <p class="a b" title="t">',
        '    <label for="id-1">',
        '      "Name"',
        '    <input aria-describedby="id-2 id-3" id="id-1">',
        '    "~/example-repo"',
      ].join('\n'),
    )
  })

  it('reads two renders alike when only their IDs and class order differ', () => {
    const first = render(<Labelled classes="a b" />).container
    const second = render(<Labelled classes="b  a" />).container

    expect(domShape(second)).toBe(domShape(first))
  })

  it('tells renders apart by a class', () => {
    const first = render(<Labelled classes="a b" />).container
    const second = render(<Labelled classes="a c" />).container

    expect(domShape(second)).not.toBe(domShape(first))
  })
})
