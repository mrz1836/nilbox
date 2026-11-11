import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { scriptRun, scriptRunChunk } from '../test/fixtures'
import { platform } from '../test/platform'
import {
  filterRuns,
  formatDuration,
  isLive,
  readLog,
  runDuration,
  runFilterOrder,
  runFilters,
  runPlace,
  runStatusLabels,
  runWhere,
} from './scriptRuns'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('the runsâ words', () => {
  it('names every status, never by color alone', () => {
    expect(runStatusLabels).toEqual({
      running: 'Running',
      succeeded: 'Done',
      failed: 'Failed',
      stopped: 'Stopped',
      interrupted: 'Interrupted',
      orphaned: 'Orphaned',
    })
  })

  it('counts running and orphaned runs as going', () => {
    expect(isLive('running')).toBe(true)
    expect(isLive('orphaned')).toBe(true)
    expect(isLive('succeeded')).toBe(false)
    expect(isLive('interrupted')).toBe(false)
  })

  it('says where a run runs, naming the terminal chosen for launches', () => {
    expect(runPlace('app', 'iterm2')).toBe(`In ${brand.name}`)
    expect(runPlace('terminal', 'iterm2')).toBe('In iTerm2')
    expect(runPlace('terminal', 'terminal')).toBe('In Terminal')
    expect(runPlace('terminal', null)).toBe('In your terminal')
  })

  it('names what a run ran in: its repository, or its folder', () => {
    expect(runWhere({ repo: 'acme-org/sdk', dir: '~/projects/sdk' })).toBe('acme-org/sdk')
    expect(runWhere({ repo: '', dir: '~/scratch' })).toBe('~/scratch')
  })

  it.each([
    [-5_000, '0 s'],
    [0, '0 s'],
    [38_400, '38 s'],
    [120_000, '2 min'],
    [125_000, '2 min 5 s'],
    [3_600_000, '1 h'],
    [3_780_000, '1 h 3 min'],
  ])('says %i ms as %s', (ms, text) => {
    expect(formatDuration(ms)).toBe(text)
  })

  it('says how long an ended run took, and nothing for one still going', () => {
    expect(runDuration({ startedAt: '2026-06-15T12:00:00Z', finishedAt: '2026-06-15T12:00:38Z' })).toBe('38 s')
    expect(runDuration({ startedAt: '2026-06-15T12:00:00Z', finishedAt: '' })).toBe('')
    expect(runDuration({ startedAt: '', finishedAt: '2026-06-15T12:00:38Z' })).toBe('')
  })

  it('filters Hooks by both hooks actions and checks, and Updates by the tools updating themselves', () => {
    expect(runFilterOrder.map((id) => [runFilters[id].label, runFilters[id].kinds])).toEqual([
      ['All', []],
      ['Release', ['release']],
      ['Hooks', ['hooks', 'checks']],
      ['Scripts', ['script']],
      ['Updates', ['update']],
    ])
  })
})

describe('filterRuns', () => {
  const runs = [
    scriptRun({ id: 5, kind: 'release', repo: 'acme-org/SDK' }),
    scriptRun({ id: 4, kind: 'hooks', repo: 'acme-org/web' }),
    scriptRun({ id: 3, kind: 'checks', repo: 'other-org/sdk-tools' }),
    scriptRun({ id: 2, kind: 'script', repo: '', dir: '~/sdk' }),
    scriptRun({ id: 1, kind: 'script', repo: 'acme-org/example-repo' }),
    scriptRun({ id: 0, kind: 'update', title: 'Update magex', repo: '', dir: '~/.local/bin' }),
  ]

  // ids lists the runs picked, by ID.
  function ids(filter: Parameters<typeof filterRuns>[1], search = ''): number[] {
    return filterRuns(runs, filter, search).map((r) => r.id)
  }

  it('lists every run with no filter or search, in its order', () => {
    expect(ids('all')).toEqual([5, 4, 3, 2, 1, 0])
  })

  it('picks the kinds a filter names, Hooks taking the checks too', () => {
    expect(ids('release')).toEqual([5])
    expect(ids('hooks')).toEqual([4, 3])
    expect(ids('scripts')).toEqual([2, 1])
    expect(ids('updates')).toEqual([0])
  })

  it('finds part of a repository’s name, trimmed and in any case, and no folder', () => {
    expect(ids('all', '  sdk ')).toEqual([5, 3])
    expect(ids('all', 'ACME-ORG/')).toEqual([5, 4, 1])
    expect(ids('scripts', 'sdk')).toEqual([])
    expect(ids('all', '   ')).toEqual([5, 4, 3, 2, 1, 0])
  })
})

