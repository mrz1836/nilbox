import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import type { UpdateLocation } from '../platform'
import { runGoing, scriptRun, updateReady, updateStatus } from '../test/fixtures'
import { emptyIssueDraft } from './issueDrafts'
import { emptyDraft } from './reviewDrafts'
import {
  aboutLine,
  bannerOf,
  finishingWords,
  locationNote,
  locationWords,
  namesRuns,
  offerOf,
  questionWords,
  releasePageOf,
  runsGoingIn,
  runText,
  sentence,
  unsentSheets,
  type RestartStep,
} from './update'

const idle: RestartStep = { step: 'idle' }

describe('bannerOf', () => {
  it('shows nothing before the status loads, for a build that doesn’t update, or while up to date', () => {
    expect(bannerOf(null, idle, '')).toEqual({ kind: 'none' })
    expect(bannerOf(updateReady({ enabled: false, state: 'off' }), idle, '')).toEqual({ kind: 'none' })
    expect(bannerOf(updateStatus(), idle, '')).toEqual({ kind: 'none' })
    expect(bannerOf(updateStatus({ state: 'checking', checking: true }), idle, '')).toEqual({ kind: 'none' })
  })

  it('offers a version ready to restart into, or available to update to', () => {
    expect(bannerOf(updateReady(), idle, '')).toEqual({ kind: 'ready', version: '0.3.0' })
    expect(bannerOf(updateReady({ state: 'ready', checking: true }), idle, '')).toEqual({
      kind: 'ready',
      version: '0.3.0',
    })
    expect(bannerOf(updateReady({ state: 'available', mode: 'notify' }), idle, '')).toEqual({
      kind: 'available',
      version: '0.3.0',
    })
    // Don't check: a check the user ran found it.
    expect(bannerOf(updateReady({ state: 'available', mode: 'off' }), idle, '')).toEqual({
      kind: 'available',
      version: '0.3.0',
    })
  })

  it('stays quiet while Download it and tell me fetches what it found, unless the download failed', () => {
    expect(bannerOf(updateReady({ state: 'available' }), idle, '')).toEqual({ kind: 'none' })
    expect(bannerOf(updateReady({ state: 'downloading', progress: 40 }), idle, '')).toEqual({ kind: 'none' })
    expect(
      bannerOf(updateReady({ state: 'available', lastError: 'Couldn’t download 0.3.0: it stalled.' }), idle, ''),
    ).toEqual({ kind: 'available', version: '0.3.0' })
  })

  it('hides what Later hid, and shows nothing for a version it can’t name', () => {
    expect(bannerOf(updateReady({ bannerHidden: true }), idle, '')).toEqual({ kind: 'none' })
    expect(bannerOf(updateReady({ latest: '' }), idle, '')).toEqual({ kind: 'none' })
  })

  it('says why an update waiting can’t install where the app runs', () => {
    const location: UpdateLocation = { ok: false, kind: 'elsewhere', target: '/Applications/Example.app' }

    // Nothing downloads there, so a version found stays available, whatever the setting.
    for (const mode of ['download', 'notify', 'off'] as const) {
      expect(bannerOf(updateReady({ location, state: 'available', mode }), idle, '')).toEqual({
        kind: 'location',
        version: '0.3.0',
        ready: false,
      })
    }
    // One kept from before it moved there is ready.
    expect(bannerOf(updateReady({ location }), idle, '')).toEqual({ kind: 'location', version: '0.3.0', ready: true })
    expect(bannerOf(updateReady({ location, bannerHidden: true }), idle, '')).toEqual({ kind: 'none' })
  })

  it('waits quietly while the last update finishes, over everything', () => {
    expect(
      bannerOf(
        updateReady({ bannerHidden: true }),
        { step: 'waiting', message: 'The last update is still finishing. Try again in a moment.' },
        '',
      ),
    ).toEqual({ kind: 'waiting', message: 'The last update is still finishing. Try again in a moment.' })
  })

  it('says the update is finishing while the app quits on its own after a held quit, over everything', () => {
    const finishing = {
      kind: 'working',
      text: 'Finishing the update…',
      progress: null,
      detail: `${brand.name} will reopen in a moment.`,
    }

    expect(bannerOf(updateReady({ state: 'finishing' }), idle, '')).toEqual(finishing)
    expect(bannerOf(updateReady({ state: 'finishing', bannerHidden: true }), idle, '')).toEqual(finishing)
    expect(bannerOf(updateReady({ state: 'finishing' }), { step: 'restarting' }, '')).toEqual(finishing)
    expect(bannerOf(updateReady({ state: 'finishing' }), { step: 'waiting', message: finishingWords }, '')).toEqual(
      finishing,
    )
    // Before the status says so, the restart's refusal says the same, quietly.
    expect(bannerOf(updateReady(), { step: 'waiting', message: finishingWords }, '')).toEqual({
      kind: 'waiting',
      message: `Finishing the update… ${brand.name} will reopen in a moment.`,
    })
  })

  it('says the last launch’s update didn’t install over the same version, ready again', () => {
    // A rollback leaves the version that failed downloaded and ready: the failure wins, in
    // every mode, Later or not, since its Try again restarts into it.
    const failed = updateReady({
      installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true },
    })

    expect(bannerOf(failed, idle, '')).toEqual({
      kind: 'lastLaunch',
      version: '0.3.0',
      current: '0.2.0',
      newer: null,
    })
    expect(bannerOf({ ...failed, mode: 'notify' }, idle, '')).toEqual({
      kind: 'lastLaunch',
      version: '0.3.0',
      current: '0.2.0',
      newer: null,
    })
    expect(bannerOf({ ...failed, bannerHidden: true }, idle, '').kind).toBe('lastLaunch')
  })

  it('names a newer version found since the one that didn’t install, which its button installs', () => {
    const failure = { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true }

    expect(bannerOf(updateReady({ latest: '0.3.1', installFailed: failure }), idle, '')).toEqual({
      kind: 'lastLaunch',
      version: '0.3.0',
      current: '0.2.0',
      newer: { version: '0.3.1', ready: true },
    })
    expect(
      bannerOf(updateReady({ state: 'available', mode: 'notify', latest: '0.3.1', installFailed: failure }), idle, ''),
    ).toMatchObject({ kind: 'lastLaunch', newer: { version: '0.3.1', ready: false } })
    // One downloading is offered nothing yet, so the banner names nothing either.
    expect(
      bannerOf(updateReady({ state: 'downloading', latest: '0.3.1', installFailed: failure }), idle, ''),
    ).toMatchObject({ kind: 'lastLaunch', newer: null })
    expect(bannerOf(updateStatus({ installFailed: failure }), idle, '')).toMatchObject({
      kind: 'lastLaunch',
      newer: null,
    })
  })

  it('never offers the version that just failed as plainly ready once its failure is put away', () => {
    const failed = updateReady({
      installFailed: { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true },
    })

    expect(bannerOf(failed, idle, '0.3.0')).toEqual({ kind: 'none' })
    expect(bannerOf({ ...failed, state: 'idle', latest: '' }, idle, '0.3.0')).toEqual({ kind: 'none' })
    // A newer version than the one that failed is offered as any other.
    expect(bannerOf({ ...failed, latest: '0.3.1' }, idle, '0.3.0')).toEqual({ kind: 'ready', version: '0.3.1' })
  })

  it('shows the restart under way from the live status: the download’s progress, then the restart', () => {
    expect(bannerOf(updateReady({ state: 'downloading', progress: 42 }), { step: 'downloading' }, '')).toEqual({
      kind: 'working',
      text: 'Downloading 0.3.0…',
      progress: 42,
      detail: '',
    })
    // The download asked for, about to begin.
    expect(bannerOf(updateReady({ state: 'available' }), { step: 'downloading' }, '')).toEqual({
      kind: 'working',
      text: 'Downloading 0.3.0…',
      progress: 0,
      detail: '',
    })
    // Restart waits for a newer version that a check found to download, and names it.
    expect(
      bannerOf(updateReady({ state: 'downloading', latest: '0.3.1', progress: 7 }), { step: 'restarting' }, ''),
    ).toEqual({ kind: 'working', text: 'Downloading 0.3.1…', progress: 7, detail: '' })
    expect(bannerOf(updateReady(), { step: 'restarting' }, '')).toEqual({
      kind: 'working',
      text: 'Restarting into 0.3.0…',
      progress: null,
      detail: '',
    })
    expect(bannerOf(updateReady({ state: 'installing' }), { step: 'restarting' }, '')).toEqual({
      kind: 'working',
      text: 'Restarting into 0.3.0…',
      progress: null,
      detail: 'Backing up your data and preparing the update…',
    })
    expect(bannerOf(null, { step: 'restarting' }, '')).toEqual({
      kind: 'working',
      text: 'Restarting into the new version…',
      progress: null,
      detail: '',
    })
    expect(bannerOf(updateStatus(), { step: 'checking' }, '')).toEqual({
      kind: 'working',
      text: 'Checking for updates…',
      progress: null,
      detail: '',
    })
  })

  it('shows a restart under way that the window didn’t start, Later or not', () => {
    expect(bannerOf(updateReady({ state: 'installing', bannerHidden: true }), idle, '')).toEqual({
      kind: 'working',
      text: 'Restarting into 0.3.0…',
      progress: null,
      detail: 'Backing up your data and preparing the update…',
    })
  })

  it('keeps what it shows while the restart gets ready or asks first', () => {
    expect(bannerOf(updateReady(), { step: 'preparing' }, '')).toEqual({ kind: 'ready', version: '0.3.0' })
    expect(bannerOf(updateReady(), { step: 'asking', question: { runs: null, sheets: 1 } }, '')).toEqual({
      kind: 'ready',
      version: '0.3.0',
    })
  })

  it('shows a restart that failed, with the backend’s words as a sentence, over everything', () => {
    expect(
      bannerOf(
        updateReady({ bannerHidden: true }),
        { step: 'failed', message: 'Couldn’t install 0.3.0: no space' },
        '',
      ),
    ).toEqual({ kind: 'failed', message: 'Couldn’t install 0.3.0: no space.' })
  })
})

