import { describe, expect, it, vi } from 'vitest'

import type { ReleaseCandidate, ReleaseCloneState, ReleaseFollow, ScriptRunProgress } from '../platform'
import { releaseCandidate, releaseFollow, releaseResult, releaseRun, scriptRun } from '../test/fixtures'
import {
  bumpHint,
  bumpKind,
  cloneNotes,
  commitChecksText,
  followLine,
  followState,
  intentOf,
  isBumpResult,
  listProgressWords,
  matchCandidates,
  outcomeKind,
  readiness,
  readinessCounts,
  readinessOrder,
  readinessWords,
  recentReleases,
  recentWords,
  releasable,
  releaseSteps,
  resultHeading,
  sharedHeadlines,
  shownNotes,
  sortCandidates,
  unreleasedCount,
  waveAt,
  type ReleaseIntent,
} from './release'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

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
    expect(releasable('muted')).toBe(false)
  })

  it('searches the repositories by owner and name, trimmed and in any case', () => {
    const repos = [releaseCandidate(), releaseCandidate({ repo: 'acme-org/web' })]

    expect(matchCandidates(repos, '  SDK ').map((r) => r.repo)).toEqual(['acme-org/sdk'])
    expect(matchCandidates(repos, '')).toHaveLength(2)
  })

  it('says how far a read of the list has got, phase by phase', () => {
    expect(listProgressWords({ phase: 'clones', done: 0, total: 0 })).toBe('Finding your clones…')
    expect(listProgressWords({ phase: 'github', done: 45, total: 151 })).toBe('Asking GitHub: 45 of 151 repositories…')
    expect(listProgressWords({ phase: 'github', done: 0, total: 1 })).toBe('Asking GitHub: 0 of 1 repository…')
    expect(listProgressWords({ phase: 'compare', done: 15, total: 1200 })).toBe(
      'Asking GitHub what changed: 15 of 1,200 repositories…',
    )
    expect(listProgressWords({ phase: 'checking', done: 3, total: 12 })).toBe('Checking your clones: 3 of 12…')
  })

  it('knows magex’s JSON result line, and nothing else that looks like it', () => {
    const result = {
      from: 'v1.4.3',
      to: 'v1.4.4',
      bump: 'patch',
      expect: '',
      dryRun: false,
      tag: 'v1.4.4',
      commit: 'a1b2c3',
      pushed: true,
      startBranch: 'master',
      branch: 'master',
      warnings: [],
      error: '',
    }

    expect(isBumpResult(JSON.stringify(result))).toBe(true)
    expect(isBumpResult(`  ${JSON.stringify(result)}  `)).toBe(true)
    expect(isBumpResult(JSON.stringify({ ...result, error: undefined }))).toBe(false)
    expect(isBumpResult('{"from":"v1.4.3"}')).toBe(false)
    expect(isBumpResult('{ not json }')).toBe(false)
    expect(isBumpResult('[]')).toBe(false)
    expect(isBumpResult('21:59:47 [INFO] Using bump type: patch')).toBe(false)
  })

  it('counts the unreleased repositories, none before the list', () => {
    expect(unreleasedCount([releaseCandidate(), releaseCandidate({ group: 'upToDate' })])).toBe(1)
    expect(unreleasedCount(undefined)).toBe(0)
  })

  it('says what matters about a clone, and nothing about one on its default branch', () => {
    expect(cloneNotes(null, 'master')).toEqual([])
    expect(cloneNotes(clone(), 'master')).toEqual([])
    expect(cloneNotes(clone({ branch: '' }), 'master')).toEqual([])
    expect(
      cloneNotes(clone({ branch: 'feature-x', dirty: 2, notPushed: 3, behind: true, localOnly: ['v1.4.3'] }), 'master'),
    ).toEqual([
      {
        kind: 'branch',
        text: 'feature-x',
        blocks: false,
        title: 'You’re on feature-x. magex switches to master, then back.',
      },
      {
        kind: 'dirty',
        text: '2 uncommitted',
        blocks: true,
        title: '2 files have uncommitted changes. Commit or stash them first.',
      },
      { kind: 'notPushed', text: '3 not pushed', blocks: true, title: '3 commits aren’t on GitHub yet. Push first.' },
      {
        kind: 'behind',
        text: 'behind',
        blocks: false,
        title: 'origin/master has commits your clone doesn’t. magex pulls them first.',
      },
      {
        kind: 'localOnly',
        text: 'v1.4.3 only on this Mac',
        blocks: true,
        title: 'The tag v1.4.3 is in this clone but not on GitHub. Push it or delete it first.',
      },
    ])
  })

  it('words one uncommitted file and one commit not pushed alone, and each version only the clone has', () => {
    expect(cloneNotes(clone({ dirty: 1, notPushed: 1, localOnly: ['v1.4.3', 'v1.5.0'] }), 'master')).toEqual([
      {
        kind: 'dirty',
        text: '1 uncommitted',
        blocks: true,
        title: '1 file has uncommitted changes. Commit or stash them first.',
      },
      { kind: 'notPushed', text: '1 not pushed', blocks: true, title: '1 commit isn’t on GitHub yet. Push first.' },
      {
        kind: 'localOnly',
        text: 'v1.4.3 only on this Mac',
        blocks: true,
        title: 'The tag v1.4.3 is in this clone but not on GitHub. Push it or delete it first.',
      },
      {
        kind: 'localOnly',
        text: 'v1.5.0 only on this Mac',
        blocks: true,
        title: 'The tag v1.5.0 is in this clone but not on GitHub. Push it or delete it first.',
      },
    ])
  })

  it('notes a detached commit over any branch, and a clone git couldn’t read alone', () => {
    const detached = {
      kind: 'detached',
      text: 'on a detached commit',
      blocks: true,
      title: 'Your clone is on a detached commit. Switch to a branch first.',
    }

    expect(cloneNotes(clone({ detached: true, branch: '' }), 'master')).toEqual([detached])
    expect(cloneNotes(clone({ detached: true, branch: 'feature-x' }), 'master')).toEqual([detached])
    expect(cloneNotes(clone({ unreadable: true, dirty: 3, behind: true, branch: 'feature-x' }), 'master')).toEqual([
      {
        kind: 'unreadable',
        text: 'couldn’t read the clone',
        blocks: true,
        title: 'git couldn’t read this clone, so Release can’t check it.',
      },
    ])
  })

  it('calls it the default branch when the default branch isn’t known', () => {
    expect(cloneNotes(clone({ behind: true }), '').map((n) => n.title)).toEqual([
      'You’re on master. magex switches to the default branch, then back.',
      'origin/the default branch has commits your clone doesn’t. magex pulls them first.',
    ])
  })

  it('shows only the notes that stop a release, else the commits the clone lacks before its branch', () => {
    const kinds = (change: Partial<ReleaseCloneState>) =>
      shownNotes(cloneNotes(clone(change), 'master')).map((n) => n.kind)

    expect(kinds({ branch: 'feature-x', dirty: 2, behind: true, localOnly: ['v1.4.3'] })).toEqual([
      'dirty',
      'localOnly',
    ])
    expect(kinds({ branch: 'feature-x', behind: true })).toEqual(['behind', 'branch'])
    expect(kinds({ branch: 'feature-x' })).toEqual(['branch'])
    expect(kinds({ behind: true })).toEqual(['behind'])
    expect(shownNotes([])).toEqual([])
  })

  it('reads how ready a repository is, the first that applies', () => {
    const blocked = clone({ dirty: 1 })

    expect(readiness(releaseCandidate({ checks: 'FAILURE' }))).toBe('failing')
    expect(readiness(releaseCandidate({ checks: 'ERROR' }))).toBe('failing')
    expect(readiness(releaseCandidate({ checks: 'FAILURE', clone: blocked }))).toBe('failing')
    expect(readiness(releaseCandidate({ checks: 'ERROR', clone: clone({ unreadable: true }) }))).toBe('failing')
    expect(readiness(releaseCandidate({ checks: 'PENDING', clone: blocked }))).toBe('blocked')
    expect(readiness(releaseCandidate({ checks: 'SUCCESS', clone: blocked }))).toBe('blocked')
    expect(readiness(releaseCandidate({ checks: '', clone: clone({ unreadable: true }) }))).toBe('blocked')
    expect(readiness(releaseCandidate({ checks: 'PENDING' }))).toBe('running')
    expect(readiness(releaseCandidate({ checks: 'EXPECTED' }))).toBe('running')
    expect(readiness(releaseCandidate({ checks: 'SUCCESS' }))).toBe('ready')
    expect(readiness(releaseCandidate({ checks: 'SUCCESS', clone: null }))).toBe('ready')
    expect(readiness(releaseCandidate({ checks: '' }))).toBe('none')
  })

  it('lets a clone behind origin or on another branch be ready', () => {
    expect(readiness(releaseCandidate({ clone: clone({ branch: 'feature-x', behind: true }) }))).toBe('ready')
  })

  it('words and orders the readinesses, what needs a look first', () => {
    expect(readinessOrder.map((r) => readinessWords[r])).toEqual([
      'failing',
      'blocked',
      'running',
      'ready',
      'without checks',
    ])
  })

  it('counts the rows of each readiness in order, leaving out a readiness no row has', () => {
    const rows = [
      releaseCandidate({ checks: '' }),
      releaseCandidate({ checks: 'SUCCESS' }),
      releaseCandidate({ checks: 'SUCCESS', clone: clone({ dirty: 2 }) }),
      releaseCandidate({ checks: 'FAILURE' }),
      releaseCandidate({ checks: 'PENDING', clone: clone({ notPushed: 1 }) }),
      releaseCandidate({ checks: 'SUCCESS' }),
    ]

    expect(readinessCounts(rows)).toEqual([
      { readiness: 'failing', count: 1 },
      { readiness: 'blocked', count: 2 },
      { readiness: 'ready', count: 2 },
      { readiness: 'none', count: 1 },
    ])
    expect(readinessCounts([])).toEqual([])
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
    expect(resultHeading(releaseResult({ follow: releaseFollow() }), 'acme-org/sdk')).toBe(
      'acme-org/sdk v1.4.3 is on GitHub',
    )
    expect(resultHeading(releaseResult({ follow: releaseFollow({ state: 'published' }) }), 'acme-org/sdk')).toBe(
      'acme-org/sdk v1.4.3 is published',
    )
  })

  it('reads how following a release’s workflow goes from its run', () => {
    const followed = (follow: unknown) =>
      releaseRun({ result: { ...releaseRun().result, outcome: { kind: 'released', follow } } })

    expect(followState(releaseRun())).toBe('')
    expect(followState(followed(null))).toBe('')
    expect(followState(followed({ state: 'other' }))).toBe('')
    expect(followState(scriptRun())).toBe('')

    for (const state of ['following', 'published', 'failed', 'noWorkflow', 'unpublished', 'timedOut'] as const) {
      expect(followState(followed({ state }))).toBe(state)
    }

    expect(recentWords(followed({ state: 'following' }))).toBe('on GitHub, following its workflow')
    expect(recentWords(followed({ state: 'published' }))).toBe('published')
    expect(recentWords(followed({ state: 'failed' }))).toBe('release workflow failed')
    expect(recentWords(followed({ state: 'noWorkflow' }))).toBe('on GitHub')
    expect(recentWords(followed({ state: 'unpublished' }))).toBe('on GitHub, not published')
    expect(recentWords(followed({ state: 'timedOut' }))).toBe('on GitHub')
    expect(
      recentWords(
        releaseRun({ result: { ...releaseRun().result, outcome: { kind: 'failed', follow: { state: 'failed' } } } }),
      ),
    ).toBe('failed')
  })

  it('words the workflow line of a followed release', () => {
    const line = (follow: Partial<ReleaseFollow> | null) =>
      followLine(releaseResult({ follow: follow === null ? null : releaseFollow(follow) }), 'acme-org/sdk')

    expect(line(null)).toBeNull()
    expect(line({})).toEqual({
      tone: 'info',
      text: 'Following the release workflow for acme-org/sdk v1.4.3…',
      url: '',
      label: '',
    })
    expect(line({ state: 'published', url: 'https://github.com/acme-org/sdk/releases/tag/v1.4.3' })).toEqual({
      tone: 'ok',
      text: 'The release workflow published acme-org/sdk v1.4.3',
      url: 'https://github.com/acme-org/sdk/releases/tag/v1.4.3',
      label: 'View the release',
    })
    expect(line({ state: 'failed', url: 'https://github.com/acme-org/sdk/actions/runs/7' })).toEqual({
      tone: 'bad',
      text: 'The release workflow failed for acme-org/sdk v1.4.3',
      url: 'https://github.com/acme-org/sdk/actions/runs/7',
      label: 'View the failed workflow',
    })
    expect(
      line({ state: 'failed', workflow: 'goreleaser', url: 'https://github.com/acme-org/sdk/actions/runs/7' }),
    ).toEqual({
      tone: 'bad',
      text: 'The goreleaser workflow failed for acme-org/sdk v1.4.3',
      url: 'https://github.com/acme-org/sdk/actions/runs/7',
      label: 'View the failed workflow',
    })
    expect(line({ state: 'noWorkflow', url: 'https://github.com/acme-org/sdk/actions' })).toEqual({
      tone: 'warn',
      text: 'No workflow ran for the v1.4.3 tag, so there’s nothing to follow',
      url: 'https://github.com/acme-org/sdk/actions',
      label: 'View the tag’s workflow runs',
    })
    expect(line({ state: 'unpublished', url: 'https://github.com/acme-org/sdk/releases' })).toEqual({
      tone: 'warn',
      text: 'The tag’s workflows finished, but GitHub hasn’t published a release for v1.4.3',
      url: 'https://github.com/acme-org/sdk/releases',
      label: 'View the releases',
    })
    expect(line({ state: 'timedOut', minutes: 45, url: 'https://github.com/acme-org/sdk/actions' })).toEqual({
      tone: 'warn',
      text: 'Stopped following acme-org/sdk v1.4.3 after 45 minutes',
      url: 'https://github.com/acme-org/sdk/actions',
      label: 'View the release workflow',
    })
    expect(
      followLine(releaseResult({ tag: '', follow: releaseFollow({ state: 'timedOut', minutes: 1 }) }), 'acme-org/sdk')
        ?.text,
    ).toBe('Stopped following acme-org/sdk v1.4.3 after 1 minute')
  })
})