describe('readLog', () => {
  it('reads a log in one page as plain text', async () => {
    platform.scriptRunOutput.mockResolvedValue(scriptRunChunk({ text: '\x1b[32mok\x1b[0m\n' }))

    expect(await readLog(4)).toBe('ok\n')
    expect(platform.scriptRunOutput).toHaveBeenCalledExactlyOnceWith(4, 0, 0)
  })

  it('reads page after page, up to the size the last page found', async () => {
    platform.scriptRunOutput
      .mockResolvedValueOnce(scriptRunChunk({ text: 'one\n', offset: 0, next: 4, size: 8, done: false }))
      .mockResolvedValueOnce(scriptRunChunk({ text: 'two\n', offset: 4, next: 8, size: 8, done: false }))

    expect(await readLog(4)).toBe('one\ntwo\n')
    expect(platform.scriptRunOutput.mock.calls).toEqual([
      [4, 0, 0],
      [4, 4, 0],
    ])
  })

  it('reads a log cut down between two pages again from the start, once', async () => {
    platform.scriptRunOutput
      .mockResolvedValueOnce(scriptRunChunk({ text: 'head\n', offset: 0, next: 5, size: 9, done: false }))
      .mockResolvedValueOnce(scriptRunChunk({ text: '', offset: 5, next: 5, size: 3, restart: true }))
      .mockResolvedValueOnce(scriptRunChunk({ text: 'cut', offset: 0, next: 3, size: 3, done: true }))

    expect(await readLog(4)).toBe('cut')
  })

  it('reads a log that grew smaller between two pages again from the start', async () => {
    // The second page is within the log cut down, so the backend reads it as asked: only
    // the size says the log is not the one read.
    platform.scriptRunOutput
      .mockResolvedValueOnce(scriptRunChunk({ text: 'head\n', offset: 0, next: 5, size: 20, done: false }))
      .mockResolvedValueOnce(scriptRunChunk({ text: '[cut]', offset: 5, next: 10, size: 12, done: false }))
      .mockResolvedValueOnce(scriptRunChunk({ text: 'head\n[cut]\ntail\n', offset: 0, next: 16, size: 16, done: true }))

    expect(await readLog(4)).toBe('head\n[cut]\ntail\n')
    expect(platform.scriptRunOutput.mock.calls).toEqual([
      [4, 0, 0],
      [4, 5, 0],
      [4, 0, 0],
    ])
  })

  it('gives what it read when the log is cut down again', async () => {
    platform.scriptRunOutput
      .mockResolvedValueOnce(scriptRunChunk({ text: 'head\n', offset: 0, next: 5, size: 20, done: false }))
      .mockResolvedValueOnce(scriptRunChunk({ text: '', offset: 5, next: 5, size: 3, restart: true }))
      .mockResolvedValueOnce(scriptRunChunk({ text: 'new\n', offset: 0, next: 4, size: 9, done: false }))
      .mockResolvedValueOnce(scriptRunChunk({ text: 'x', offset: 4, next: 5, size: 6, done: false }))

    expect(await readLog(4)).toBe('new\n')
    expect(platform.scriptRunOutput).toHaveBeenCalledTimes(4)
  })

  it('stops when a page makes no progress, rather than ask forever', async () => {
    platform.scriptRunOutput
      .mockResolvedValueOnce(scriptRunChunk({ text: 'head\n', offset: 0, next: 5, size: 9, done: false }))
      .mockResolvedValueOnce(scriptRunChunk({ text: '', offset: 5, next: 5, size: 9, done: false }))

    expect(await readLog(4)).toBe('head\n')
    expect(platform.scriptRunOutput).toHaveBeenCalledTimes(2)
  })

  it('fails as the read fails', async () => {
    platform.scriptRunOutput.mockRejectedValue(new Error('the run is no longer recorded'))

    await expect(readLog(4)).rejects.toThrow('the run is no longer recorded')
  })
})
