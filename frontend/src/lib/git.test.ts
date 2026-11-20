import { describe, expect, it } from 'vitest'

import { homeWord, shellQuoted, shortCommit } from './git'

describe('git', () => {
  it('shortens a commit as git and GitHub do', () => {
    expect(shortCommit('a1b2c3d4e5f60718293a')).toBe('a1b2c3d')
    expect(shortCommit('a1b')).toBe('a1b')
  })

  it('quotes a word for a shell only when the shell would read it otherwise', () => {
    expect(shellQuoted('/Users/example/.local/bin/magex')).toBe('/Users/example/.local/bin/magex')
    expect(shellQuoted('bump=patch')).toBe('bump=patch')
    expect(shellQuoted('Tool Folder')).toBe(`'Tool Folder'`)
    expect(shellQuoted("it's")).toBe(`'it'\\''s'`)
    expect(shellQuoted('$HOME')).toBe(`'$HOME'`)
    expect(shellQuoted('')).toBe(`''`)
  })

  it.each([
    ['~/projects/example-repo', '~/projects/example-repo'],
    ['~/projects/My Repo/pr-7', `~/'projects/My Repo/pr-7'`],
    ["~/it's here", `~/'it'\\''s here'`],
    ['/srv/clones/a b', `'/srv/clones/a b'`],
    ['/srv/clones/example-repo', '/srv/clones/example-repo'],
    ['~', `'~'`],
    ['', `''`],
  ])('keeps the leading ~/ of %j outside the quotes, so the shell expands it', (path, want) => {
    expect(homeWord(path)).toBe(want)
  })
})
