import { describe, expect, it } from 'vitest'

import type { SetupStep } from '../platform'
import {
  agents,
  agentVersions,
  githubSignIn,
  goPreCommitTool,
  integrationTool,
  integrationTools,
  list,
  locationState,
  item,
  setupSignals,
  snapshot,
} from '../test/fixtures'
import {
  agentsNeedAttention,
  announceText,
  eyebrowText,
  finishLog,
  goDuration,
  inboxPreview,
  nextStep,
  numberedSteps,
  previousStep,
  progressOf,
  progressText,
  reachedThrough,
  rootNeedsAttention,
  setupFromURL,
  stepsOf,
  stepStatus,
  toolsFit,
  toolsReason,
  urlWithoutSetup,
  welcomeText,
  type FinishFacts,
} from './setup'

describe('the setup parameter', () => {
  it.each([
    ['?setup=1', true],
    ['?textSize=large&setup=1', true],
    ['?setup=0', false],
    ['?textSize=large', false],
    ['', false],
  ])('reads %j as %s', (search, want) => {
    expect(setupFromURL(search)).toBe(want)
  })

  it('drops the parameter, keeping every other', () => {
    expect(urlWithoutSetup('wails://localhost/?setup=1&textSize=large')).toBe('wails://localhost/?textSize=large')
    expect(urlWithoutSetup('wails://localhost/?textSize=large')).toBeNull()
  })
})

describe('the steps', () => {
  it('shows Tools only when it fits', () => {
    expect(stepsOf(false)).toEqual(['welcome', 'github', 'clones', 'agents', 'notifications', 'finish'])
    expect(stepsOf(true)).toEqual(['welcome', 'github', 'clones', 'agents', 'notifications', 'tools', 'finish'])
    expect(numberedSteps(true)).toHaveLength(5)
  })

  it('moves forward and back, and stops at the ends', () => {
    expect(nextStep('welcome', false)).toBe('github')
    expect(nextStep('notifications', false)).toBe('finish')
    expect(nextStep('notifications', true)).toBe('tools')
    expect(nextStep('finish', true)).toBe('finish')
    expect(previousStep('github', false)).toBe('welcome')
    expect(previousStep('finish', true)).toBe('tools')
    expect(previousStep('welcome', true)).toBe('welcome')
  })

  it.each<[SetupStep, boolean, string]>([
    ['welcome', false, 'Getting started'],
    ['github', true, 'Step 1 of 5 · 4 to go'],
    ['clones', true, 'Step 2 of 5 · 3 to go'],
    ['notifications', false, 'Step 4 of 4 · last one'],
    ['tools', true, 'Step 5 of 5 · last one'],
    ['finish', true, 'All 5 steps done'],
  ])('says where %s stands, with Tools %s: %s', (step, tools, want) => {
    expect(progressText(step, tools)).toBe(want)
  })

  it('numbers the steps that count', () => {
    expect(progressOf('welcome', false)).toEqual({ number: 0, total: 4, left: 4 })
    expect(progressOf('agents', false)).toEqual({ number: 3, total: 4, left: 1 })
    expect(progressOf('finish', false)).toEqual({ number: 5, total: 4, left: 0 })
    expect(progressOf('tools', false)).toEqual({ number: 5, total: 4, left: 0 })
    expect(welcomeText(false)).toBe('4 quick steps · about 2 minutes')
  })

  it('heads and announces each step', () => {
    expect(eyebrowText('clones', true)).toBe('Step 2 of 5')
    expect(eyebrowText('welcome', true)).toBe('')
    expect(announceText('clones', true)).toBe('Step 2 of 5: Find your clones')
    expect(announceText('finish', true)).toBe('You’re all set')
  })
})

describe('the rail', () => {
  const reached = reachedThrough('agents', true)

  it('reaches every step up to the one the setup opens on', () => {
    expect([...reached]).toEqual(['welcome', 'github', 'clones', 'agents'])
    expect([...reachedThrough('welcome', false)]).toEqual(['welcome'])
  })

  it.each<[SetupStep, string]>([
    ['github', 'attention'],
    ['clones', 'done'],
    ['agents', 'current'],
    ['notifications', 'upcoming'],
    ['tools', 'suggested'],
  ])('shows %s as %s', (step, want) => {
    expect(stepStatus(step, 'agents', reached, new Set(['github', 'notifications']))).toBe(want)
  })
})