describe('bumpHint', () => {
  it('suggests nothing for commits without a feature or a breaking change', () => {
    expect(bumpHint([])).toBe('')
    expect(bumpHint(['fix: retry on 502', 'chore(deps): bump x', '[Scripts] Save your own scripts (#46)'])).toBe('')
    expect(bumpHint(['feature: not a prefix', 'feat without a colon'])).toBe('')
  })

  it('suggests a minor release for commits that start with feat:', () => {
    expect(bumpHint(['feat: add a flag', 'fix: typo'])).toBe(
      '1 commit starts with feat:, which usually means a minor release.',
    )
    expect(bumpHint(['feat(cli): add a flag', 'FEAT: shout', 'fix: typo'])).toBe(
      '2 commits start with feat:, which usually means a minor release.',
    )
  })

  it('suggests a major release for a commit marked breaking, over any feature', () => {
    expect(bumpHint(['feat!: drop the old flag', 'feat: add a flag'])).toBe(
      '1 commit is marked breaking (feat!:), which usually means a major release.',
    )
    expect(bumpHint(['fix(api)!: rename a field', 'refactor!: move things'])).toBe(
      '2 commits are marked breaking (such as fix!:), which usually means a major release.',
    )
    expect(bumpHint(['BREAKING CHANGE: the config moved'])).toBe(
      '1 commit is marked breaking (BREAKING CHANGE:), which usually means a major release.',
    )
  })
})

