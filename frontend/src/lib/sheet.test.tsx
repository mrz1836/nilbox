import { act, render, renderHook, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useLayoutEffect, useRef } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { item } from '../test/fixtures'
import { emit, listening, send } from '../test/platform'
import {
  cameBy,
  endedState,
  endedText,
  movesOnTo,
  placeIn,
  stepPast,
  stepTo,
  useActionProgress,
  useEnded,
  useSheetKeys,
  useSheetVisits,
  withStep,
  type EndedItem,
  type ProgressEvent,
  type SheetKeys,
} from './sheet'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('placeIn', () => {
  const siblings = [item({ id: 1 }), item({ id: 2 }), item({ id: 3 })]

  it('finds the cards around one, and its place in the list', () => {
    expect(placeIn(siblings, 2, 'Yours')).toEqual({
      prev: siblings[0],
      next: siblings[2],
      position: '2 of 3 in Yours',
    })
    expect(placeIn(siblings, 1, 'Yours')).toMatchObject({ prev: undefined, next: siblings[1] })
    expect(placeIn(siblings, 3, 'Yours')).toMatchObject({ prev: siblings[1], next: undefined })
  })

  it('has no place for a card the list doesn’t show', () => {
    expect(placeIn(siblings, 9, 'Yours')).toEqual({ prev: undefined, next: undefined, position: '' })
    expect(placeIn([], 1, 'Yours')).toEqual({ prev: undefined, next: undefined, position: '' })
  })
})

describe('movesOnTo', () => {
  const first = item({ id: 1, repo: 'acme-org/api' })
  const third = item({ id: 3, repo: 'acme-org/api' })
  const siblings = [first, item({ id: 2 }), third, item({ id: 4 })]
  const alone = () => false

  it('moves on to the next card, or the one before it at the end', () => {
    expect(movesOnTo(siblings, 2, alone)?.id).toBe(3)
    expect(movesOnTo(siblings, 4, alone)?.id).toBe(3)
    expect(movesOnTo([first], 1, alone), 'none is left').toBeUndefined()
    expect(movesOnTo(siblings, 9, alone), 'a card the list doesn’t show').toBeUndefined()
  })

  it('skips the cards that leave with it', () => {
    const api = (card: { repo: string }) => card.repo === 'acme-org/api'

    expect(movesOnTo(siblings, 1, api)?.id).toBe(2)
    expect(movesOnTo(siblings, 3, api)?.id).toBe(4)
    expect(movesOnTo([first, third], 3, api)).toBeUndefined()
  })
})

describe('cameBy', () => {
  const siblings = [item({ id: 1 }), item({ id: 2 }), item({ id: 3 })]

  it('says the way a step went, with the cards it went through', () => {
    expect(cameBy(siblings, 1, 2)).toEqual({ way: 'next', cards: [1, 2, 3] })
    expect(cameBy(siblings, 3, 1)).toEqual({ way: 'prev', cards: [1, 2, 3] })
    expect(cameBy(siblings, 9, 2), 'from a card the list no longer shows').toMatchObject({ way: 'next' })
  })
})

describe('stepPast', () => {
  const cards = [1, 2, 3, 4]
  const all = () => true

  it('steps past the card the way the sheet came, else back the other way', () => {
    expect(stepPast({ way: 'next', cards }, 2, all)).toEqual({ to: 3, came: { way: 'next', cards: [1, 3, 4] } })
    expect(stepPast({ way: 'prev', cards }, 3, all)).toEqual({ to: 2, came: { way: 'prev', cards: [1, 2, 4] } })
    expect(stepPast({ way: 'next', cards }, 4, all)?.to, 'at the end').toBe(3)
    expect(stepPast({ way: 'prev', cards }, 1, all)?.to, 'at the start').toBe(2)
  })

  it('never steps back to a card it stepped past, though the list still holds it', () => {
    // #2 and #4 both ended, and the list, not yet read again, still holds both.
    const past = stepPast({ way: 'next', cards }, 2, all)
    expect(past?.to).toBe(3)

    const on = stepPast({ way: 'next', cards: past?.came.cards ?? [] }, 4, all)
    expect(on, 'back to #3, not #2').toEqual({ to: 3, came: { way: 'next', cards: [1, 3] } })
  })

  it('skips the cards the list no longer holds, and closes once none is left', () => {
    const held = (id: number) => id !== 3

    expect(stepPast({ way: 'next', cards }, 2, held)?.to).toBe(4)
    expect(stepPast({ way: 'prev', cards }, 4, held)?.to).toBe(2)
    expect(
      stepPast({ way: 'next', cards }, 2, (id) => id === 2),
      'only the card itself',
    ).toBeUndefined()
    expect(stepPast({ way: 'next', cards }, 9, all), 'a card the sheet didn’t step through').toBeUndefined()
    expect(stepPast(null, 2, all), 'a card the sheet opened on').toBeUndefined()
  })
})

