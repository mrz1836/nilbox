import { describe, expect, it } from 'vitest'

import { launchDefaults, resolution } from '../test/fixtures'
import {
  defaultsInput,
  emptyDefaults,
  findDefaults,
  hasChoices,
  joinList,
  launchRequest,
  noChoices,
  savableChoices,
  saveStatus,
  shadowedChoices,
  withChoices,
} from './launchChoices'

describe('choices', () => {
  it('knows when anything was chosen, and what can be saved', () => {
    expect(hasChoices(noChoices)).toBe(false)
    expect(hasChoices({ ...noChoices, model: 'opus' })).toBe(true)
    expect(hasChoices({ ...noChoices, override: true })).toBe(true)
    expect(savableChoices({ ...noChoices, override: true })).toBe(false)
    expect(savableChoices({ ...noChoices, worktree: false })).toBe(true)
  })

  it('turns the choices into a launch request, the override only for untrusted items', () => {
    const c = { ...noChoices, promptId: 2, profile: 'work', planMode: true, override: true }

    expect(launchRequest(7, c, true)).toEqual({
      itemId: 7,
      profile: 'work',
      promptId: 2,
      model: null,
      effort: null,
      planMode: true,
      worktree: null,
      fullAccessOverride: true,
      confirmed: '',
    })
    expect(launchRequest(7, c, false).fullAccessOverride).toBe(false)
  })
})

describe('launch defaults', () => {
  it('finds stored defaults ignoring case', () => {
    const all = [launchDefaults(), launchDefaults({ name: 'api' })]

    expect(findDefaults(all, 'ACME-ORG', '')).toBe(all[0])
    expect(findDefaults(all, 'acme-org', 'API')).toBe(all[1])
    expect(findDefaults(all, 'acme-org', 'web')).toBeUndefined()
  })

  it('writes the choices over stored defaults, keeping what was not chosen', () => {
    const base = defaultsInput(launchDefaults({ effort: 'max', extraFlags: '--advisor fable' }))

    expect(withChoices(base, { ...noChoices, promptId: 3, model: 'opus', worktree: false }, 'pr')).toEqual({
      ...base,
      prPromptId: 3,
      model: 'opus',
      worktree: false,
    })
    expect(
      withChoices(base, { ...noChoices, promptId: 4, profile: 'alt', effort: 'low', planMode: true }, 'issue'),
    ).toEqual({ ...base, issuePromptId: 4, profile: 'alt', effort: 'low', planMode: true })
  })

  it('starts empty defaults with nothing set', () => {
    expect(emptyDefaults('acme-org', 'api')).toEqual({
      owner: 'acme-org',
      name: 'api',
      agent: '',
      profile: '',
      prPromptId: null,
      issuePromptId: null,
      model: '',
      effort: '',
      planMode: null,
      worktree: null,
      extraFlags: '',
    })
  })
})

describe('saving defaults that would not apply', () => {
  // r is a resolution under the Quick look prompt, which sets the model, in a repository
  // that sets its own profile.
  const r = resolution({
    profile: { value: 'work', source: 'repo', inherited: 'work', inheritedSource: 'repo' },
    prompt: { ...resolution().prompt, id: 2, nickname: 'Quick look', inheritedId: 1, inheritedSource: 'app' },
    model: { value: 'sonnet', source: 'prompt', inherited: 'sonnet', inheritedSource: 'prompt' },
  })

  it('finds the choices the prompt or the repository overrides', () => {
    const c = { ...noChoices, profile: 'default', promptId: 2, model: 'opus', effort: 'max' }

    expect(shadowedChoices(c, r, 'repo')).toEqual({ byPrompt: ['model'], byRepo: [] })
    expect(shadowedChoices(c, r, 'org')).toEqual({ byPrompt: ['model'], byRepo: ['profile'] })
    expect(shadowedChoices(noChoices, r, 'org')).toEqual({ byPrompt: [], byRepo: [] })
  })

  it('says so when saving', () => {
    expect(saveStatus('acme-org/api', 'Quick look', 'acme-org/api', { byPrompt: [], byRepo: [] })).toEqual({
      tone: 'success',
      text: 'Saved as the default for acme-org/api.',
    })
    expect(
      saveStatus('every acme-org repository', 'Quick look', 'acme-org/api', {
        byPrompt: ['model', 'effort'],
        byRepo: ['profile'],
      }),
    ).toEqual({
      tone: 'warning',
      text:
        'Saved as the default for every acme-org repository. “Quick look” sets its own model and effort, so they ' +
        'apply only with other prompts. acme-org/api has its own profile, which still wins there.',
    })
    expect(saveStatus('x', 'Quick look', 'x', { byPrompt: ['model'], byRepo: ['profile', 'effort'] }).text).toContain(
      'so it applies only with other prompts. x has its own profile and effort, which still win there.',
    )
  })

  it('joins names for a sentence', () => {
    expect(joinList([])).toBe('')
    expect(joinList(['model'])).toBe('model')
    expect(joinList(['model', 'effort'])).toBe('model and effort')
    expect(joinList(['model', 'effort', 'plan mode'])).toBe('model, effort, and plan mode')
  })
})
