import { describe, expect, it } from 'vitest'

import { bumpInput, magexTarget, repoScripts, scriptQuestion, scriptSection, yourEntry } from '../test/fixtures'
import {
  argsOf,
  changedCommand,
  emptyReason,
  firstChoice,
  magexTargets,
  matchTargets,
  otherPlace,
  shownSections,
  trustTitle,
  yoursSection,
} from './repoScripts'

describe('trustTitle', () => {
  it('names the script, the repository, and the file it comes from', () => {
    expect(trustTitle(scriptQuestion(), 'acme-org/sdk')).toBe('Run “Test” from acme-org/sdk’s t3.json?')
    expect(trustTitle(scriptQuestion({ source: 'tasks' }), 'acme-org/sdk')).toBe(
      'Run “Test” from acme-org/sdk’s .vscode/tasks.json?',
    )
    expect(trustTitle(scriptQuestion({ name: 'deploy', source: 'magex' }), 'sdk')).toBe(
      'Run “deploy” from sdk’s magefiles?',
    )
    expect(trustTitle(scriptQuestion({ source: 'yours' }), 'sdk')).toBe('Run “Test” from sdk’s your scripts?')
  })
})

describe('changedCommand', () => {
  it('is the command trusted before, only when a changed command reads differently now', () => {
    expect(changedCommand(scriptQuestion({ trust: 'changed', trusted: 'magex lint:fix', command: 'magex lint' }))).toBe(
      'magex lint:fix',
    )
    expect(changedCommand(scriptQuestion({ trust: 'changed', trusted: 'magex lint', command: 'magex lint' }))).toBe('')
    expect(changedCommand(scriptQuestion({ trust: 'changed', trusted: '' }))).toBe('')
    expect(changedCommand(scriptQuestion({ trust: 'ask', trusted: 'magex lint:fix' }))).toBe('')
  })
})

describe('shownSections', () => {
  it('shows each source on with scripts, an error, or magex’s note, and hides the rest', () => {
    expect(shownSections(repoScripts()).map((s) => s.source)).toEqual(['t3', 'tasks', 'magex'])

    const page = repoScripts({
      sections: [
        scriptSection({ source: 'yours', title: 'Yours' }),
        scriptSection({ error: 'unreadable' }),
        scriptSection({ source: 'tasks', off: true, error: 'unreadable' }),
        scriptSection({ source: 'magex', note: 'magex isn’t installed.' }),
      ],
    })

    expect(shownSections(page).map((s) => s.source)).toEqual(['t3', 'magex'])
    expect(shownSections(repoScripts({ sections: [scriptSection({ note: 'a note' })] }))).toEqual([])
    expect(
      shownSections(repoScripts({ sections: [scriptSection({ source: 'yours', entries: [yourEntry()] })] })),
    ).toEqual([])
  })
})

describe('yoursSection', () => {
  it('is the user’s own scripts, or null for a page without them', () => {
    expect(yoursSection(repoScripts())?.title).toBe('Yours')
    expect(yoursSection(repoScripts({ off: true, sections: [] }))).toBeNull()
  })
})

describe('magexTargets', () => {
  it('splits magex’s section into its chips, the repository’s own targets, and the built-ins', () => {
    const section = scriptSection({
      source: 'magex',
      common: ['magex:lint', 'magex:gone'],
      entries: [magexTarget('deploy', 'Deploy', { custom: true, args: false }), magexTarget('lint', 'Lint')],
    })

    const split = magexTargets(section)

    expect(split.common.map((e) => e.name)).toEqual(['lint'])
    expect(split.custom.map((e) => e.name)).toEqual(['deploy'])
    expect(split.builtins.map((e) => e.name)).toEqual(['lint'])
  })
})

describe('matchTargets', () => {
  it('keeps the targets whose name or description holds the search, in any case', () => {
    const entries = [magexTarget('lint', 'Run the linters'), magexTarget('test', 'Run the tests')]

    expect(matchTargets(entries, '  ').map((e) => e.name)).toEqual(['lint', 'test'])
    expect(matchTargets(entries, 'LINT').map((e) => e.name)).toEqual(['lint'])
    expect(matchTargets(entries, 'the tests').map((e) => e.name)).toEqual(['test'])
    expect(matchTargets(entries, 'deploy')).toEqual([])
  })
})

