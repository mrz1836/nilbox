import { describe, expect, it } from 'vitest'

import { galleryTemplate, snoozeTemplate } from '../test/fixtures'
import {
  blankProblems,
  blankValues,
  filledSpec,
  freeName,
  missingStarters,
  modeHint,
  quickProblems,
  suggestedFirst,
  templateDraft,
} from './templates'

// flaky is a template with two blanks: the flaky checks' names, required, and a where.
function flaky() {
  return galleryTemplate({
    key: 're-run-flaky-checks-once',
    blanks: [
      { kind: 'checks', label: 'Flaky checks', help: '', source: 0, condition: 0, required: true },
      { kind: 'where', label: 'Where', help: '', source: 1, condition: 0, required: false },
    ],
    spec: {
      version: 1,
      watch: { sources: [{ lists: ['authored'] }, { search: { kind: 'pr', where: ['@me'] } }] },
      trigger: { event: 'checks_failed' },
      conditions: { match: 'all', of: [{ field: 'checks.failedNames', op: 'hasAny', values: [] }] },
      steps: [{ agent: { prompt: { starter: 'rerun-failed-jobs' } } }],
      limits: {},
    },
  })
}

describe('templates', () => {
  it('reads each blank as the template ships it, and fills it in where it sits', () => {
    const t = flaky()

    expect(blankValues(t)).toEqual([[], ['@me']])

    const spec = filledSpec(t, [['e2e / browser'], ['@me', 'org:acme-org']])

    expect(spec.conditions.of[0]?.values).toEqual(['e2e / browser'])
    expect(spec.watch.sources[1]?.search?.where).toEqual(['@me', 'org:acme-org'])
    expect(t.spec.conditions.of[0]?.values, 'the template itself is left as it is').toEqual([])
    expect(filledSpec(t, []), 'a blank left out keeps what the template ships').toEqual(t.spec)
  })

  it('reads and fills nothing where a blank points past its spec, and finds a checks blank’s problems', () => {
    const t = galleryTemplate({
      blanks: [
        { kind: 'where', label: 'Where', help: '', source: 4, condition: 0, required: false },
        { kind: 'checks', label: 'Checks', help: '', source: 0, condition: 4, required: false },
      ],
    })

    expect(blankValues(t)).toEqual([[], []])
    expect(filledSpec(t, [['@me'], ['ci']])).toEqual(t.spec)

    const at = { part: 'conditions' as const, field: 'of[0].values', message: 'add a name' }
    const checks = { kind: 'checks' as const, label: 'Checks', help: '', source: 0, condition: 0, required: true }

    expect(blankProblems(checks, [at])).toEqual([at])
  })

  it('makes a new routine of the template: its name, its mode, its key', () => {
    const t = galleryTemplate()
    const d = templateDraft(t, [['org:acme-org']], 'Fix Dependabot security failures 2', 'dryRun')

    expect(d).toMatchObject({
      id: 0,
      name: 'Fix Dependabot security failures 2',
      mode: 'dryRun',
      templateKey: 'fix-dependabot-security',
      spec: { watch: { sources: [{ search: { where: ['org:acme-org'] } }] } },
    })
  })

  it('names a routine apart from the user’s, ignoring case', () => {
    expect(freeName([], 'Snooze passing bot PRs')).toBe('Snooze passing bot PRs')
    expect(freeName([{ name: 'snooze passing bot prs' }], 'Snooze passing bot PRs')).toBe('Snooze passing bot PRs 2')
    expect(
      freeName([{ name: 'Snooze passing bot PRs' }, { name: 'Snooze passing bot PRs 2' }], 'Snooze passing bot PRs'),
    ).toBe('Snooze passing bot PRs 3')
  })

  it('puts the suggested first, each list in the gallery’s order', () => {
    const fortress = galleryTemplate({ suggested: 'One of your clones runs go-fortress’s security checks.' })
    const { suggested, rest } = suggestedFirst([snoozeTemplate(), fortress, flaky()])

    expect(suggested.map((t) => t.key)).toEqual(['fix-dependabot-security'])
    expect(rest.map((t) => t.key)).toEqual(['snooze-passing-bot-prs', 're-run-flaky-checks-once'])
  })

  it('shows the template’s mode as a hint alone, beside the one it starts in', () => {
    expect(modeHint(snoozeTemplate(), 'ask')).toBe(
      'This template suggests Autopilot. It starts in Ask first, as new routines do: change it here if you like.',
    )
    expect(modeHint(snoozeTemplate(), 'auto')).toBe('')
  })

  it('leaves out of quick setup’s problems what its needs and a starter to add back already say', () => {
    const where = { part: 'steps' as const, field: '[0].agent.where', message: 'can’t run in the background yet' }
    const prompt = { part: 'steps' as const, field: '[0].agent.prompt', message: 'that starter prompt isn’t…' }
    const blank = { part: 'watch' as const, field: 'sources[0].search.where', message: 'name a place' }

    expect(quickProblems(galleryTemplate(), [where, prompt, blank])).toEqual([prompt, blank])

    const lost = galleryTemplate({
      needs: [],
      prompts: [{ starter: 'fix-dependency-security', name: 'Fix dep security', inLibrary: false }],
    })

    expect(missingStarters(lost)).toHaveLength(1)
    expect(quickProblems(lost, [where, prompt, blank])).toEqual([where, blank])
    const whereBlank = { kind: 'where' as const, label: 'Where', help: '', source: 0, condition: 0, required: false }

    expect(blankProblems(whereBlank, [where, prompt, blank])).toEqual([blank])
  })
})
