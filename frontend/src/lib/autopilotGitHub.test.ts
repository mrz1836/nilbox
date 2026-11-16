import { describe, expect, it } from 'vitest'

import { githubMatch, githubRun, githubStep } from '../test/fixtures'
import { actionLabels, chipOf, githubAheadText, githubGoLabel } from './autopilot'
import { sendsToGitHub } from './routineBuilder'

describe('a routine’s GitHub actions, as the window words them', () => {
  it('says on the chip the last action GitHub took', () => {
    const done = (action: string) =>
      chipOf(
        githubMatch({
          state: 'done',
          blockedBy: '',
          reason: 'ran',
          steps: [githubStep({ status: 'done', sent: 1 })],
          runs: [githubRun({ action })],
        }),
      )

    expect(done('rebase')).toEqual({ label: 'Asked rebase', tone: 'accent' })
    expect(done('updateBranch')).toEqual({ label: 'Updated branch', tone: 'accent' })
    expect(done('reply')).toEqual({ label: 'Commented', tone: 'accent' })
    expect(done('labels')).toEqual({ label: 'Labeled', tone: 'accent' })

    const notSent = chipOf(
      githubMatch({
        state: 'done',
        blockedBy: '',
        reason: 'ran',
        steps: [githubStep({ status: 'done' })],
        runs: [githubRun({ outcome: 'not_sent' })],
      }),
    )

    expect(notSent.label, 'an action that sent nothing is no chip of its own').toBe('Ran')

    const marked = chipOf(
      githubMatch({
        state: 'done',
        blockedBy: '',
        reason: 'ran',
        steps: [
          githubStep({ kind: 'markDone', label: 'Mark the GitHub notification done', status: 'done', actions: [] }),
        ],
      }),
    )

    expect(marked).toEqual({ label: 'Marked done', tone: 'accent' })
    expect(chipOf(githubMatch())).toEqual({ label: 'Needs you', tone: 'warning' })
  })

  it('names what a waiting GitHub step would send next, and what lets it go', () => {
    const two = githubMatch({ steps: [githubStep({ actions: ['reply', 'labels'], sent: 1 })] })

    expect(githubAheadText(two), 'the actions already sent are left out').toBe(actionLabels.labels)
    expect(githubAheadText(githubMatch({ steps: [githubStep({ actions: ['reply', 'labels'] })] }))).toBe(
      'Comment one of my replies, then Add or remove labels',
    )

    expect(githubGoLabel(githubMatch({ blockedBy: 'warning' }))).toBe('Send anyway')
    expect(githubGoLabel(githubMatch({ blockedBy: 'backlog' }))).toBe('Run on them')
    expect(githubGoLabel(githubMatch({ blockedBy: 'outside_contributor' }))).toBe('Send it')
    expect(githubGoLabel(githubMatch({ blockedBy: 'interrupted' }))).toBe('Send it again')
    expect(githubGoLabel(githubMatch({ blockedBy: 'ask' }))).toBe('Send it')
    expect(githubGoLabel(githubMatch({ state: 'queued', blockedBy: 'writes_held' })), 'it goes by itself').toBe('')
    expect(
      githubGoLabel(githubMatch({ steps: [githubStep({ kind: 'agent', actions: [] })], blockedBy: 'ask' })),
      'an agent’s OK is its own card',
    ).toBe('')
  })

  it('knows a routine that sends GitHub anything, for Test’s Preview', () => {
    expect(sendsToGitHub({ steps: [{ github: [{ rebase: {} }] }] })).toBe(true)
    expect(sendsToGitHub({ steps: [{ markDone: {} }] })).toBe(true)
    expect(sendsToGitHub({ steps: [{ notify: {} }, { pin: {} }] })).toBe(false)
  })
})