describe('sentence', () => {
  it('ends words with a full stop once', () => {
    expect(sentence('No space')).toBe('No space.')
    expect(sentence(' Done. ')).toBe('Done.')
    expect(sentence('Really?')).toBe('Really?')
    expect(sentence('Wait…')).toBe('Wait…')
    expect(sentence('')).toBe('')
  })
})

describe('aboutLine', () => {
  it('words a build that doesn’t update by why', () => {
    const off = updateStatus({ enabled: false, off: 'Updates are off in builds made from source.', state: 'off' })

    expect(aboutLine(off)).toEqual({
      text: 'Updates are off in builds made from source.',
      progress: null,
      checkedAt: '',
      note: '',
    })
  })

  it('words each state', () => {
    expect(aboutLine(updateStatus())).toEqual({
      text: 'Up to date',
      progress: null,
      checkedAt: '2026-06-15T11:48:00Z',
      note: '',
    })
    expect(aboutLine(updateStatus({ lastChecked: '' }))).toEqual({
      text: 'Not checked yet',
      progress: null,
      checkedAt: '',
      note: '',
    })
    expect(aboutLine(updateStatus({ state: 'checking', checking: true })).text).toBe('Checking…')
    expect(aboutLine(updateStatus({ checking: true })).text).toBe('Checking…')
    // The percentage stands apart from the words, which the row reads out as they change.
    expect(aboutLine(updateReady({ state: 'downloading', progress: 42 }))).toEqual({
      text: 'Downloading 0.3.0…',
      progress: 42,
      checkedAt: '',
      note: '',
    })
    expect(aboutLine(updateReady()).text).toBe('0.3.0 is ready')
    expect(aboutLine(updateReady({ state: 'available' })).text).toBe('0.3.0 is available')
    expect(aboutLine(updateReady({ state: 'installing' })).text).toBe('Restarting into 0.3.0…')
    expect(aboutLine(updateReady({ state: 'finishing' })).text).toBe('Finishing the update…')
  })

  it('says the last check failed, and keeps a failure under a version found', () => {
    expect(aboutLine(updateStatus({ lastError: 'Couldn’t reach the releases: offline' }))).toEqual({
      text: 'Last check failed: Couldn’t reach the releases: offline.',
      progress: null,
      checkedAt: '',
      note: '',
    })
    expect(aboutLine(updateReady({ lastError: 'Couldn’t install 0.3.0: no space' }))).toEqual({
      text: '0.3.0 is ready',
      progress: null,
      checkedAt: '',
      note: 'Couldn’t install 0.3.0: no space.',
    })
  })
})

