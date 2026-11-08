import { describe, expect, it } from 'vitest'

import { item, list, project, settings, snapshot } from '../test/fixtures'
import {
  featuredFields,
  grantProjectsCommand,
  hasIssues,
  offersProjects,
  projectText,
  projectTooltip,
  subIssuesText,
} from './projects'

const withIssue = (overrides: Parameters<typeof snapshot>[0] = {}) =>
  snapshot({ assigned: list([item({ id: 2, kind: 'issue' })]), ...overrides })

describe('grantProjectsCommand', () => {
  it('adds the read-only projects scope to the GitHub CLI’s token', () => {
    expect(grantProjectsCommand).toBe('gh auth refresh -s read:project')
  })
})

describe('projectText', () => {
  it('names the project and the issue’s status there', () => {
    expect(projectText(project())).toBe('Roadmap: In progress')
  })

  it('names the project alone when the issue has no status there', () => {
    expect(projectText(project({ status: '' }))).toBe('Roadmap')
  })
})

describe('featuredFields', () => {
  const priority = { name: 'Priority', value: 'High', color: '#fa4549' }
  const size = { name: 'T-shirt size', value: 'XL', color: '' }

  it('features how urgent and how big an issue is, in that order, whatever the project’s order', () => {
    const fields = [
      { name: 'Phase', value: 'Beta', color: '' },
      size,
      { name: 'Estimate', value: '3', color: '' },
      priority,
    ]

    expect(featuredFields(project({ fields }))).toEqual([priority, size])
  })

  it('features the first field named like each, and nothing when none is', () => {
    const second = { name: 'Priority (old)', value: 'Low', color: '' }

    expect(featuredFields(project({ fields: [priority, second] }))).toEqual([priority])
    expect(featuredFields(project({ fields: [{ name: 'Phase', value: 'Beta', color: '' }] }))).toEqual([])
    expect(featuredFields(project())).toEqual([])
  })
})

describe('projectTooltip', () => {
  it('lists where the issue stands, every field, and that a click opens it', () => {
    const fields = [
      { name: 'Priority', value: 'High', color: '#fa4549' },
      { name: 'Target', value: 'Jun 27, 2026', color: '' },
    ]

    expect(projectTooltip(project({ fields }))).toBe(
      'Roadmap: In progress\nPriority: High\nTarget: Jun 27, 2026\nOpen the project on GitHub',
    )
  })
})

describe('subIssuesText', () => {
  it('says how many are done, of how many, as a share', () => {
    expect(subIssuesText({ total: 8, completed: 6 })).toBe('6 of 8 sub-issues done (75%)')
    expect(subIssuesText({ total: 1, completed: 0 })).toBe('0 of 1 sub-issue done (0%)')
    expect(subIssuesText({ total: 3, completed: 2 })).toBe('2 of 3 sub-issues done (67%)')
  })
})

describe('hasIssues', () => {
  it('finds an issue in any list', () => {
    expect(hasIssues(withIssue())).toBe(true)
    expect(hasIssues(snapshot({ reviewRequested: list([item({ kind: 'issue' })]) }))).toBe(true)
  })

  it('finds none among pull requests alone', () => {
    expect(hasIssues(snapshot({ authored: list([item()]) }))).toBe(false)
    expect(hasIssues(snapshot())).toBe(false)
  })
})

describe('offersProjects', () => {
  it('offers while the token cannot read projects and the inbox holds an issue', () => {
    expect(offersProjects(withIssue({ projectAccess: 'needs_scope' }), settings())).toBe(true)
  })

  it('offers nothing once the user put the offer away', () => {
    expect(offersProjects(withIssue({ projectAccess: 'needs_scope' }), settings({ projectsTipDismissed: true }))).toBe(
      false,
    )
  })

  it('offers nothing when the token reads projects, or before the first refresh', () => {
    expect(offersProjects(withIssue({ projectAccess: 'on' }), settings())).toBe(false)
    expect(offersProjects(withIssue({ projectAccess: '' }), settings())).toBe(false)
  })

  it('offers nothing without an issue to show projects on', () => {
    expect(offersProjects(snapshot({ projectAccess: 'needs_scope', authored: list([item()]) }), settings())).toBe(false)
  })

  it('waits for the inbox and the settings', () => {
    expect(offersProjects(null, settings())).toBe(false)
    expect(offersProjects(withIssue({ projectAccess: 'needs_scope' }), null)).toBe(false)
  })
})
