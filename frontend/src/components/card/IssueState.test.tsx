import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { ItemDetails } from '../../platform'
import { details, item, project } from '../../test/fixtures'
import { IssueState } from './IssueState'

function renderState(d: Partial<ItemDetails> | null) {
  return render(<IssueState item={item({ kind: 'issue', details: d === null ? null : details(d) })} />)
}

describe('IssueState', () => {
  it('shows the first project’s status as a pill tinted with its color', () => {
    renderState({ projects: [project(), project({ title: 'Backlog', status: 'Todo' })] })

    const pill = screen.getByText('In progress').parentElement
    expect(pill).toHaveStyle({ backgroundColor: '#bf870029' })
    expect(screen.queryByText('Todo')).not.toBeInTheDocument()
  })

  it('shows the project’s first featured field as an outlined pill', () => {
    renderState({
      projects: [
        project({
          status: '',
          fields: [
            { name: 'Iteration', value: 'Sprint 4', color: '' },
            { name: 'Size', value: 'M', color: '' },
            { name: 'Priority', value: 'P2', color: '#e9b44c' },
          ],
        }),
      ],
    })

    expect(screen.getByText('P2').parentElement).toHaveStyle({ borderColor: '#e9b44cb3' })
    expect(screen.getByText('Priority')).toHaveClass('sr-only')
    expect(screen.queryByText('M')).not.toBeInTheDocument()
    expect(screen.queryByText('Sprint 4')).not.toBeInTheDocument()
    expect(screen.queryByText('In progress')).not.toBeInTheDocument()
  })

  it('shows the sub-issues as a ring and how many are done, green once all are', () => {
    const { container, rerender } = renderState({ subIssues: { total: 5, completed: 3 } })

    const ring = container.querySelector('[data-sub-issues]')
    expect(ring).toHaveTextContent('3/5')
    expect(ring).toHaveClass('text-muted')
    expect(screen.getByText('Sub-issues: 3 of 5 sub-issues done (60%)')).toHaveClass('sr-only')

    rerender(<IssueState item={item({ kind: 'issue', details: details({ subIssues: { total: 5, completed: 5 } }) })} />)
    expect(container.querySelector('[data-sub-issues]')).toHaveClass('text-success')
  })

  it('shows them all together', () => {
    const { container } = renderState({
      projects: [project({ fields: [{ name: 'Priority', value: 'P1', color: '' }] })],
      subIssues: { total: 2, completed: 1 },
    })

    expect(container).toHaveTextContent('In progress')
    expect(container).toHaveTextContent('P1')
    expect(container).toHaveTextContent('1/2')
  })

  it('shows nothing for an issue with none of them, nor while the details load', () => {
    const { container, rerender } = renderState({ projects: [project({ status: '' })] })

    expect(container).toBeEmptyDOMElement()

    rerender(<IssueState item={item({ kind: 'issue', details: null })} />)
    expect(container).toBeEmptyDOMElement()
  })
})