describe('endedState', () => {
  it('says a merge or a close, and nothing while open', () => {
    expect(endedState('MERGED')).toBe('merged')
    expect(endedState('CLOSED')).toBe('closed')
    expect(endedState('OPEN')).toBe('')
    expect(endedState('')).toBe('')
  })
})

describe('endedText', () => {
  it('names the item and what became of it', () => {
    const ended: EndedItem = {
      id: 1,
      repo: 'acme-org/example-repo',
      number: 7,
      title: 'Add the widget cache',
      url: 'https://github.com/acme-org/example-repo/pull/7',
      state: 'merged',
    }

    expect(endedText(ended)).toBe('acme-org/example-repo#7 was merged')
    expect(endedText({ ...ended, number: 12, state: 'closed' })).toBe('acme-org/example-repo#12 was closed')
  })
})

describe('useEnded', () => {
  const shown = {
    id: 1,
    repo: 'acme-org/example-repo',
    number: 7,
    title: 'Add the widget cache',
    url: 'https://github.com/acme-org/example-repo/pull/7',
    state: 'OPEN',
  }

  it('tells once a read finds the item merged or closed, once, and waits while held', () => {
    const onEnded = vi.fn<(ended: EndedItem) => void>()
    const initialProps: { at: typeof shown | null; hold: boolean } = { at: shown, hold: false }
    const { rerender } = renderHook(
      ({ at, hold }) => {
        useEnded(at, hold, onEnded)
      },
      { initialProps },
    )

    rerender({ at: null, hold: false })
    expect(onEnded, 'nothing read, or open').not.toHaveBeenCalled()

    rerender({ at: { ...shown, state: 'MERGED' }, hold: true })
    expect(onEnded, 'held').not.toHaveBeenCalled()

    rerender({ at: { ...shown, state: 'MERGED' }, hold: false })
    expect(onEnded).toHaveBeenCalledExactlyOnceWith({
      id: 1,
      repo: 'acme-org/example-repo',
      number: 7,
      title: 'Add the widget cache',
      url: 'https://github.com/acme-org/example-repo/pull/7',
      state: 'merged',
    })

    rerender({ at: { ...shown, state: 'MERGED', title: 'Renamed' }, hold: false })
    expect(onEnded, 'told once').toHaveBeenCalledOnce()

    rerender({ at: { ...shown, id: 2, number: 8, state: 'CLOSED' }, hold: false })
    expect(onEnded).toHaveBeenLastCalledWith(expect.objectContaining({ id: 2, state: 'closed' }))
  })

  it('needs no one to hear it', () => {
    const { rerender } = renderHook(
      ({ at }) => {
        useEnded(at, false, undefined)
      },
      { initialProps: { at: shown } },
    )

    expect(() => {
      rerender({ at: { ...shown, state: 'CLOSED' } })
    }).not.toThrow()
  })
})