describe('argsOf', () => {
  it('reads the words the field holds, separated by spaces', () => {
    expect(argsOf('')).toEqual([])
    expect(argsOf('  bump=minor \t push  ')).toEqual(['bump=minor', 'push'])
  })
})

describe('firstChoice', () => {
  it('starts a choice on the first that fills in its default, or else the first', () => {
    expect(firstChoice(bumpInput())).toBe(0)
    expect(firstChoice(bumpInput({ default: 'minor' }))).toBe(1)
    expect(firstChoice(bumpInput({ default: 'nope' }))).toBe(0)
    expect(firstChoice(bumpInput({ default: 'x', options: [] }))).toBe(0)
    expect(
      firstChoice(
        bumpInput({
          default: 'patch',
          options: [
            { label: 'Minor', value: 'minor', shown: 'Minor (minor)' },
            { label: 'Patch', value: 'patch', shown: 'Patch (patch)' },
            { label: 'Fix', value: 'patch', shown: 'Fix (patch)' },
          ],
        }),
      ),
    ).toBe(1)
  })
})

describe('emptyReason', () => {
  const t3 = scriptSection()
  const tasks = scriptSection({ source: 'tasks', title: 'From .vscode/tasks.json' })
  const magexOff = scriptSection({ source: 'magex', title: 'magex', off: true })

  // reason words why the page with sections shows nothing.
  const reason = (...sections: ReturnType<typeof scriptSection>[]) => emptyReason(repoScripts({ sections }))

  it('says the files list nothing, naming each', () => {
    expect(reason(t3, tasks)).toEqual({
      lines: ['acme-org/sdk’s t3.json and .vscode/tasks.json list no commands.'],
      off: false,
    })
    expect(reason(t3, { ...tasks, found: false })).toEqual({
      lines: ['acme-org/sdk’s t3.json lists no commands.', 'acme-org/sdk has no .vscode/tasks.json.'],
      off: false,
    })
  })

  it('says the clone has neither file', () => {
    expect(reason({ ...t3, found: false }, { ...tasks, found: false }).lines).toEqual([
      'acme-org/sdk has no t3.json or .vscode/tasks.json.',
    ])
  })

  it('says which sources are turned off, pointing to Settings', () => {
    expect(reason({ ...t3, off: true }, { ...tasks, off: true })).toEqual({
      lines: ['Commands from t3.json and VS Code tasks are turned off in Settings → Integrations.'],
      off: true,
    })
    expect(reason({ ...t3, off: true }, { ...tasks, off: true }, magexOff).lines).toEqual([
      'Commands from t3.json and VS Code tasks and magex targets are turned off in Settings → Integrations.',
    ])
    expect(reason({ ...t3, found: false }, magexOff)).toEqual({
      lines: ['acme-org/sdk has no t3.json.', 'magex targets are turned off in Settings → Integrations.'],
      off: true,
    })
  })

  it('says there is nothing to show when no source is listed', () => {
    expect(reason()).toEqual({ lines: ['acme-org/sdk has no scripts to show.'], off: false })
    expect(reason(scriptSection({ source: 'yours', title: 'Yours' })).lines).toEqual([
      'acme-org/sdk has no scripts to show.',
    ])
  })

  it('says only that the repository has nothing of its own while the user’s scripts show', () => {
    expect(reason(scriptSection({ source: 'yours', title: 'Yours', entries: [yourEntry()] })).lines).toEqual([
      'acme-org/sdk has no commands of its own to show.',
    ])
    expect(
      reason(scriptSection({ source: 'yours', title: 'Yours', entries: [yourEntry()] }), { ...t3, found: false }).lines,
    ).toEqual(['acme-org/sdk has no t3.json.'])
  })
})

describe('otherPlace', () => {
  it('is the terminal for the app, and the app for the terminal', () => {
    expect(otherPlace('app')).toBe('terminal')
    expect(otherPlace('terminal')).toBe('app')
  })
})
