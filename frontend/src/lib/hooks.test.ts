import { describe, expect, it } from 'vitest'

import type { HookAction, HookChangedFile, HookLastRun, HookRepo, ScriptRunStatus } from '../platform'
import { hookChecksResult, hookRepo, hooksList, scriptRun } from '../test/fixtures'
import {
  actionButton,
  attentionCount,
  blocked,
  busyRun,
  changedHeading,
  changedNote,
  checksHeading,
  checksTone,
  checksTook,
  ciNote,
  hookState,
  installable,
  lastChecksWords,
  liveDirs,
  matchClones,
  matchHooks,
  ours,
  ownerOf,
  question,
  replaceQuestion,
  sharedQuestion,
  uninstallable,
  uninstallQuestion,
} from './hooks'

// row is a fixture's repository by its name.
function row(name: string): HookRepo {
  const found = hooksList().repos.find((r) => r.name === name)

  if (found === undefined) {
    throw new Error(`no row ${name}`)
  }

  return found
}

// last is a row's newest checks run, ended with status.
function last(status: ScriptRunStatus, change: Partial<HookLastRun> = {}): HookLastRun {
  return { id: 3, status, summary: '', finishedAt: '2026-06-15T11:00:00Z', ...change }
}

// file is a file the checks changed, with changes made.
function file(change: Partial<HookChangedFile> = {}): HookChangedFile {
  return { path: 'README.md', staged: false, wasDirty: false, by: [], ...change }
}

describe('attentionCount', () => {
  it('counts the repositories that need attention', () => {
    expect(attentionCount(hooksList().repos)).toBe(4)
    expect(attentionCount([hookRepo()])).toBe(0)
    expect(attentionCount(undefined)).toBe(0)
  })
})

describe('matchHooks and matchClones', () => {
  it('keep what holds the search by name, repository, or folder, in any case', () => {
    const repos = [
      hookRepo(),
      hookRepo({ repo: '', name: 'scratch', dir: '~/projects/scratch' }),
      hookRepo({ repo: 'acme-org/api', name: 'acme-org/api', dir: '~/projects/api-server' }),
    ]

    expect(matchHooks(repos, '  WEB ').map((r) => r.name)).toEqual(['acme-org/web'])
    expect(matchHooks(repos, 'scr').map((r) => r.name)).toEqual(['scratch'])
    expect(matchHooks(repos, 'api-server').map((r) => r.name)).toEqual(['acme-org/api'])
    expect(matchHooks(repos, 'acme-org').map((r) => r.name)).toEqual(['acme-org/web', 'acme-org/api'])
    expect(matchHooks(repos, '')).toHaveLength(3)

    const clones = hooksList().notSetUp

    expect(matchClones(clones, 'NOTES').map((c) => c.name)).toEqual(['notes'])
    expect(matchClones(clones, 'projects/site').map((c) => c.name)).toEqual(['site'])
    expect(matchClones(clones, 'nothing')).toEqual([])
  })
})

describe('installable', () => {
  it('covers installs, updates, and reinstalls in a folder of their own, and nothing else', () => {
    expect(installable(row('acme-org/api'))).toBe(true)
    expect(installable(row('acme-org/cli'))).toBe(true)
    expect(installable(hookRepo({ group: 'attention', state: 'notExecutable', action: 'reinstall' }))).toBe(true)
    expect(installable(row('acme-org/ops'))).toBe(false)
    expect(installable(row('acme-org/sdk'))).toBe(false)
    expect(installable(row('acme-org/web'))).toBe(false)
    expect(installable(row('scratch'))).toBe(false)
  })
})

