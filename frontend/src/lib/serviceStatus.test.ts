import { describe, expect, it } from 'vitest'

import { claudeOutage, serviceProvider, serviceStatus } from '../test/fixtures'
import {
  affectsWords,
  othersOnPage,
  othersWords,
  stageLabel,
  stateLabel,
  statusLabel,
  toneOf,
  troubled,
  watched,
  worst,
} from './serviceStatus'

describe('serviceStatus', () => {
  it('shows only the providers in trouble, the worst first', () => {
    expect(troubled(null)).toEqual([])
    expect(troubled(serviceStatus())).toEqual([])
    expect(troubled(serviceStatus({ state: 'off' }, serviceProvider({ state: 'off' })))).toEqual([])
    expect(troubled(serviceStatus({}, serviceProvider({ state: 'unknown' })))).toEqual([])

    const github = serviceProvider({ state: 'minor' })
    const openai = serviceProvider({ id: 'openai', name: 'OpenAI', state: 'critical' })
    const status = serviceStatus({ state: 'critical' }, github, claudeOutage(), openai)

    expect(troubled(status).map((p) => p.id)).toEqual(['openai', 'claude', 'github'])
    expect(worst(troubled(status))).toBe('critical')
    expect(worst([])).toBe('operational')
    expect(troubled(serviceStatus({}, serviceProvider({ state: 'maintenance' })))).toHaveLength(1)
  })

  it('lists the providers watched, in the catalog’s order', () => {
    expect(watched(null)).toEqual([])
    expect(watched(serviceStatus()).map((p) => p.name)).toEqual(['GitHub', 'Claude', 'OpenAI'])
    expect(
      watched(serviceStatus({}, serviceProvider({ id: 'claude', name: 'Claude', state: 'off' }))).map((p) => p.name),
    ).toEqual(['GitHub', 'OpenAI'])
  })

  it('warns of degraded performance or maintenance, and an outage louder', () => {
    expect(toneOf('minor')).toBe('warning')
    expect(toneOf('maintenance')).toBe('warning')
    expect(toneOf('major')).toBe('danger')
    expect(toneOf('critical')).toBe('danger')
    expect(toneOf('operational')).toBeNull()
    expect(toneOf('unknown')).toBeNull()
    expect(toneOf('off')).toBeNull()
  })

  it('words each state as Go’s banners do', () => {
    expect(stateLabel('operational')).toBe('Operational')
    expect(stateLabel('maintenance')).toBe('Maintenance')
    expect(stateLabel('minor')).toBe('Degraded')
    expect(stateLabel('major')).toBe('Partial outage')
    expect(stateLabel('critical')).toBe('Major outage')
  })

  it('words where an incident stands', () => {
    expect(stageLabel('investigating')).toBe('Investigating')
    expect(stageLabel('identified')).toBe('Identified')
    expect(stageLabel('monitoring')).toBe('Monitoring a fix')
    expect(stageLabel('in_progress')).toBe('Under way')
    expect(stageLabel('verifying')).toBe('Verifying')
    expect(stageLabel('other')).toBe('')
  })

  it('names the warning for each provider in trouble', () => {
    expect(statusLabel([claudeOutage()])).toBe('Service status: Partial outage on Claude')
    expect(statusLabel([claudeOutage(), serviceProvider({ state: 'minor' })])).toBe(
      'Service status: Partial outage on Claude, Degraded on GitHub',
    )
  })

  it('names the parts affected, and the incidents elsewhere on the page', () => {
    expect(affectsWords(claudeOutage())).toBe('Affects: Claude API')
    expect(affectsWords(claudeOutage({ affected: [] }))).toBe('')
    expect(othersWords(0)).toBe('')
    expect(othersWords(1)).toBe('1 other incident doesn’t touch what you watch')
    expect(othersWords(3)).toBe('3 other incidents don’t touch what you watch')
    expect(othersOnPage(0)).toBe('')
    expect(othersOnPage(1)).toBe('1 other incident on the page now')
    expect(othersOnPage(2)).toBe('2 other incidents on the page now')
  })
})
