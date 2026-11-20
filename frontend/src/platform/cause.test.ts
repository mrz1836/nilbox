import { describe, expect, it } from 'vitest'

import { causeOf } from './cause'

describe('causeOf', () => {
  it('reads the backend’s words and detail from an Error’s cause', () => {
    const detail = { ref: 't3:Test' }
    const err = new Error('the command needs the user’s trust', {
      cause: { message: 'Confirm “Test” before it runs.', detail },
    })

    expect(causeOf(err)).toStrictEqual({ detail, message: 'Confirm “Test” before it runs.' })
    expect(causeOf(err)?.detail).toBe(detail)
  })

  it('reads the words alone when the cause carries no detail', () => {
    const err = new Error('call the GitHub API: dial tcp: no such host', {
      cause: { message: 'Could not reach GitHub.' },
    })

    expect(causeOf(err)).toStrictEqual({ detail: undefined, message: 'Could not reach GitHub.' })
  })

  it('reads the detail with no words when the cause’s message is missing or not text', () => {
    expect(causeOf(new Error('x', { cause: { detail: { code: 'noMatch' } } }))).toStrictEqual({
      detail: { code: 'noMatch' },
      message: '',
    })
    expect(causeOf(new Error('x', { cause: { message: 7, detail: 'x' } }))).toStrictEqual({ detail: 'x', message: '' })
    expect(causeOf(new Error('x', { cause: {} }))).toStrictEqual({ detail: undefined, message: '' })
  })

  it('keeps a detail that is null or falsy as it came', () => {
    expect(causeOf(new Error('x', { cause: { message: 'm', detail: null } }))).toStrictEqual({
      detail: null,
      message: 'm',
    })
    expect(causeOf(new Error('x', { cause: { message: 'm', detail: 0 } }))).toStrictEqual({ detail: 0, message: 'm' })
  })

  it('reads an Error whose cause is an object of any kind, an array or another Error included', () => {
    const inner = new Error('inner')

    expect(causeOf(new Error('x', { cause: inner }))).toStrictEqual({ detail: undefined, message: 'inner' })
    expect(causeOf(new Error('x', { cause: [] }))).toStrictEqual({ detail: undefined, message: '' })
  })

  it('reads nothing from an Error with no cause, or one that is not an object', () => {
    expect(causeOf(new Error('no local clone'))).toBeNull()
    expect(causeOf(new Error('x', { cause: undefined }))).toBeNull()
    expect(causeOf(new Error('x', { cause: null }))).toBeNull()
    expect(causeOf(new Error('x', { cause: 'text' }))).toBeNull()
    expect(causeOf(new Error('x', { cause: 7 }))).toBeNull()
    expect(causeOf(new Error('x', { cause: Object.assign(() => 'words', { message: 'm' }) }))).toBeNull()
  })

  it('reads only an Error', () => {
    expect(causeOf({ message: 'no local clone', cause: { message: 'Could not reach GitHub.' } })).toBeNull()
    expect(causeOf('no local clone')).toBeNull()
    expect(causeOf(undefined)).toBeNull()
    expect(causeOf(null)).toBeNull()
  })
})
