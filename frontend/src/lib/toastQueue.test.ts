import { describe, expect, it } from 'vitest'

import { emptyQueue, offerToast, turnOf, type Queued, type ToastQueueState } from './toastQueue'

interface Message extends Queued {
  text: string
}

// offers runs the offers in order from an empty queue.
function offers(...steps: [string, Message | null][]): ToastQueueState<Message> {
  return steps.reduce((q, [key, toast]) => offerToast(q, key, toast), emptyQueue<Message>())
}

// shown names the toast showing, and the number it says.
function shown(q: ToastQueueState<Message>): string {
  const head = q.entries[0]

  if (head === undefined) {
    return 'none'
  }

  const turn = turnOf(q, head.key)

  return `${head.toast.text} ${String(turn.position)} of ${String(turn.total)}`
}

const snoozed: Message = { text: 'Snoozed' }
const dismissed: Message = { text: 'Dismissed' }
const cleaned: Message = { text: 'Cleaned up', background: true }
const updated: Message = { text: 'Updated', background: true }

describe('the toast queue', () => {
  it('shows one toast at a time, numbering it while more wait', () => {
    const q = offers(['inbox', snoozed], ['app', cleaned])

    expect(shown(q)).toBe('Snoozed 1 of 2')
    expect(turnOf(q, 'app').showing).toBe(false)

    const next = offerToast(q, 'inbox', null)
    expect(shown(next)).toBe('Cleaned up 2 of 2')

    const empty = offerToast(next, 'app', null)
    expect(shown(empty)).toBe('none')
    expect(empty.done, 'an empty queue starts counting afresh').toBe(0)
  })

  it('shows a toast that answers a click at once, and puts the one showing back in line', () => {
    const q = offers(['app', cleaned], ['inbox', snoozed])

    expect(shown(q)).toBe('Snoozed 1 of 2')
    expect(shown(offerToast(q, 'inbox', null)), 'the cleanup shows once the click is answered').toBe(
      'Cleaned up 2 of 2',
    )
  })

  it('shows the newest click first, keeping the earlier one, Undo and all, in line', () => {
    const q = offers(['inbox', snoozed], ['autopilot', dismissed])

    expect(shown(q)).toBe('Dismissed 1 of 2')
    expect(shown(offerToast(q, 'autopilot', null))).toBe('Snoozed 2 of 2')
  })

  it('makes a background toast wait behind every other, in the order they came', () => {
    const q = offers(['app', cleaned], ['update', updated], ['inbox', snoozed])

    expect(q.entries.map((e) => e.toast.text)).toEqual(['Snoozed', 'Cleaned up', 'Updated'])
  })

  it('replaces a source’s own toast in place, numbered as it was', () => {
    const q = offers(['inbox', snoozed], ['inbox', dismissed])

    expect(shown(q)).toBe('Dismissed 1 of 1')

    const waiting = offers(['inbox', snoozed], ['app', cleaned], ['app', updated])
    expect(waiting.entries.map((e) => e.toast.text)).toEqual(['Snoozed', 'Updated'])
    expect(shown(waiting)).toBe('Snoozed 1 of 2')
  })

  it('moves a source’s new toast that answers a click to the front', () => {
    const q = offers(['inbox', snoozed], ['autopilot', dismissed], ['inbox', { text: 'Restored' }])

    expect(q.entries.map((e) => e.toast.text)).toEqual(['Restored', 'Dismissed'])
  })

  it('keeps a toast handed to another Toast in its place and number, whichever offers first', () => {
    // The page's Toast lets go first, then the sheet's takes it.
    const handed = offers(['app', cleaned], ['page', snoozed], ['page', null], ['sheet', snoozed])
    expect(shown(handed)).toBe('Snoozed 1 of 2')
    expect(turnOf(handed, 'sheet').showing).toBe(true)

    // The sheet's Toast takes it first, then the page's lets go.
    const taken = offers(['app', cleaned], ['page', snoozed], ['sheet', snoozed], ['page', null])
    expect(shown(taken)).toBe('Snoozed 1 of 2')
    expect(turnOf(taken, 'sheet').showing).toBe(true)

    // Alone, it is still the only one.
    expect(shown(offers(['page', snoozed], ['page', null], ['sheet', snoozed]))).toBe('Snoozed 1 of 1')
  })

  it('counts nothing for a waiting toast taken off, and nothing for an offer it holds already', () => {
    const q = offers(['inbox', snoozed], ['app', cleaned], ['app', null])

    expect(shown(q)).toBe('Snoozed 1 of 1')
    expect(offerToast(q, 'inbox', snoozed)).toBe(q)
    expect(offerToast(q, 'nobody', null)).toBe(q)
  })
})