describe('locationWords and locationNote', () => {
  it('ask to move the app into the Applications folder it would go in', () => {
    const location: UpdateLocation = { ok: false, kind: 'translocated', target: '~/Applications/Example.app' }

    expect(locationWords(location)).toEqual({
      title: `Move ${brand.name} to Applications to update it`,
      hint: `${brand.name} can only update itself from your Applications folder. Move it to ~/Applications, then open it from there.`,
    })
    expect(locationWords({ ...location, target: '' }).hint).toContain('Move it to your Applications folder,')
    expect(locationNote(location)).toBe(`Move ${brand.name} to your Applications folder to install it.`)
  })

  it('say an Applications folder the account can’t change is why', () => {
    for (const kind of ['applications', 'userApplications'] as const) {
      const location: UpdateLocation = { ok: false, kind, target: '/Applications/Example.app' }

      expect(locationWords(location).title).toBe(`${brand.name} can’t update itself here`)
      expect(locationNote(location)).toBe('Your account can’t change the folder it’s in, so it can’t install it.')
    }
  })
})

describe('questionWords', () => {
  it('names one run and where it runs, with what stopping a release can leave', () => {
    expect(questionWords({ runs: [runGoing()], sheets: 0 })).toEqual({
      text: 'Restarting stops Release v1.2.0 in acme-org/sdk.',
      list: [],
      notes: ['Stopping it now can leave a tag on this Mac but not on GitHub.'],
    })
  })

  it('names the text not sent, in one sheet or several', () => {
    expect(questionWords({ runs: [], sheets: 1 })).toEqual({
      text: 'Restarting loses the text you haven’t sent in a sheet.',
      list: [],
      notes: [],
    })
    expect(questionWords({ runs: [], sheets: 2 }).text).toBe('Restarting loses the text you haven’t sent in 2 sheets.')
  })

  it('names both in one sentence', () => {
    expect(questionWords({ runs: [runGoing()], sheets: 2 }).text).toBe(
      'Restarting stops Release v1.2.0 in acme-org/sdk and loses the text you haven’t sent in 2 sheets.',
    )
  })

  it('counts several runs and lists them, and says the runs as a whole when they couldn’t be listed', () => {
    const pull = runGoing({ id: 8, kind: 'gitPull', title: 'Pull main', repo: 'acme-org/api' })
    const words = questionWords({ runs: [runGoing(), pull], sheets: 0 })

    expect(words.text).toBe('Restarting stops 2 runs.')
    expect(words.list).toEqual([runGoing(), pull])
    expect(words.notes).toEqual(['Stopping a release now can leave a tag on this Mac but not on GitHub.'])
    expect(questionWords({ runs: null, sheets: 1 }).text).toBe(
      `Restarting stops the runs going in ${brand.name} and loses the text you haven’t sent in a sheet.`,
    )
  })

  it('says what waits for a run held back from stopping', () => {
    const pull = runGoing({ kind: 'gitPull', title: 'Pull main', stopRefused: 'git is writing the clone’s files' })

    expect(questionWords({ runs: [pull], sheets: 0 }).notes).toEqual([
      `git is writing the clone’s files. Restarting waits up to a few seconds for it. If it hasn’t ended by then, it goes on after ${brand.name} closes.`,
    ])
    expect(questionWords({ runs: [pull, runGoing({ id: 9, kind: 'checks' })], sheets: 0 }).notes).toEqual([
      `A run that can’t be stopped right now is waited for up to a few seconds. If it doesn’t finish, it goes on after ${brand.name} closes.`,
    ])
    expect(questionWords({ runs: [runGoing({ stopRefused: 'magex is pulling' })], sheets: 0 }).notes[0]).toMatch(
      /^magex is switching branches or pulling/,
    )
  })

  it('names a run with no repository by its folder, or by its title alone', () => {
    expect(runText(runGoing({ repo: '' }))).toBe('Release v1.2.0 in ~/projects/sdk')
    expect(runText(runGoing({ repo: '', dir: '' }))).toBe('Release v1.2.0')
  })

  it('says nothing when nothing would be lost', () => {
    expect(questionWords({ runs: [], sheets: 0 })).toEqual({ text: '', list: [], notes: [] })
  })
})

