import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import { userScript } from '../test/fixtures'
import {
  aboutScript,
  isChosen,
  newUserScript,
  placeName,
  placeOf,
  readyToSave,
  repoChoices,
  runsInLabel,
  scopeWords,
  toggleRepo,
} from './userScripts'

describe('newUserScript', () => {
  it('starts a new script for every repository, run where runs go, without asking', () => {
    expect(newUserScript()).toEqual({
      id: 0,
      name: '',
      command: '',
      icon: 'play',
      scope: 'all',
      repos: [],
      runsIn: '',
      confirm: false,
    })
  })
})

describe('places', () => {
  it('names the app and the terminal, and a script’s own place over the setting', () => {
    expect(placeName('app', 'iTerm2')).toBe(brand.name)
    expect(placeName('terminal', 'iTerm2')).toBe('iTerm2')

    expect(placeOf(userScript(), 'terminal')).toBe('terminal')
    expect(placeOf(userScript({ runsIn: 'app' }), 'terminal')).toBe('app')

    expect(runsInLabel('')).toBe('As set in Settings')
    expect(runsInLabel('app')).toBe(brand.name)
    expect(runsInLabel('terminal')).toBe('Terminal')
  })
})

describe('aboutScript', () => {
  it('says where a script shows, where it runs when it says, and that it asks first', () => {
    expect(aboutScript(userScript(), 'iTerm2')).toBe('All repositories')
    expect(aboutScript(userScript({ scope: 'go', runsIn: 'terminal', confirm: true }), 'iTerm2')).toBe(
      'Go repositories · Runs in iTerm2 · Asks before running',
    )
    expect(scopeWords(userScript({ scope: 'repos', repos: ['acme-org/sdk', 'acme-org/web'] }))).toBe(
      'acme-org/sdk, acme-org/web',
    )
    expect(aboutScript(userScript({ runsIn: 'app' }), 'iTerm2')).toBe(`All repositories · Runs in ${brand.name}`)
  })
})

describe('repository choices', () => {
  it('offers the clones’ repositories and those chosen, once each in any case, sorted', () => {
    expect(repoChoices(['acme-org/web', 'Acme-Org/SDK', 'acme-org/api'], ['acme-org/sdk', 'acme-org/gone'])).toEqual([
      'acme-org/api',
      'acme-org/gone',
      'acme-org/sdk',
      'acme-org/web',
    ])
    expect(repoChoices(['b/x', 'B/x', 'a/x'], [])).toEqual(['a/x', 'b/x'])
  })

  it('chooses and unchooses a repository in any case', () => {
    expect(toggleRepo(['acme-org/sdk'], 'acme-org/web', true)).toEqual(['acme-org/sdk', 'acme-org/web'])
    expect(toggleRepo(['Acme-Org/SDK', 'acme-org/web'], 'acme-org/sdk', false)).toEqual(['acme-org/web'])
    expect(isChosen(['Acme-Org/SDK'], 'acme-org/sdk')).toBe(true)
    expect(isChosen([], 'acme-org/sdk')).toBe(false)
  })
})

describe('readyToSave', () => {
  it('needs a name, a command, and a repository when they’re chosen', () => {
    expect(readyToSave(userScript())).toBe(true)
    expect(readyToSave(userScript({ name: ' ' }))).toBe(false)
    expect(readyToSave(userScript({ command: '' }))).toBe(false)
    expect(readyToSave(userScript({ scope: 'repos' }))).toBe(false)
    expect(readyToSave(userScript({ scope: 'repos', repos: ['acme-org/sdk'] }))).toBe(true)
  })
})
