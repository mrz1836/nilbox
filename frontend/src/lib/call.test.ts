import { describe, expect, it, vi } from 'vitest'

import { all, andThen, attempt, cancel, logFailure, serial, start } from './call'

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

describe('logFailure', () => {
  it('logs a failure under its message with the error as it came, not the user’s sentence', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const err = new Error('graphql: 502 Bad Gateway', { cause: { message: 'Could not reach GitHub.' } })
    const onData = vi.fn()

    logFailure(Promise.reject(err), 'could not quit:', onData)

    await vi.waitFor(() => {
      expect(logged).toHaveBeenCalledExactlyOnceWith('could not quit:', err)
    })
    expect(onData).not.toHaveBeenCalled()
  })

  it('hands the answer to onData, logging nothing', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const onData = vi.fn()

    logFailure(Promise.resolve(4), 'could not count', onData)
    logFailure(Promise.resolve('ignored'), 'could not say')

    await vi.waitFor(() => {
      expect(onData).toHaveBeenCalledExactlyOnceWith(4)
    })
    expect(logged).not.toHaveBeenCalled()
  })

  it('handles a cancelled call’s rejection too', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    let refuse: (err: Error) => void = () => undefined
    const call = Object.assign(
      new Promise<void>((_, reject) => {
        refuse = reject
      }),
      {
        cancel: () => {
          refuse(new Error('cancelled'))
        },
      },
    )

    logFailure(call, 'could not keep running:')
    cancel(call)

    await vi.waitFor(() => {
      expect(logged).toHaveBeenCalledExactlyOnceWith('could not keep running:', new Error('cancelled'))
    })
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

describe('andThen', () => {
  it('makes what it makes of the answer, and cancels every call all waits on', async () => {
    const first = cancellable([1, 2])
    const second = cancellable([3])
    const flat = andThen(all([first, second]), (got) => got.flat())

    expect(await flat).toEqual([1, 2, 3])

    flat.cancel()

    expect(first.cancel).toHaveBeenCalledOnce()
    expect(second.cancel).toHaveBeenCalledOnce()
  })

  it('stops the bound calls behind it', () => {
    const call = cancellable('a')

    andThen(call, (v) => v.length).cancel()

    expect(call.cancel).toHaveBeenCalledOnce()
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