describe('runsGoingIn', () => {
  it('keeps the runs in the app that can stop, or are held back for now', () => {
    const going = scriptRun({ id: 1, kind: 'release', title: 'Release v1.2.0', canStop: true })
    const held = scriptRun({
      id: 2,
      kind: 'gitPull',
      title: 'Pull main',
      canStop: false,
      progress: { ...scriptRun().progress, stopRefused: 'git is writing' },
    })
    const ended = scriptRun({ id: 3, canStop: false })

    expect(runsGoingIn([going, held, ended])).toEqual([
      { id: 1, kind: 'release', title: 'Release v1.2.0', repo: going.repo, dir: going.dir, stopRefused: '' },
      { id: 2, kind: 'gitPull', title: 'Pull main', repo: held.repo, dir: held.dir, stopRefused: 'git is writing' },
    ])
  })
})

describe('unsentSheets', () => {
  it('counts the reviews with text written or picked, or line comments, and the issues with text', () => {
    const reviews = [
      { ...emptyDraft, body: 'Looks good', edited: true },
      { ...emptyDraft, body: 'Default reply', edited: false },
      { ...emptyDraft, body: '  ', edited: true },
      {
        ...emptyDraft,
        comments: [{ id: 1, path: 'a.go', side: 'RIGHT' as const, line: 3, startLine: 0, body: 'Why?' }],
      },
      { ...emptyDraft, viewed: ['a.go'] },
    ]
    const issues = [
      { ...emptyIssueDraft, body: 'On it' },
      { ...emptyIssueDraft, action: 'comment' as const },
    ]

    expect(unsentSheets(reviews, issues)).toBe(3)
    expect(unsentSheets([], [])).toBe(0)
  })
})

