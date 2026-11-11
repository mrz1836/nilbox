import { describe, expect, it } from 'vitest'

import { goPreCommitTool, integrationsConfig, integrationTool, integrationTools, scriptRun } from '../test/fixtures'
import { foundToolsText, scriptsBadge, scriptsBadgeTitle, showsScripts, toolOf, unusable, usable } from './scriptsTab'

// off is a tool switched off.
const off = { switch: 'off' as const, on: false }

describe('scriptsTab', () => {
  it('finds a tool by its integration, none before the list', () => {
    expect(toolOf(integrationTools(), 'go-pre-commit')?.binary).toBe('go-pre-commit')
    expect(toolOf(null, 'mage-x')).toBeUndefined()
  })

  it('tells a tool that is on and usable from one that is on and isn’t', () => {
    expect(usable(integrationTool())).toBe(true)
    expect(usable(integrationTool({ state: 'update' }))).toBe(true)
    expect(usable(integrationTool({ state: 'tooOld' }))).toBe(false)
    expect(usable(integrationTool(off))).toBe(false)
    expect(usable(undefined)).toBe(false)
    expect(unusable(integrationTool({ state: 'missing' }))).toBe(true)
    expect(unusable(integrationTool({ state: 'missing', on: false }))).toBe(false)
  })

  it('shows the tab while a tool is on, or Scripts was turned on (D2)', () => {
    const allOff = integrationTools({ tools: [integrationTool(off), goPreCommitTool(off)] })

    expect(showsScripts(integrationTools(), integrationsConfig())).toBe(true)
    expect(showsScripts(allOff, integrationsConfig())).toBe(false)
    expect(
      showsScripts(allOff, integrationsConfig({ switches: { ...integrationsConfig().switches, scripts: 'on' } })),
    ).toBe(true)
    expect(showsScripts(null, integrationsConfig())).toBe(false)
    expect(showsScripts(integrationTools(), null)).toBe(false)
  })

  it('badges the runs going, a run failed and unseen, and a tool on that can’t be used', () => {
    expect(scriptsBadge(null, null)).toEqual({ running: 0, failed: false, blocked: false })
    expect(
      scriptsBadge(
        [
          scriptRun({ id: 3, status: 'running' }),
          scriptRun({ id: 2, status: 'failed', seen: false }),
          scriptRun({ id: 1, status: 'failed', seen: true }),
        ],
        integrationTools({ tools: [integrationTool({ state: 'wontRun' })] }),
      ),
    ).toEqual({ running: 1, failed: true, blocked: true })
    // An update available shows no dot.
    expect(scriptsBadge([], integrationTools({ tools: [integrationTool({ state: 'update' })] })).blocked).toBe(false)
  })

  it('explains the badge', () => {
    expect(scriptsBadgeTitle({ running: 1, failed: false, blocked: false })).toBe('1 script running')
    expect(scriptsBadgeTitle({ running: 2, failed: true, blocked: true })).toBe(
      '2 scripts running, a run failed, a tool that’s on can’t be used',
    )
    expect(scriptsBadgeTitle({ running: 0, failed: true, blocked: false })).toBe('A run failed')
    expect(scriptsBadgeTitle({ running: 0, failed: false, blocked: false })).toBe('')
  })

  it('announces the tools found, by their commands’ names', () => {
    expect(foundToolsText(integrationTools())).toBe('Found magex and go-pre-commit. They’re in the new Scripts tab.')
    expect(foundToolsText(integrationTools({ tools: [integrationTool()] }))).toBe(
      'Found magex. Release is in the new Scripts tab.',
    )
    expect(foundToolsText(integrationTools({ tools: [integrationTool(off), goPreCommitTool()] }))).toBe(
      'Found go-pre-commit. It’s in the new Scripts tab.',
    )
    expect(foundToolsText(integrationTools({ tools: [integrationTool({ state: 'missing' })] }))).toBe('')
    expect(foundToolsText(null)).toBe('')
  })
})
