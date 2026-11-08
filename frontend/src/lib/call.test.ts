import { describe, expect, it, vi } from 'vitest'

import { all, attempt, cancel, serial, start } from './call'

// cancellable is a call that records its cancellation, as a bound call does.
function cancellable<T>(value: T) {
  return Object.assign(Promise.resolve(value), { cancel: vi.fn() })
}

describe('attempt', () => {
  it('reports a value, or the message for a failure', async () => {
    expect(await attempt(Promise.resolve(4))).toEqual({ ok: true, data: 4 })
    expect(await attempt(Promise.reject(new Error('database is locked')))).toEqual({
      ok: false,
      error: 'database is locked',
    })
  })

  it('prefers the message the backend worded for the user', async () => {
    const err = new Error('graphql: 502 Bad Gateway', { cause: { message: 'Could not reach GitHub.' } })

    expect(await attempt(Promise.reject(err))).toEqual({ ok: false, error: 'Could not reach GitHub.' })
  })
})

describe('start', () => {
  it('makes the call', async () => {
    expect(await start(() => Promise.resolve('made'))).toBe('made')
  })

  it('turns a throw into a rejected call', async () => {
    await expect(
      start(() => {
        throw new Error('no backend')
      }),
    ).rejects.toThrow('no backend')
  })
})

describe('cancel', () => {
  it('cancels a call that can be cancelled, and leaves any other alone', () => {
    const call = cancellable('a')

    cancel(call)
    cancel(Promise.resolve('b'))

    expect(call.cancel).toHaveBeenCalledOnce()
  })
})

describe('all', () => {
  it('waits for every call, and cancels each one that can be', async () => {
    const first = cancellable(1)
    const second = Promise.resolve('two')
    const both = all([first, second])

    expect(await both).toEqual([1, 'two'])

    both.cancel()

    expect(first.cancel).toHaveBeenCalledOnce()
  })
})

describe('serial', () => {
  it('starts each task once the one before it has settled, however it ended', async () => {
    const queue = serial()
    const order: string[] = []
    let fail: (err: Error) => void = () => undefined

    const first = queue(
      () =>
        new Promise<void>((_, reject) => {
          order.push('first')
          fail = reject
        }),
    )
    const second = queue(() => {
      order.push('second')

      return Promise.resolve('done')
    })

    await Promise.resolve()
    expect(order).toEqual(['first'])

    fail(new Error('database is locked'))

    await expect(first).rejects.toThrow('database is locked')
    expect(await second).toBe('done')
    expect(order).toEqual(['first', 'second'])
  })
})
