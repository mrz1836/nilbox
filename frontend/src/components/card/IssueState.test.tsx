import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { ItemDetails } from '../../platform'
import { details, item, project } from '../../test/fixtures'
import { IssueGrade, IssueState, IssueTrack } from './IssueState'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

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

describe('IssueTrack', () => {
  function renderTrack(d: Partial<ItemDetails> | null) {
    return render(<IssueTrack item={item({ kind: 'issue', details: d === null ? null : details(d) })} />)
  }

  it('rings the sub-issues, and says them with the project’s status', () => {
    renderTrack({ projects: [project()], subIssues: { total: 5, completed: 3 } })

    const ring = screen.getByRole('img', { name: 'Roadmap: In progress · 3 of 5 sub-issues done (60%)' })
    expect(ring).toHaveAttribute('title', 'Roadmap: In progress · 3 of 5 sub-issues done (60%)')
    expect(ring).toHaveClass('relative', 'z-10')
    expect(ring.querySelector('svg')).not.toBeNull()
  })

  it('rings the sub-issues of an issue in no project', () => {
    renderTrack({ subIssues: { total: 2, completed: 2 } })

    expect(screen.getByRole('img', { name: '2 of 2 sub-issues done (100%)' })).toBeInTheDocument()
  })

  it('shows a dot in the status’s color without sub-issues, muted when the status has none', () => {
    const { container, rerender } = renderTrack({ projects: [project()] })

    expect(screen.getByRole('img', { name: 'Roadmap: In progress' })).toBeInTheDocument()
    expect(container.querySelector('[data-status-dot]')).toHaveStyle({ backgroundColor: '#bf8700' })

    rerender(
      <IssueTrack item={item({ kind: 'issue', details: details({ projects: [project({ statusColor: '' })] }) })} />,
    )
    expect(container.querySelector('[data-status-dot]')).toHaveClass('bg-muted')
  })

  it('shows nothing without a status or sub-issues, or while the details load', () => {
    const { container, rerender } = renderTrack({ projects: [project({ status: '' })] })

    expect(container).toBeEmptyDOMElement()

    rerender(<IssueTrack item={item({ kind: 'issue' })} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe('IssueGrade', () => {
  it('shows the first project’s first featured field, named in its tooltip', () => {
    render(
      <IssueGrade
        item={item({
          kind: 'issue',
          details: details({ projects: [project({ fields: [{ name: 'Priority', value: 'P1', color: '' }] })] }),
        })}
      />,
    )

    const grade = screen.getByTitle('Priority: P1')
    expect(grade).toHaveTextContent('Priority P1')
    expect(grade).toHaveClass('relative', 'z-10')
  })

  it('shows nothing without a featured field, a project, or the details', () => {
    const { container, rerender } = render(
      <IssueGrade item={item({ kind: 'issue', details: details({ projects: [project()] }) })} />,
    )

    expect(container).toBeEmptyDOMElement()

    rerender(<IssueGrade item={item({ kind: 'issue', details: details() })} />)
    expect(container).toBeEmptyDOMElement()

    rerender(<IssueGrade item={item({ kind: 'issue' })} />)
    expect(container).toBeEmptyDOMElement()
  })
})
