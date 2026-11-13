import { act, render, renderHook, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useLayoutEffect, useRef } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { item } from '../test/fixtures'
import { emit, listening } from '../test/platform'
import { placeIn, stepTo, useActionProgress, useSheetKeys, withStep, type SheetKeys } from './sheet'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('placeIn', () => {
  const siblings = [item({ id: 1 }), item({ id: 2 }), item({ id: 3 })]

  it('finds the cards around one, and its place in the list', () => {
    expect(placeIn(siblings, 2, 'Assigned')).toEqual({
      prev: siblings[0],
      next: siblings[2],
      position: '2 of 3 in Assigned',
    })
    expect(placeIn(siblings, 1, 'Assigned')).toMatchObject({ prev: undefined, next: siblings[1] })
    expect(placeIn(siblings, 3, 'Assigned')).toMatchObject({ prev: siblings[1], next: undefined })
  })

  it('has no place for a card the list doesn’t show', () => {
    expect(placeIn(siblings, 9, 'Assigned')).toEqual({ prev: undefined, next: undefined, position: '' })
    expect(placeIn([], 1, 'Assigned')).toEqual({ prev: undefined, next: undefined, position: '' })
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
    act(() => {
      emit('review:progress', { itemId: 7, step: 'Approving' })
    })
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
})