describe('useSheetVisits', () => {
  it('hears each opening once, marking read the item opened and the one it moved from', () => {
    const seen = vi.fn<(ids: readonly number[]) => void>()
    const onOpened = vi.fn<(itemId: number) => void>()
    const { rerender } = renderHook(
      ({ id }) => {
        useSheetVisits(id, seen, onOpened)
      },
      { initialProps: { id: 0 } },
    )

    expect(seen, 'no sheet open').not.toHaveBeenCalled()

    rerender({ id: 7 })
    expect(seen).toHaveBeenLastCalledWith([7])
    expect(onOpened).toHaveBeenLastCalledWith(7)

    rerender({ id: 7 })
    expect(seen, 'heard once').toHaveBeenCalledOnce()

    rerender({ id: 8 })
    expect(seen).toHaveBeenLastCalledWith([7, 8])
    expect(onOpened).toHaveBeenLastCalledWith(8)

    rerender({ id: 0 })
    expect(seen).toHaveBeenLastCalledWith([8])
    expect(onOpened, 'a close opens nothing').toHaveBeenCalledTimes(2)
  })

  it('needs no one to hear the openings', () => {
    const seen = vi.fn<(ids: readonly number[]) => void>()
    const { rerender } = renderHook(
      ({ id }) => {
        useSheetVisits(id, seen, undefined)
      },
      { initialProps: { id: 0 } },
    )

    rerender({ id: 3 })
    expect(seen).toHaveBeenCalledExactlyOnceWith([3])
  })
})

describe('stepTo', () => {
  it('names the card it steps to, and steps there', () => {
    const onNavigate = vi.fn()
    const step = stepTo(item({ id: 4, title: 'Fix the parser' }), onNavigate)

    expect(step?.title).toBe('Fix the parser')
    step?.onStep()
    expect(onNavigate).toHaveBeenCalledWith(4)
    expect(stepTo(undefined, onNavigate)).toBeNull()
  })
})

// KeyedSheet is an open dialog named by its title, with keys, and a field to type in.
function KeyedSheet(keys: SheetKeys) {
  const ref = useRef<HTMLDialogElement>(null)

  useLayoutEffect(() => {
    ref.current?.showModal()
  }, [])
  useSheetKeys('sheet-title', keys)

  return (
    <dialog ref={ref} aria-labelledby="sheet-title">
      <h2 id="sheet-title">Sheet</h2>
      <textarea aria-label="Your comment" />
      <div role="menu" aria-label="Snooze for…">
        <button type="button" role="menuitem">
          1 day
        </button>
      </div>
      <div role="dialog" aria-label="Labels">
        <input type="checkbox" aria-label="bug" />
      </div>
    </dialog>
  )
}

function keys(overrides: Partial<SheetKeys> = {}): SheetKeys {
  return {
    working: false,
    run: vi.fn(),
    prev: { title: 'Add the widget cache', onStep: vi.fn() },
    next: { title: 'Crash on start', onStep: vi.fn() },
    ...overrides,
  }
}

describe('useSheetKeys', () => {
  it('runs the button on ⌘↩, typing or not, and steps with the arrows while not typing', async () => {
    const k = keys()
    render(<KeyedSheet {...k} />)

    await userEvent.keyboard('{ArrowLeft}')
    expect(k.prev?.onStep).toHaveBeenCalledOnce()
    await userEvent.keyboard('{ArrowRight}')
    expect(k.next?.onStep).toHaveBeenCalledOnce()
    await userEvent.keyboard('{ArrowUp}')

    await userEvent.type(screen.getByRole('textbox', { name: 'Your comment' }), '{ArrowLeft}{ArrowRight}')
    expect(k.prev?.onStep).toHaveBeenCalledOnce()
    expect(k.next?.onStep).toHaveBeenCalledOnce()

    await userEvent.keyboard('{Meta>}{Enter}{/Meta}')
    expect(k.run).toHaveBeenCalledOnce()
  })

  it('leaves the arrows to a menu or a picker open in the sheet', async () => {
    const k = keys()
    render(<KeyedSheet {...k} />)

    screen.getByRole('menuitem', { name: '1 day' }).focus()
    await userEvent.keyboard('{ArrowLeft}{ArrowRight}')
    screen.getByRole('checkbox', { name: 'bug' }).focus()
    await userEvent.keyboard('{ArrowLeft}{ArrowRight}{Meta>}{Enter}{/Meta}')
    expect(k.run, 'a picker’s ⌘↩ is its own').not.toHaveBeenCalled()

    expect(k.prev?.onStep).not.toHaveBeenCalled()
    expect(k.next?.onStep).not.toHaveBeenCalled()
  })

  it('steps nowhere at the ends, nor with a modifier held', async () => {
    const k = keys({ prev: null, next: null })
    const { rerender } = render(<KeyedSheet {...k} />)

    await userEvent.keyboard('{ArrowLeft}{ArrowRight}')

    const held = keys()
    rerender(<KeyedSheet {...held} />)
    await userEvent.keyboard('{Alt>}{ArrowLeft}{/Alt}{Control>}{ArrowRight}{/Control}{Meta>}{ArrowLeft}{/Meta}')
    expect(held.prev?.onStep).not.toHaveBeenCalled()
    expect(held.next?.onStep).not.toHaveBeenCalled()
  })

  it('hears nothing while an action runs, nor while another dialog is over it', async () => {
    const k = keys({ working: true })
    const { rerender } = render(<KeyedSheet {...k} />)

    await userEvent.keyboard('{ArrowLeft}{Meta>}{Enter}{/Meta}')
    expect(k.prev?.onStep).not.toHaveBeenCalled()
    expect(k.run).not.toHaveBeenCalled()

    const over = keys()
    const confirm = document.createElement('dialog')
    confirm.setAttribute('open', '')
    rerender(<KeyedSheet {...over} />)
    document.body.append(confirm)

    await userEvent.keyboard('{ArrowRight}{Meta>}{Enter}{/Meta}')
    expect(over.next?.onStep).not.toHaveBeenCalled()
    expect(over.run).not.toHaveBeenCalled()
    confirm.remove()
  })
})

