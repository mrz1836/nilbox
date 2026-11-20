import { describe, expect, it } from 'vitest'

import { badgeText, counted, errorMessage, fileDate, fileName, formatCount, joinList, refText } from './format'

describe('formatCount', () => {
  it('formats with the locale grouping', () => {
    expect(formatCount(4980)).toBe(new Intl.NumberFormat().format(4980))
    expect(formatCount(7)).toBe('7')
  })
})

describe('counted', () => {
  it('pairs a count with the right noun', () => {
    expect(counted(1, 'file', 'files')).toBe('1 file')
    expect(counted(0, 'file', 'files')).toBe('0 files')
    expect(counted(1204, 'file', 'files')).toBe(`${formatCount(1204)} files`)
  })

  it('groups the digits of a count from 1,000', () => {
    expect(counted(1000, 'clone', 'clones')).toBe(`${formatCount(1000)} clones`)
    expect(counted(999, 'clone', 'clones')).toBe('999 clones')
  })
})

describe('badgeText', () => {
  it('shows the count up to 99, then caps it at 99+', () => {
    expect(badgeText(0)).toBe('0')
    expect(badgeText(1)).toBe('1')
    expect(badgeText(7)).toBe('7')
    expect(badgeText(99)).toBe('99')
    expect(badgeText(100)).toBe('99+')
    expect(badgeText(1000)).toBe('99+')
  })
})

describe('joinList', () => {
  it('joins names for a sentence, with a comma before the last of three or more', () => {
    expect(joinList([])).toBe('')
    expect(joinList(['model'])).toBe('model')
    expect(joinList(['model', 'effort'])).toBe('model and effort')
    expect(joinList(['model', 'effort', 'plan mode'])).toBe('model, effort, and plan mode')
    expect(joinList(['model', 'effort', 'plan mode', 'profile'])).toBe('model, effort, plan mode, and profile')
  })
})

describe('errorMessage', () => {
  it('uses an error message verbatim', () => {
    expect(errorMessage(new Error('run gh auth login'))).toBe('run gh auth login')
  })

  it('prefers the message the backend worded for the user', () => {
    const err = new Error('refresh the inbox: call the GitHub API: dial tcp: no such host', {
      cause: { message: 'Could not reach GitHub.' },
    })
    expect(errorMessage(err)).toBe('Could not reach GitHub.')
  })

  it('ignores a cause without a message', () => {
    expect(errorMessage(new Error('no local clone', { cause: {} }))).toBe('no local clone')
    expect(errorMessage(new Error('no local clone', { cause: { message: '' } }))).toBe('no local clone')
    expect(errorMessage(new Error('no local clone', { cause: { message: 7 } }))).toBe('no local clone')
    expect(errorMessage(new Error('no local clone', { cause: 'text' }))).toBe('no local clone')
    expect(errorMessage(new Error('no local clone', { cause: 7 }))).toBe('no local clone')
    expect(errorMessage(new Error('no local clone', { cause: undefined }))).toBe('no local clone')
    expect(errorMessage(new Error('', { cause: null }))).toBe('Something went wrong.')
    expect(errorMessage(new Error('', { cause: { message: 7, detail: 'x' } }))).toBe('Something went wrong.')
  })

  it('reads the words beside any detail the cause carries', () => {
    const err = new Error('the command needs the user’s trust', {
      cause: { message: 'Confirm “Test” before it runs.', detail: { ref: 't3:Test' } },
    })
    expect(errorMessage(err)).toBe('Confirm “Test” before it runs.')
  })

  it('reads only an Error’s cause', () => {
    expect(errorMessage({ message: 'no local clone', cause: { message: 'Could not reach GitHub.' } })).toBe(
      'Something went wrong.',
    )
  })

  it('uses a string error as is', () => {
    expect(errorMessage('no local clone')).toBe('no local clone')
  })

  it('falls back for anything else', () => {
    expect(errorMessage(new Error(''))).toBe('Something went wrong.')
    expect(errorMessage(undefined)).toBe('Something went wrong.')
    expect(errorMessage({ code: 1 })).toBe('Something went wrong.')
  })
})

describe('refText', () => {
  it('names an item by its repository and number', () => {
    expect(refText({ repo: 'acme-org/example-repo', number: 14 })).toBe('acme-org/example-repo#14')
  })
})

describe('fileDate', () => {
  it('writes a day as a file name carries it, with two digits for the month and the day', () => {
    expect(fileDate(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05')
    expect(fileDate(new Date(2026, 10, 14))).toBe('2026-11-14')
  })
})

describe('fileName', () => {
  it('is the last part of a path', () => {
    expect(fileName('/exports/shared prompts.json')).toBe('shared prompts.json')
    expect(fileName('C:\\exports\\all.json')).toBe('all.json')
    expect(fileName('all.json')).toBe('all.json')
  })
})