describe('bumpKind', () => {
  it('reads a major bump from a commit marked breaking, over any feature', () => {
    expect(bumpKind(['feat!: drop the old flag', 'feat: add a flag'])).toBe('major')
    expect(bumpKind(['fix: typo', 'fix(api)!: rename a field'])).toBe('major')
    expect(bumpKind(['BREAKING CHANGE: the config moved'])).toBe('major')
    expect(bumpKind(['  breaking-change: spelled loosely'])).toBe('major')
  })

  it('reads a minor bump from a commit that starts with feat:', () => {
    expect(bumpKind(['fix: typo', 'feat(cli): add a flag'])).toBe('minor')
    expect(bumpKind(['  FEAT: shout'])).toBe('minor')
  })

  it('reads nothing from commits without a feature or a breaking change', () => {
    expect(bumpKind([])).toBe('')
    expect(bumpKind(['fix: retry on 502', 'chore(deps): bump x'])).toBe('')
    expect(bumpKind(['feature: not a prefix', 'feat without a colon'])).toBe('')
  })
})

describe('sortCandidates', () => {
  // row is a candidate named repo, its newest commit at headAt, with ahead commits since
  // its version.
  function row(repo: string, headAt: string, ahead = 0, dir = ''): ReleaseCandidate {
    return releaseCandidate({ repo: `acme-org/${repo}`, headAt, ahead, dir })
  }

  const names = (rows: readonly ReleaseCandidate[]) => rows.map((r) => r.repo.slice('acme-org/'.length))
  const nothingPinned = () => false

  it('lists the newest commit first, one without a time last, and ties by name', () => {
    const rows = [
      row('muted', ''),
      row('web', '2026-06-12T09:00:00Z'),
      row('sdk', '2026-06-15T11:48:00Z'),
      row('api', '2026-06-12T09:00:00Z'),
      row('archive', ''),
    ]

    expect(names(sortCandidates(rows, 'newest', nothingPinned))).toEqual(['sdk', 'api', 'web', 'archive', 'muted'])
  })

  it('lists by name in any case, as Go compares the lowered names', () => {
    const rows = [row('Web', ''), row('sdk_go', ''), row('API', ''), row('SDK-go', ''), row('sdk', '')]

    // Byte order puts a name before a longer one it starts, and "-" before "_", which a
    // locale's collation needn't.
    expect(names(sortCandidates(rows, 'name', nothingPinned))).toEqual(['API', 'sdk', 'SDK-go', 'sdk_go', 'Web'])
  })

  it('lists the most commits first, then the newest, then by name', () => {
    const rows = [
      row('tools', '2026-06-15T11:48:00Z', 2),
      row('web', '2026-06-12T09:00:00Z', 9),
      row('sdk', '2026-06-12T09:00:00Z', 2),
      row('api', '2026-06-12T09:00:00Z', 2),
      row('muted', '', 2),
      row('old', '2026-06-01T09:00:00Z', 0),
    ]

    expect(names(sortCandidates(rows, 'commits', nothingPinned))).toEqual([
      'web',
      'tools',
      'api',
      'sdk',
      'muted',
      'old',
    ])
  })

  it('keeps the pinned repositories first in every sort', () => {
    const rows = [
      row('api', '2026-06-01T09:00:00Z', 1),
      row('web', '2026-06-15T11:48:00Z', 9),
      row('zeta', '2026-06-10T09:00:00Z', 3),
      row('sdk', '2026-06-12T09:00:00Z', 5),
    ]
    const pinned = (repo: string) => repo === 'acme-org/zeta' || repo === 'acme-org/api'

    expect(names(sortCandidates(rows, 'newest', pinned))).toEqual(['zeta', 'api', 'web', 'sdk'])
    expect(names(sortCandidates(rows, 'name', pinned))).toEqual(['api', 'zeta', 'sdk', 'web'])
    expect(names(sortCandidates(rows, 'commits', pinned))).toEqual(['zeta', 'api', 'web', 'sdk'])
  })

  it('keeps the order of candidates it can’t tell apart, in a new array', () => {
    const rows = [
      row('sdk', '2026-06-12T09:00:00Z', 2, '~/projects/first'),
      row('SDK', '2026-06-12T09:00:00Z', 2, '~/projects/second'),
      row('Sdk', '2026-06-12T09:00:00Z', 2, '~/projects/third'),
    ]
    const before = [...rows]

    for (const sort of ['newest', 'name', 'commits'] as const) {
      const sorted = sortCandidates(rows, sort, nothingPinned)

      expect(sorted.map((r) => r.dir)).toEqual(['~/projects/first', '~/projects/second', '~/projects/third'])
      expect(sorted).not.toBe(rows)
    }

    expect(sortCandidates([...rows].reverse(), 'name', nothingPinned).map((r) => r.dir)).toEqual([
      '~/projects/third',
      '~/projects/second',
      '~/projects/first',
    ])
    expect(rows).toEqual(before)
  })

  it('leaves the list it was given as it was', () => {
    const rows = [row('web', '', 1), row('api', '', 3)]

    sortCandidates(rows, 'name', nothingPinned)
    sortCandidates(rows, 'commits', () => true)

    expect(names(rows)).toEqual(['web', 'api'])
  })
})

