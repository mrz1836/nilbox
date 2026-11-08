import { describe, expect, it } from 'vitest'

import { item, list, project, settings, snapshot } from '../test/fixtures'
import { grantProjectsCommand, hasIssues, offersProjects, projectText } from './projects'

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