describe('hookState', () => {
  it.each<[string, Partial<HookRepo>, string, string]>([
    ['missing', { state: 'missing', action: 'install', group: 'attention' }, 'bad', 'No hook yet'],
    ['outdated', { state: 'outdated', action: 'update', group: 'attention' }, 'warn', 'Outdated hook'],
    [
      'not executable',
      { state: 'notExecutable', action: 'reinstall', group: 'attention' },
      'warn',
      'The hook isn’t executable',
    ],
    [
      'husky’s',
      { state: 'other', otherTool: 'husky', action: 'replace', group: 'attention' },
      'warn',
      'Another tool’s hook is there (husky)',
    ],
    [
      'another tool’s',
      { state: 'other', action: 'replace', group: 'attention' },
      'warn',
      'Another tool’s hook is there',
    ],
    ['ready', {}, 'ok', 'Installed'],
    ['unknown', { state: 'unknown' }, 'info', 'Installed · go-pre-commit couldn’t say whether it’s current'],
    ['unreadable', { state: 'unreadable', group: 'attention' }, 'bad', 'Couldn’t read the hooks'],
    [
      'in a shared folder six use',
      { state: 'missing', action: 'install', group: 'attention', shared: true, hooksDir: '~/.githooks', sharedWith: 6 },
      'warn',
      'Shared hooks folder ~/.githooks (6 repos)',
    ],
    [
      'in a shared folder only it is known to use',
      { state: 'outdated', action: 'update', group: 'attention', shared: true, hooksDir: '~/.githooks', sharedWith: 1 },
      'warn',
      'Shared hooks folder ~/.githooks',
    ],
    ['ready in a shared folder', { shared: true, hooksDir: '~/.githooks', sharedWith: 6 }, 'ok', 'Installed'],
  ])('says a hook is %s', (_, change, tone, text) => {
    expect(hookState(hookRepo(change))).toEqual({ tone, text })
  })
})

describe('ours and uninstallable', () => {
  it('know go-pre-commit’s own hooks', () => {
    for (const state of ['ready', 'outdated', 'notExecutable', 'unknown'] as const) {
      expect(ours(hookRepo({ state }))).toBe(true)
      expect(uninstallable(hookRepo({ state }))).toBe(true)
    }

    for (const state of ['missing', 'other', 'unreadable'] as const) {
      expect(ours(hookRepo({ state }))).toBe(false)
      expect(uninstallable(hookRepo({ state }))).toBe(false)
    }

    expect(uninstallable(hookRepo({ state: 'other', alsoInstalled: ['commit-msg'] }))).toBe(true)
  })
})

describe('actionButton', () => {
  it('names the action, with an ellipsis where it asks first', () => {
    expect(actionButton(row('acme-org/api'))).toEqual({ action: 'install', label: 'Install', asks: false })
    expect(actionButton(row('acme-org/cli'))).toEqual({ action: 'update', label: 'Update', asks: false })
    expect(actionButton(row('acme-org/sdk'))).toEqual({ action: 'replace', label: 'Replace…', asks: true })
    expect(actionButton(row('acme-org/ops'))).toEqual({ action: 'install', label: 'Install…', asks: true })
    expect(actionButton(hookRepo({ group: 'attention', state: 'notExecutable', action: 'reinstall' }))).toEqual({
      action: 'reinstall',
      label: 'Reinstall',
      asks: false,
    })
  })

  it('offers none for a ready row, one turned off, or one it can’t act on', () => {
    expect(actionButton(row('acme-org/web'))).toBeNull()
    expect(actionButton(row('scratch'))).toBeNull()
    expect(actionButton(hookRepo({ group: 'attention', state: 'unreadable' }))).toBeNull()
  })
})

