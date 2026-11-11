import { describe, expect, it } from 'vitest'

import type { ReleaseCloneState, ScriptRunProgress } from '../platform'
import { releaseCandidate, releaseResult, releaseRun, scriptRun } from '../test/fixtures'
import {
  cloneNotes,
  commitChecksText,
  intentOf,
  matchCandidates,
  outcomeKind,
  recentReleases,
  recentWords,
  releasable,
  releaseSteps,
  resultHeading,
  unreleasedCount,
  type ReleaseIntent,
} from './release'

// clone is a clone on master with nothing to say about it.
function clone(change: Partial<ReleaseCloneState> = {}): ReleaseCloneState {
  return {
    branch: 'master',
    detached: false,
    dirty: 0,
    notPushed: 0,
    behind: false,
    localOnly: [],
    unreadable: false,
    ...change,
  }
}

// progress is a release's progress, at a step and working on another.
function progress(step: string, working = ''): ScriptRunProgress {
  return { step, working, stopRefused: '', stopConfirm: '' }
}

const intent: ReleaseIntent = { version: 'v1.4.3', branch: 'master', original: 'feature-x' }

describe('release', () => {
  it('offers Release… for the groups with something to release', () => {
    expect(releasable('unreleased')).toBe(true)
    expect(releasable('noReleases')).toBe(true)
    expect(releasable('upToDate')).toBe(false)
    expect(releasable('cantRelease')).toBe(false)
  })

  it('searches the repositories by owner and name, trimmed and in any case', () => {
    const repos = [releaseCandidate(), releaseCandidate({ repo: 'acme-org/web' })]

    expect(matchCandidates(repos, '  SDK ').map((r) => r.repo)).toEqual(['acme-org/sdk'])
    expect(matchCandidates(repos, '')).toHaveLength(2)
  })

  it('counts the unreleased repositories, none before the list', () => {
    expect(unreleasedCount([releaseCandidate(), releaseCandidate({ group: 'upToDate' })])).toBe(1)
    expect(unreleasedCount(undefined)).toBe(0)
  })

  it('says what matters about a clone, and nothing about one on its default branch', () => {
    expect(cloneNotes(null, 'master')).toEqual([])
    expect(cloneNotes(clone(), 'master')).toEqual([])
    expect(cloneNotes(clone({ branch: '' }), 'master')).toEqual([])
    expect(cloneNotes(clone({ unreadable: true, dirty: 3 }), 'master')).toEqual(['couldn’t read the clone'])
    expect(
      cloneNotes(clone({ branch: 'feature-x', dirty: 2, notPushed: 3, behind: true, localOnly: ['v1.4.3'] }), 'master'),
    ).toEqual(['on feature-x', '2 uncommitted', '3 not pushed', 'behind origin', 'v1.4.3 only on this Mac'])
    expect(cloneNotes(clone({ detached: true, branch: '' }), 'master')).toEqual(['on a detached commit'])
  })

  it('words the checks on a commit', () => {
    expect(commitChecksText('SUCCESS')).toBe('checks passed')
    expect(commitChecksText('PENDING')).toBe('checks running')
    expect(commitChecksText('EXPECTED')).toBe('checks running')
    expect(commitChecksText('FAILURE')).toBe('checks failing')
    expect(commitChecksText('ERROR')).toBe('checks failing')
    expect(commitChecksText('')).toBe('no checks')
  })

  it('reads what a release set out to do, and nothing from a run that kept none', () => {
    expect(intentOf(releaseRun())).toEqual(intent)
    expect(intentOf(scriptRun())).toEqual({ version: '', branch: '', original: '' })
    expect(intentOf(scriptRun({ result: { ...scriptRun().result, intent: { version: 7 } } }))).toEqual({
      version: '',
      branch: '',
      original: '',
    })
  })

  it('reads how a release came out, and nothing from a run without a known result', () => {
    expect(outcomeKind(releaseRun())).toBe('released')
    expect(outcomeKind(scriptRun())).toBe('')
    expect(outcomeKind(releaseRun({ result: { ...releaseRun().result, outcome: { kind: 'other' } } }))).toBe('')
  })

  it('lists the newest releases, ten at most, from the runs', () => {
    const runs = [scriptRun(), ...Array.from({ length: 12 }, (_, i) => releaseRun({ id: 20 - i }))]

    expect(recentReleases(runs).map((r) => r.id)).toEqual([20, 19, 18, 17, 16, 15, 14, 13, 12, 11])
    expect(recentReleases(null)).toEqual([])
  })

  it('says where a recent release is', () => {
    const ended = (outcome: unknown) => releaseRun({ result: { ...releaseRun().result, outcome } })

    expect(recentWords(releaseRun({ status: 'running' }))).toBe('running')
    expect(recentWords(ended({ kind: 'released' }))).toBe('on GitHub')
    expect(recentWords(ended({ kind: 'notPushed' }))).toBe('not pushed')
    expect(recentWords(ended({ kind: 'failed' }))).toBe('failed')
    expect(recentWords(ended({ kind: 'unverified' }))).toBe('not checked on GitHub')
    expect(recentWords(releaseRun({ status: 'succeeded', result: { ...releaseRun().result, outcome: null } }))).toBe(
      'done',
    )
    expect(recentWords(releaseRun({ status: 'stopped', result: { ...releaseRun().result, outcome: null } }))).toBe(
      'stopped',
    )
  })

  it('lists a release’s steps, the one under way next after the last reached', () => {
    const states = (p: ScriptRunProgress, live = true) => releaseSteps(p, intent, live).map((s) => s.state)

    expect(states(progress(''))).toEqual(['working', 'pending', 'pending', 'pending', 'pending'])
    expect(states(progress('onBranch', 'updated'))).toEqual(['done', 'working', 'pending', 'pending', 'pending'])
    expect(states(progress('tagCreated'))).toEqual(['done', 'done', 'done', 'working', 'pending'])
    expect(states(progress('tagPushed'))).toEqual(['done', 'done', 'done', 'done', 'working'])
    expect(states(progress('updated'), false)).toEqual(['done', 'done', 'pending', 'pending', 'pending'])
  })

  it('words each step for the release’s branch and version', () => {
    const texts = releaseSteps(progress('tagPushed'), intent, true).map((s) => s.text)

    expect(texts).toEqual([
      'Switched to master',
      'Pulled master from origin',
      'Created tag v1.4.3',
      'Pushed v1.4.3 to origin',
      'Checking that GitHub has it',
    ])
    expect(releaseSteps(progress(''), intent, true)[0]?.text).toBe('Switching to master')
    expect(releaseSteps(progress('onBranch'), { ...intent, original: 'master' }, true)[0]?.text).toBe('On master')
    expect(releaseSteps(progress(''), { ...intent, original: 'master' }, true)[0]?.text).toBe('Checking out master')
    expect(releaseSteps(progress('verified'), intent, false)[4]?.text).toBe('GitHub has it')
  })

  it('words a step plainly when the release kept no intent', () => {
    const texts = releaseSteps(progress('tagCreated'), { version: '', branch: '', original: '' }, false).map(
      (s) => s.text,
    )

    expect(texts.slice(0, 3)).toEqual([
      'Switched to the release branch',
      'Pulled the release branch from origin',
      'Created tag the tag',
    ])
  })

  it('heads a result with what happened', () => {
    expect(resultHeading(releaseResult(), 'acme-org/sdk')).toBe('acme-org/sdk v1.4.3 is on GitHub')
    expect(resultHeading(releaseResult({ kind: 'notPushed' }), 'acme-org/sdk')).toBe(
      'v1.4.3 is tagged on this Mac but isn’t on GitHub',
    )
    expect(
      resultHeading(
        releaseResult({
          kind: 'failed',
          tag: '',
          failure: { kind: 'dirty', heading: 'The clone has uncommitted changes.', quote: '', bug: false },
        }),
        'acme-org/sdk',
      ),
    ).toBe('The clone has uncommitted changes.')
    expect(resultHeading(releaseResult({ kind: 'failed', tag: '' }), 'acme-org/sdk')).toBe(
      'The release of v1.4.3 failed',
    )
    expect(resultHeading(releaseResult({ kind: 'unverified' }), 'acme-org/sdk')).toBe(
      'Couldn’t check whether v1.4.3 is on GitHub',
    )
  })
})
