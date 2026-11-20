import { describe, expect, it } from 'vitest'

import { deferred, nextTask } from './async'

describe('deferred', () => {
  it('stays pending until the test resolves it', async () => {
    const call = deferred<string>()
    let answered = ''
    const read = call.promise.then((text) => {
      answered = text
    })

    await Promise.resolve()
    expect(answered).toBe('')

    call.resolve('done')
    await read
    expect(answered).toBe('done')
  })

  it('fails with the reason the test rejects it with', async () => {
    const call = deferred<number>()

    call.reject(new Error('the database is busy'))

    await expect(call.promise).rejects.toThrow('the database is busy')
  })

  it('answers nothing by default', async () => {
    const call = deferred()

    call.resolve()

    await expect(call.promise).resolves.toBeUndefined()
  })
})

describe('nextTask', () => {
  it('waits past every answer already given', async () => {
    const order: string[] = []

    void Promise.resolve().then(() => {
      order.push('answer')
    })
    setTimeout(() => {
      order.push('timer')
    }, 0)

    await nextTask()

    expect(order).toEqual(['answer', 'timer'])
  })
})