describe('the Tools step', () => {
  const missing = integrationTools({
    tools: [integrationTool({ state: 'missing' }), goPreCommitTool({ state: 'missing' })],
  })

  it('fits when a tool is installed, or a clone points to one', () => {
    expect(toolsFit(integrationTools(), null)).toBe(true)
    expect(toolsFit(missing, null)).toBe(false)
    expect(toolsFit(missing, setupSignals({ clones: 9 }))).toBe(false)
    expect(toolsFit(missing, setupSignals({ clones: 9, goModules: 1 }))).toBe(true)
    expect(toolsFit(null, setupSignals({ fortress: 2 }))).toBe(true)
  })

  it('says why it is suggested', () => {
    const onlyMage = integrationTools({ tools: [integrationTool(), goPreCommitTool({ state: 'missing' })] })

    expect(toolsReason(onlyMage, setupSignals({ mageX: 12, goPreCommit: 9, goModules: 30 }))).toBe(
      'Suggested because mage-x is installed · 12 clones use mage-x · 9 use go-pre-commit · 30 are Go modules.',
    )
    expect(toolsReason(missing, setupSignals({ goModules: 1 }))).toBe('Suggested because 1 clone is a Go module.')
    expect(toolsReason(integrationTools(), setupSignals())).toBe(
      'Suggested because mage-x and go-pre-commit are installed.',
    )
    expect(toolsReason(missing, setupSignals())).toBe('')
  })
})

describe('what needs attention', () => {
  it('flags a clone root that is not there', () => {
    expect(rootNeedsAttention(locationState({ state: 'missing' }))).toBe(true)
    expect(rootNeedsAttention(locationState({ clones: 0 }))).toBe(false)
    expect(rootNeedsAttention(undefined)).toBe(false)
  })

  it('flags no agent installed', () => {
    expect(agentsNeedAttention(agents())).toBe(false)
    expect(agentsNeedAttention(agents().map((a) => ({ ...a, installed: false })))).toBe(true)
    expect(agentsNeedAttention([])).toBe(false)
  })
})

describe('the Finish log', () => {
  const facts: FinishFacts = {
    github: githubSignIn({ scopes: ['repo', 'read:org'] }),
    cloneRoot: '~/projects',
    root: locationState({ clones: 42 }),
    locations: 0,
    agents: agents(),
    versions: agentVersions(),
    defaultAgent: 'claude',
    terminal: 'iterm2',
    terminalName: 'iTerm2',
    worktrees: true,
    banners: 'on',
    menuBar: true,
    tools: [],
  }

  it('prints a line for each part', () => {
    expect(finishLog(facts).map((l) => [l.mark, l.name, l.text])).toEqual([
      ['ok', 'github', '@octo-user · repo, read:org'],
      ['ok', 'clones', '42 in ~/projects'],
      ['ok', 'agents', 'Claude 2.1.319 (default) · Codex 0.163.0'],
      ['ok', 'terminal', 'iTerm2 · worktrees on'],
      ['ok', 'notify', 'banners on · menu bar on'],
      ['skip', 'tools', 'skipped'],
    ])
  })

  it('warns of what needs attention, with the step that fixes it', () => {
    const lines = finishLog({
      ...facts,
      github: githubSignIn({ state: 'signed_out' }),
      root: locationState({ state: 'missing' }),
      locations: 2,
      agents: agents().map((a) => ({ ...a, installed: false })),
      banners: 'off',
      menuBar: false,
      tools: null,
    })

    expect(lines.map((l) => [l.mark, l.text, l.step])).toEqual([
      ['warn', 'not connected yet', 'github'],
      ['warn', '~/projects not found + 2 more locations', 'clones'],
      ['warn', 'no coding agent found', 'agents'],
      ['ok', 'iTerm2 · worktrees on', 'agents'],
      ['ok', 'banners off · menu bar off', 'notifications'],
    ])
  })

  it('names an empty clone root, and a token that reports no scopes', () => {
    const lines = finishLog({
      ...facts,
      github: githubSignIn({ unverified: true, scopes: [] }),
      root: locationState({ clones: 0 }),
      tools: ['mage-x'],
    })

    expect(lines[0]?.text).toBe('@octo-user · scopes not reported')
    expect(lines[1]?.text).toBe('~/projects, empty for now')
    expect(lines[5]).toEqual({ mark: 'ok', name: 'tools', text: 'mage-x', step: 'tools' })
  })

  it.each([
    [0, '0s'],
    [800, '1s'],
    [42_000, '42s'],
    [102_000, '1m42s'],
    [3_723_000, '1h2m3s'],
  ])('writes %i ms as %s', (ms, want) => {
    expect(goDuration(ms)).toBe(want)
  })
})

describe('the inbox preview', () => {
  it('counts each list once the inbox has refreshed', () => {
    expect(inboxPreview(null)).toBe('')
    expect(inboxPreview(snapshot())).toBe('')
    expect(
      inboxPreview(
        snapshot({
          lastRefreshedAt: '2026-06-15T12:00:00Z',
          reviewRequested: list([item()], 5),
          authored: list([], 4),
          assigned: list([], 1),
        }),
      ),
    ).toBe('5 review requests · 4 authored · 1 assigned')
  })
})