describe('withStep', () => {
  it('adds each new step once, and nothing when no action runs', () => {
    const started = { title: 'Approve', target: 'acme-org/example-repo#7', steps: [] }

    expect(withStep(null, 'Approving')).toBeNull()
    expect(withStep(started, 'Approving')).toEqual({
      title: 'Approve',
      target: 'acme-org/example-repo#7',
      steps: ['Approving'],
    })

    const once = { title: 'Approve', target: 'acme-org/example-repo#7', steps: ['Approving'] }

    expect(withStep(once, 'Approving')).toBe(once)
  })
})

describe('useActionProgress', () => {
  it('follows the steps of its own item’s action while one runs, and stops listening once gone', () => {
    const { result, rerender, unmount } = renderHook(({ id }) => useActionProgress('review:progress', id), {
      initialProps: { id: 7 },
    })

    // Before any action, a step is no news.
    send('review:progress', { itemId: 7, step: 'Approving' })
    expect(result.current[0]).toBeNull()

    act(() => {
      result.current[1]({ title: 'Approve', target: 'acme-org/example-repo#7', steps: [] })
    })
    act(() => {
      emit('review:progress', { itemId: 8, step: 'Merging' })
      emit('review:progress', { itemId: 7, step: 'Approving' })
      emit('review:progress', { itemId: 7, step: 'Approving' })
    })
    expect(result.current[0]?.steps).toEqual(['Approving'])

    // Stepped to another pull request, the sheet hears only that one's steps.
    rerender({ id: 8 })
    act(() => {
      emit('review:progress', { itemId: 7, step: 'Merging' })
      emit('review:progress', { itemId: 8, step: 'Merging' })
    })
    expect(result.current[0]?.steps).toEqual(['Approving', 'Merging'])

    act(() => {
      result.current[1](null)
    })
    expect(result.current[0]).toBeNull()

    unmount()
    expect(listening('review:progress')).toBe(0)
  })

  it('listens on its new event once the event changes', () => {
    const initialProps: { event: ProgressEvent } = { event: 'review:progress' }
    const { result, rerender } = renderHook(({ event }) => useActionProgress(event, 7), { initialProps })

    act(() => {
      result.current[1]({ title: 'Close', target: 'acme-org/example-repo#7', steps: [] })
    })
    rerender({ event: 'issue:progress' })
    expect(listening('review:progress')).toBe(0)
    expect(listening('issue:progress')).toBe(1)

    send('issue:progress', { itemId: 7, step: 'Closing' })
    expect(result.current[0]?.steps).toEqual(['Closing'])
  })
})
