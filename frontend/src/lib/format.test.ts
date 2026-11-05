import { describe, expect, it } from 'vitest'

import { counted, errorMessage, formatCount } from './format'

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

  it('uses a string error as is', () => {
    expect(errorMessage('no local clone')).toBe('no local clone')
  })

  it('falls back for anything else', () => {
    expect(errorMessage(new Error(''))).toBe('Something went wrong.')
    expect(errorMessage(undefined)).toBe('Something went wrong.')
    expect(errorMessage({ code: 1 })).toBe('Something went wrong.')
  })
})
