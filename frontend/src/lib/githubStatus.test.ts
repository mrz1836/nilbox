import { describe, expect, it } from 'vitest'

import { githubIncident, githubStatus } from '../test/fixtures'
import { brief, headline, phrase, statusLabel, toneOf, troubled } from './githubStatus'

describe('githubStatus', () => {
  it('shows only a reading that says something is wrong', () => {
    expect(troubled(null)).toBe(false)
    expect(troubled(githubStatus())).toBe(false)
    expect(troubled(githubStatus({ state: 'off' }))).toBe(false)
    expect(troubled(githubStatus({ state: 'unknown' }))).toBe(false)
    expect(troubled(githubIncident())).toBe(true)
    expect(troubled(githubStatus({ state: 'maintenance' }))).toBe(true)
  })

  it('warns of a minor incident or maintenance, and an outage louder', () => {
    expect(toneOf('minor')).toBe('warning')
    expect(toneOf('maintenance')).toBe('warning')
    expect(toneOf('major')).toBe('danger')
    expect(toneOf('critical')).toBe('danger')
    expect(toneOf('operational')).toBeNull()
  })

  it('words the reading in the page’s own words, in sentence case', () => {
    expect(headline(githubIncident())).toBe('Partial system outage')
    expect(statusLabel(githubIncident())).toBe('GitHub status: Partial system outage')
    expect(headline(githubStatus({ state: 'critical', description: '  ' }))).toBe('Major system outage')
  })

  it('words a state briefly for a menu hint', () => {
    expect(brief('major')).toBe('Partial outage')
    expect(brief('maintenance')).toBe('Maintenance')
    expect(brief('operational')).toBe('')
  })

  it('turns the page’s snake_case words into a phrase', () => {
    expect(phrase('partial_outage')).toBe('Partial outage')
    expect(phrase('degraded_performance')).toBe('Degraded performance')
    expect(phrase('investigating')).toBe('Investigating')
    expect(phrase('')).toBe('')
  })
})