describe('the questions', () => {
  it('ask before replacing another tool’s hook, naming where it goes', () => {
    expect(ownerOf('husky')).toBe('husky’s')
    expect(ownerOf('')).toBe('another tool’s')
    expect(replaceQuestion(row('acme-org/sdk'))).toBe(
      'Replace husky’s pre-commit hook in acme-org/sdk? go-pre-commit moves it to .git/hooks/pre-commit.backup.<number>, and puts it back if you uninstall.',
    )
    expect(replaceQuestion({ name: 'acme-org/x', dir: '~/projects/x', hooksDir: '~/hooks', otherTool: '' })).toBe(
      'Replace another tool’s pre-commit hook in acme-org/x? go-pre-commit moves it to ~/hooks/pre-commit.backup.<number>, and puts it back if you uninstall.',
    )
  })

  it('ask before changing a shared hooks folder, counting who shares it', () => {
    expect(sharedQuestion(row('acme-org/ops'), 'install')).toBe(
      '~/.githooks is shared by 6 of your repositories, and by any other repository that uses it. Installing here installs go-pre-commit’s hook for all of them.',
    )
    expect(sharedQuestion({ hooksDir: '~/.githooks', sharedWith: 1 }, 'uninstall')).toBe(
      '~/.githooks is shared by other repositories. Uninstalling here uninstalls go-pre-commit’s hooks for all of them.',
    )
  })

  it('say what Uninstall removes and puts back', () => {
    expect(uninstallQuestion(hookRepo())).toBe('go-pre-commit removes its pre-commit hook from acme-org/web.')
    expect(
      uninstallQuestion(
        hookRepo({ alsoInstalled: ['commit-msg'], backups: ['pre-commit.backup.1', 'pre-commit.backup.2'] }),
      ),
    ).toBe(
      'go-pre-commit removes its pre-commit and commit-msg hooks from acme-org/web. It puts back the hook it set aside, pre-commit.backup.2.',
    )
    expect(uninstallQuestion(hookRepo({ state: 'other', alsoInstalled: ['commit-msg'] }))).toBe(
      'go-pre-commit removes its commit-msg hook from acme-org/web.',
    )
  })

  it.each<[string, HookRepo, HookAction, ReturnType<typeof question>]>([
    [
      'replacing husky’s hook',
      row('acme-org/sdk'),
      'replace',
      {
        title: 'Replace husky’s hook?',
        text: replaceQuestion(row('acme-org/sdk')),
        confirm: 'Replace husky’s hook',
        danger: true,
      },
    ],
    [
      'replacing a hook in a shared folder',
      hookRepo({ ...row('acme-org/sdk'), shared: true, hooksDir: '~/.githooks', sharedWith: 2 }),
      'replace',
      {
        title: 'Replace husky’s hook?',
        text: 'Replace husky’s pre-commit hook in acme-org/sdk? go-pre-commit moves it to ~/.githooks/pre-commit.backup.<number>, and puts it back if you uninstall. ~/.githooks is shared by 2 of your repositories, and by any other repository that uses it. Installing here installs go-pre-commit’s hook for all of them.',
        confirm: 'Replace husky’s hook',
        danger: true,
      },
    ],
    [
      'uninstalling',
      hookRepo(),
      'uninstall',
      {
        title: 'Uninstall the hook from acme-org/web?',
        text: 'go-pre-commit removes its pre-commit hook from acme-org/web.',
        confirm: 'Uninstall the hook',
        danger: true,
      },
    ],
    [
      'installing in a shared folder',
      row('acme-org/ops'),
      'install',
      {
        title: 'Install the hook in a shared folder?',
        text: sharedQuestion(row('acme-org/ops'), 'install'),
        confirm: 'Install for all of them',
        danger: false,
      },
    ],
    [
      'updating in a shared folder',
      row('acme-org/ops'),
      'update',
      {
        title: 'Update the hook in a shared folder?',
        text: sharedQuestion(row('acme-org/ops'), 'update'),
        confirm: 'Update for all of them',
        danger: false,
      },
    ],
    [
      'reinstalling in a shared folder',
      row('acme-org/ops'),
      'reinstall',
      {
        title: 'Reinstall the hook in a shared folder?',
        text: sharedQuestion(row('acme-org/ops'), 'reinstall'),
        confirm: 'Reinstall for all of them',
        danger: false,
      },
    ],
  ])('word what %s asks', (_, repo, action, asked) => {
    expect(question(repo, action)).toEqual(asked)
  })
})

describe('lastChecksWords', () => {
  it.each<[HookLastRun, string]>([
    [{ id: 0, status: 'succeeded', summary: '', finishedAt: '' }, ''],
    [last('running', { finishedAt: '' }), 'checks running'],
    [last('orphaned', { finishedAt: '' }), 'checks running'],
    [last('succeeded'), 'checks passed'],
    [last('failed'), 'checks failed'],
    [last('stopped'), 'checks stopped'],
    [last('interrupted'), 'checks interrupted'],
  ])('says how the last checks went', (run, words) => {
    expect(lastChecksWords(run)).toBe(words)
  })
})

