import { describe, expect, it } from 'vitest'

import { counted, errorMessage, fileDate, fileName, formatCount, refText } from './format'

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
    expect(errorMessage(new Error('', { cause: null }))).toBe('Something went wrong.')
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