describe('releasePageOf', () => {
  it('builds a version’s page from any release page', () => {
    expect(releasePageOf('https://github.com/acme-org/example-repo/releases/tag/v0.3.0', '0.2.1')).toBe(
      'https://github.com/acme-org/example-repo/releases/tag/v0.2.1',
    )
  })

  it('leaves any other URL as it is', () => {
    expect(releasePageOf('', '0.2.1')).toBe('')
    expect(releasePageOf('https://example.com/x', '0.2.1')).toBe('https://example.com/x')
  })
})

describe('namesRuns', () => {
  it('says a question named the runs, by name or as a whole, and not one that named none', () => {
    expect(namesRuns({ runs: [runGoing()], sheets: 0 })).toBe(true)
    expect(namesRuns({ runs: null, sheets: 1 })).toBe(true)
    // The runs couldn't be listed beside text unsent: the question said nothing of runs.
    expect(namesRuns({ runs: [], sheets: 1 })).toBe(false)
  })
})

describe('offerOf', () => {
  const none = { action: null, label: '', page: '', move: false }
  const page = (version: string) => `https://github.com/acme-org/example-repo/releases/tag/v${version}`
  const failure = { version: '0.3.0', reason: 'It didn’t quit in time.', log: true, retryable: true }

  it('offers nothing before the status loads, for a build that doesn’t update, or with nothing newer', () => {
    expect(offerOf(null, idle)).toEqual(none)
    expect(offerOf(updateReady({ enabled: false, state: 'off' }), idle)).toEqual(none)
    expect(offerOf(updateStatus(), idle)).toEqual(none)
  })

  it('offers Restart to update for a version ready, and Update for one found', () => {
    expect(offerOf(updateReady(), idle)).toEqual({ ...none, action: 'restart', label: 'Restart to update' })
    expect(offerOf(updateReady({ state: 'available' }), idle)).toEqual({ ...none, action: 'update', label: 'Update' })
    // Hidden with Later, or about to download quietly, it is still on offer in About.
    expect(offerOf(updateReady({ bannerHidden: true }), { step: 'preparing' }).action).toBe('restart')
  })

  it('offers nothing while a version downloads or installs', () => {
    expect(offerOf(updateReady({ state: 'downloading', progress: 4 }), idle)).toEqual(none)
    expect(offerOf(updateReady({ state: 'installing' }), idle)).toEqual(none)
  })

  it('offers Try again and Download manually after a restart that failed', () => {
    expect(offerOf(updateReady(), { step: 'failed', message: 'no space' })).toEqual({
      action: 'retry',
      label: 'Try again',
      page: page('0.3.0'),
      move: false,
    })
    // With nothing found, the status names the running version's page, which holds nothing
    // to install: Download manually opens the page of the version that failed, if any.
    expect(offerOf(updateStatus(), { step: 'failed', message: 'offline' })).toEqual({
      ...none,
      action: 'retry',
      label: 'Try again',
    })
    expect(
      offerOf(updateStatus({ installFailed: { ...failure, retryable: true } }), { step: 'failed', message: 'offline' })
        .page,
    ).toBe(page('0.3.0'))
    // Before the status loads there is no page to download from, but Try again reads it.
    expect(offerOf(null, { step: 'failed', message: 'offline' })).toEqual({
      action: 'retry',
      label: 'Try again',
      page: '',
      move: false,
    })
  })

  it('offers nothing while the app quits on its own to finish an update', () => {
    expect(offerOf(updateReady({ state: 'finishing' }), idle)).toEqual(none)
  })

  it('offers Download manually alone after a restart refused for an update that fails for good', () => {
    expect(offerOf(updateReady(), { step: 'failed', message: 'It didn’t install before.', final: true })).toEqual({
      ...none,
      page: page('0.3.0'),
    })
    expect(offerOf(null, { step: 'failed', message: 'It didn’t install before.', final: true })).toEqual(none)
  })

  it('asks to move the app first where it can’t replace itself', () => {
    const away = updateReady({ location: { ok: false, kind: 'translocated', target: '' } })

    expect(offerOf(away, idle)).toEqual({ ...none, move: true })
    expect(offerOf({ ...away, state: 'available' }, idle)).toEqual({ ...none, move: true })
    expect(offerOf({ ...away, state: 'idle', latest: '', installFailed: failure }, idle)).toEqual({
      ...none,
      move: true,
    })
    expect(offerOf({ ...away, state: 'idle', latest: '' }, idle)).toEqual(none)
  })

  it('offers Try again and Download manually for the version that didn’t install, never a plain restart', () => {
    const retry = { action: 'retry', label: 'Try again', page: page('0.3.0'), move: false }

    expect(offerOf(updateReady({ installFailed: failure }), idle)).toEqual(retry)
    // Nothing found since the launch yet: Try again checks first, and Download manually
    // opens the page of the version that failed, never the running version's, which the
    // status names while nothing is found.
    expect(offerOf(updateStatus({ latest: '', installFailed: failure }), idle)).toEqual(retry)
    // A newer version found since is offered as any other.
    expect(offerOf(updateReady({ latest: '0.3.1', installFailed: failure }), idle).action).toBe('restart')
  })

  it('offers Download manually alone for an install that can’t be tried again', () => {
    const final = { ...failure, retryable: false }

    const manual = { ...none, page: page('0.3.0') }

    expect(offerOf(updateReady({ installFailed: final }), idle)).toEqual(manual)
    expect(offerOf(updateReady({ state: 'available', installFailed: final }), idle)).toEqual(manual)
    expect(offerOf(updateStatus({ latest: '', installFailed: final }), idle)).toEqual(manual)
  })
})