describe('sharedHeadlines', () => {
  it('maps each headline to the repositories whose newest commit it is, in their order', () => {
    const wave = 'sync(ci): add RELEASE_ENABLED toggle'
    const rows = [
      { repo: 'acme-org/web', headline: wave },
      { repo: 'acme-org/sdk', headline: 'Fix retries on 502 (#45)' },
      { repo: 'acme-org/muted', headline: '' },
      { repo: 'acme-org/api', headline: wave },
      { repo: 'acme-org/tools', headline: wave },
    ]
    const shared = sharedHeadlines(rows)

    expect(shared).toEqual(
      new Map([
        [wave, ['acme-org/web', 'acme-org/api', 'acme-org/tools']],
        ['Fix retries on 502 (#45)', ['acme-org/sdk']],
      ]),
    )
    expect([...shared].filter(([, repos]) => repos.length >= waveAt).map(([headline]) => headline)).toEqual([wave])
  })

  it('counts a repository once however many of its clones are listed', () => {
    const wave = 'sync(ci): add RELEASE_ENABLED toggle'
    const shared = sharedHeadlines([
      { repo: 'acme-org/sdk', headline: wave },
      { repo: 'acme-org/web', headline: wave },
      { repo: 'acme-org/sdk', headline: wave },
    ])

    expect(shared).toEqual(new Map([[wave, ['acme-org/sdk', 'acme-org/web']]]))
  })

  it('leaves out the rows without a headline', () => {
    expect(sharedHeadlines([{ repo: 'acme-org/muted', headline: '' }])).toEqual(new Map())
    expect(sharedHeadlines([])).toEqual(new Map())
  })
})