describe('ciNote', () => {
  it('names the go-pre-commit the CI pins only when it differs from the one in use', () => {
    expect(ciNote('1.14.0', '1.13.1')).toBe(
      'This repository’s CI uses go-pre-commit v1.14.0 (GO_PRE_COMMIT_VERSION); you have 1.13.1.',
    )
    expect(ciNote('1.13.1', '1.13.1')).toBe('')
    expect(ciNote('', '1.13.1')).toBe('')
    expect(ciNote('1.14.0', '')).toBe('')
  })
})

describe('busyRun and liveDirs', () => {
  const runs = [
    scriptRun({ id: 4, status: 'running', dir: '~/projects/web' }),
    scriptRun({ id: 3, status: 'orphaned', dir: '~/projects/api' }),
    scriptRun({ id: 2, status: 'running', dir: '~/projects/web' }),
    scriptRun({ id: 1, dir: '~/projects/cli' }),
  ]

  it('find the run going in a clone', () => {
    expect(busyRun(runs, '~/projects/web')).toBe(4)
    expect(busyRun(runs, '~/projects/api')).toBe(3)
    expect(busyRun(runs, '~/projects/cli')).toBeNull()
    expect(busyRun(null, '~/projects/web')).toBeNull()
  })

  it('key the folders runs go in, whatever their order', () => {
    expect(liveDirs(runs)).toBe('~/projects/api\n~/projects/web')
    expect(liveDirs([...runs].reverse())).toBe('~/projects/api\n~/projects/web')
    expect(liveDirs([runs[3] as (typeof runs)[number]])).toBe('')
    expect(liveDirs(null)).toBe('')
  })
})

describe('blocked', () => {
  it('knows an install another tool’s hook stopped', () => {
    expect(blocked({ outcome: 'nothingInstalled' })).toBe(true)
    expect(blocked({ outcome: 'installed' })).toBe(false)
  })
})

describe('the checks', () => {
  it('head each scope', () => {
    expect(checksHeading(hookChecksResult())).toBe('Checks in acme-org/web · all files')
    expect(checksHeading(hookChecksResult({ scope: 'staged' }))).toBe('Checks in acme-org/web · staged files')
    expect(checksHeading(hookChecksResult({ scope: 'one', check: 'lint' }))).toBe('lint in acme-org/web · all files')
  })

  it('tone the result by its counts', () => {
    expect(checksTone(hookChecksResult())).toBe('bad')
    expect(checksTone(hookChecksResult({ failed: 0, errors: ['no go.mod'] }))).toBe('bad')
    expect(checksTone(hookChecksResult({ failed: 0, skipped: 1 }))).toBe('warn')
    expect(checksTone(hookChecksResult({ failed: 0 }))).toBe('ok')
    expect(checksTone(hookChecksResult({ failed: 0, passed: 0 }))).toBe('info')
  })

  it('say how long they took', () => {
    expect(checksTook(0)).toBe('')
    expect(checksTook(350)).toBe('350 ms')
    expect(checksTook(38_000)).toBe('38 s')
    expect(checksTook(72_000)).toBe('1 min 12 s')
  })

  it('say which files they changed, and how', () => {
    expect(changedHeading([file()])).toBe('Changed 1 file:')
    expect(changedHeading(hookChecksResult().changed)).toBe('Changed 2 files:')
    expect(changedNote(file({ staged: true, by: ['whitespace'] }))).toBe('fixed and staged by whitespace')
    expect(changedNote(file({ by: ['eof', 'fumpt'] }))).toBe('fixed by eof and fumpt')
    expect(changedNote(file({ staged: true }))).toBe('staged')
    expect(changedNote(file({ wasDirty: true }))).toBe('already had uncommitted changes')
    expect(changedNote(file({ staged: true, wasDirty: true, by: ['whitespace'] }))).toBe(
      'fixed and staged by whitespace; already had uncommitted changes',
    )
    expect(changedNote(file())).toBe('')
  })
})
