import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import type { UpdateLocation } from '../platform'
import { runGoing, scriptRun, updateReady, updateStatus } from '../test/fixtures'
import { emptyIssueDraft } from './issueDrafts'
import { emptyDraft } from './reviewDrafts'
import {
  aboutLine,
  bannerOf,
  latestReleasePage,
  locationNote,
  locationWords,
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

    expect(bannerOf(updateReady({ location }), idle, '')).toEqual({ kind: 'location', version: '0.3.0', ready: true })
    expect(bannerOf(updateReady({ location, state: 'available', mode: 'notify' }), idle, '')).toEqual({
      kind: 'location',
      version: '0.3.0',
      ready: false,
    })
    expect(bannerOf(updateReady({ location, bannerHidden: true }), idle, '')).toEqual({ kind: 'none' })
  })

  it('says the last launch’s update didn’t install, until it is put away', () => {
    const failed = updateReady({ installFailed: { version: '0.3.0', reason: 'It wouldn’t open.', log: true } })

    expect(bannerOf(failed, idle, '')).toEqual({ kind: 'lastLaunch', version: '0.3.0', current: '0.2.0' })
    expect(bannerOf(failed, idle, '0.3.0')).toEqual({ kind: 'ready', version: '0.3.0' })
    expect(bannerOf({ ...failed, state: 'idle', latest: '' }, idle, '0.3.0')).toEqual({ kind: 'none' })
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
      detail: 'Backing up your data first.',
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
      detail: 'Backing up your data first.',
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

    expect(aboutLine(off)).toEqual({ text: 'Updates are off in builds made from source.', checkedAt: '', note: '' })
  })

  it('words each state', () => {
    expect(aboutLine(updateStatus())).toEqual({ text: 'Up to date', checkedAt: '2026-06-15T11:48:00Z', note: '' })
    expect(aboutLine(updateStatus({ lastChecked: '' }))).toEqual({ text: 'Not checked yet', checkedAt: '', note: '' })
    expect(aboutLine(updateStatus({ state: 'checking', checking: true })).text).toBe('Checking…')
    expect(aboutLine(updateStatus({ checking: true })).text).toBe('Checking…')
    expect(aboutLine(updateReady({ state: 'downloading', progress: 42 })).text).toBe('Downloading 0.3.0… 42%')
    expect(aboutLine(updateReady()).text).toBe('0.3.0 is ready')
    expect(aboutLine(updateReady({ state: 'available' })).text).toBe('0.3.0 is available')
    expect(aboutLine(updateReady({ state: 'installing' })).text).toBe('Restarting into 0.3.0…')
  })

  it('says the last check failed, and keeps a failure under a version found', () => {
    expect(aboutLine(updateStatus({ lastError: 'Couldn’t reach the releases: offline' }))).toEqual({
      text: 'Last check failed: Couldn’t reach the releases: offline.',
      checkedAt: '',
      note: '',
    })
    expect(aboutLine(updateReady({ lastError: 'Couldn’t install 0.3.0: no space' }))).toEqual({
      text: '0.3.0 is ready',
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
      `git is writing the clone’s files. Quitting waits up to a few seconds for it. If it hasn’t ended by then, it goes on after ${brand.name} closes.`,
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

describe('release pages', () => {
  const url = 'https://github.com/acme-org/example-repo/releases/tag/v0.3.0'

  it('builds a version’s page and the newest release’s from any release page', () => {
    expect(releasePageOf(url, '0.2.1')).toBe('https://github.com/acme-org/example-repo/releases/tag/v0.2.1')
    expect(latestReleasePage(url)).toBe('https://github.com/acme-org/example-repo/releases/latest')
  })

  it('leaves any other URL as it is', () => {
    expect(releasePageOf('', '0.2.1')).toBe('')
    expect(latestReleasePage('https://example.com/x')).toBe('https://example.com/x')
  })
})
