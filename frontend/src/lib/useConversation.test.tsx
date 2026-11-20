import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotRunEvents, DescribeConversation } from '../platform'
import { describeConversation, describeTurn, runEvent, runEventsPage } from '../test/fixtures'
import { emit, platform } from '../test/platform'
import { useConversation } from './useConversation'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const id = '00000000-0000-4000-8000-000000000001'

// page is a page of a turn's events numbered from seq, each its own line, done or not.
function page(from: number, count: number, done: boolean): AutopilotRunEvents {
  return runEventsPage({
    done,
    events: Array.from({ length: count }, (_, i) =>
      runEvent({ seq: from + i, offset: (from + i) * 100, text: `said ${String(from + i)}` }),
    ),
  })
}

// held is a promise the test settles itself.
function held<T>() {
  let settle: (value: T) => void = () => undefined
  const promise = new Promise<T>((resolve) => {
    settle = resolve
  })

  return { promise, settle }
}

// conversation is the current conversation with the turns given.
function conversation(turns = [describeTurn()], overrides: Partial<DescribeConversation> = {}): DescribeConversation {
  return describeConversation({ id, turns, ...overrides })
}

describe('useConversation', () => {
  it('reads the conversation as it mounts, and each finished turn’s events once', async () => {
    platform.describeItConversation.mockResolvedValue(conversation([describeTurn(), describeTurn({ n: 2 })]))
    platform.describeItEvents.mockImplementation((_id, turn) => Promise.resolve(page(turn * 10, 2, true)))

    const { result } = renderHook(() => useConversation())

    expect(result.current.conversation).toBeUndefined()
    expect(result.current.eventsOf(1)).toEqual({ events: [], loaded: false, error: '' })

    await waitFor(() => {
      expect(result.current.eventsOf(2).loaded).toBe(true)
    })
    expect(result.current.conversation?.turns).toHaveLength(2)
    expect(result.current.eventsOf(1).events.map((e) => e.text)).toEqual(['said 10', 'said 11'])
    expect(platform.describeItEvents.mock.calls).toEqual([
      [id, 1, 0],
      [id, 2, 0],
    ])

    act(() => {
      emit('describeit:updated', { conversationId: id, turn: 0 })
    })
    await waitFor(() => {
      expect(platform.describeItConversation).toHaveBeenCalledTimes(2)
    })
    expect(platform.describeItEvents).toHaveBeenCalledTimes(2)
  })

  it('follows the running turn’s events after the last it has, until the turn ends', async () => {
    const running = conversation([describeTurn({ state: 'running', reply: '' })], { running: true })

    platform.describeItConversation.mockResolvedValue(running)
    platform.describeItEvents
      .mockResolvedValueOnce(page(1, 2, false))
      .mockResolvedValueOnce(page(3, 1, false))
      .mockResolvedValueOnce(page(4, 1, true))

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.eventsOf(1).events).toHaveLength(2)
    })

    act(() => {
      emit('describeit:updated', { conversationId: id, turn: 1 })
    })
    await waitFor(() => {
      expect(result.current.eventsOf(1).events).toHaveLength(3)
    })
    expect(platform.describeItEvents.mock.lastCall).toEqual([id, 1, 2])

    platform.describeItConversation.mockResolvedValue(conversation())
    act(() => {
      emit('describeit:updated', { conversationId: id, turn: 1 })
    })
    await waitFor(() => {
      expect(result.current.eventsOf(1).events).toHaveLength(4)
    })
    expect(platform.describeItEvents.mock.lastCall).toEqual([id, 1, 3])
    expect(result.current.conversation?.running).toBe(false)

    act(() => {
      emit('describeit:updated', { conversationId: id, turn: 1 })
    })
    await waitFor(() => {
      expect(platform.describeItConversation).toHaveBeenCalledTimes(4)
    })
    expect(platform.describeItEvents).toHaveBeenCalledTimes(3)
  })

  it('reads one turn’s events one at a time, with one more waiting', async () => {
    const first = held<AutopilotRunEvents>()

    platform.describeItConversation.mockResolvedValue(
      conversation([describeTurn({ state: 'running' })], { running: true }),
    )
    platform.describeItEvents.mockReturnValueOnce(first.promise).mockResolvedValue(page(3, 1, false))

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(platform.describeItEvents).toHaveBeenCalledOnce()
    })

    // Each read of the conversation asks for the turn's events again while the first read
    // of them is under way: one more waits, and the rest are that one.
    for (let i = 1; i <= 3; i++) {
      act(() => {
        emit('describeit:updated', { conversationId: id, turn: 1 })
      })
      await waitFor(() => {
        expect(platform.describeItConversation).toHaveBeenCalledTimes(1 + i)
      })
    }

    expect(platform.describeItEvents).toHaveBeenCalledOnce()

    await act(async () => {
      first.settle(page(1, 2, false))
      await first.promise
    })
    await waitFor(() => {
      expect(result.current.eventsOf(1).events).toHaveLength(3)
    })
    expect(platform.describeItEvents.mock.calls).toEqual([
      [id, 1, 0],
      [id, 1, 2],
    ])
  })

  it('reads the conversation one read at a time, with one more waiting', async () => {
    const first = held<DescribeConversation>()

    platform.describeItConversation.mockReturnValueOnce(first.promise).mockResolvedValue(conversation())

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(platform.describeItConversation).toHaveBeenCalledOnce()
    })

    for (let i = 0; i < 3; i++) {
      act(() => {
        emit('describeit:updated', { conversationId: '', turn: 0 })
      })
    }

    expect(platform.describeItConversation).toHaveBeenCalledOnce()

    await act(async () => {
      first.settle(describeConversation())
      await first.promise
    })
    await waitFor(() => {
      expect(result.current.conversation?.id).toBe(id)
    })
    expect(platform.describeItConversation).toHaveBeenCalledTimes(2)
  })

  it('reads again for its own conversation alone, or for any while it holds none', async () => {
    platform.describeItConversation.mockResolvedValue(describeConversation())

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.conversation?.id).toBe('')
    })

    platform.describeItConversation.mockResolvedValue(conversation())
    act(() => {
      emit('describeit:updated', { conversationId: id, turn: 1 })
    })
    await waitFor(() => {
      expect(result.current.conversation?.id).toBe(id)
    })

    act(() => {
      emit('describeit:updated', { conversationId: 'another', turn: 1 })
    })
    expect(platform.describeItConversation).toHaveBeenCalledTimes(2)
  })

  it('says why the conversation or a turn’s events couldn’t be read', async () => {
    platform.describeItConversation.mockRejectedValueOnce(new Error('Describe it isn’t ready in this build yet.'))
    platform.describeItEvents.mockRejectedValue(new Error('The turn’s stream couldn’t be read.'))

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.error).toBe('Describe it isn’t ready in this build yet.')
    })

    platform.describeItConversation.mockResolvedValue(conversation())
    act(() => {
      emit('describeit:updated', { conversationId: '', turn: 0 })
    })
    await waitFor(() => {
      expect(result.current.eventsOf(1).error).toBe('The turn’s stream couldn’t be read.')
    })
    expect(result.current.error).toBe('')
    expect(result.current.eventsOf(1).loaded).toBe(true)
  })

  it('goes on reading after a read that throws', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    platform.describeItConversation.mockImplementationOnce(() => {
      throw new Error('boom')
    })

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not follow the conversation', expect.any(Error))
    })

    act(() => {
      emit('describeit:updated', { conversationId: '', turn: 0 })
    })
    await waitFor(() => {
      expect(result.current.conversation?.id).toBe('')
    })
  })

  it('sends a message, showing the conversation it answers with, and follows its turn', async () => {
    const sent = held<DescribeConversation>()

    platform.sendToDescribeIt.mockReturnValueOnce(sent.promise)

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.conversation?.id).toBe('')
    })

    let went: Promise<unknown> = Promise.resolve()

    act(() => {
      went = result.current.send({ text: 'Pin failing PRs.', attach: [{ kind: 'routine', id: 3 }] })
    })
    expect(result.current.sending).toBe(true)
    expect(platform.sendToDescribeIt).toHaveBeenCalledWith({
      text: 'Pin failing PRs.',
      attach: [{ kind: 'routine', id: 3 }],
    })

    await act(async () => {
      sent.settle(conversation([describeTurn({ text: 'Pin failing PRs.', state: 'running' })], { running: true }))
      await went
    })
    expect(result.current.sending).toBe(false)
    expect(result.current.conversation?.running).toBe(true)
    await waitFor(() => {
      expect(platform.describeItEvents).toHaveBeenCalledWith(id, 1, 0)
    })

    platform.sendToDescribeIt.mockRejectedValueOnce(new Error('A turn is running.'))
    await act(async () => {
      expect(await result.current.send({ text: 'Again.', attach: [] })).toEqual({
        ok: false,
        error: 'A turn is running.',
      })
    })
    expect(result.current.sendError).toBe('A turn is running.')
  })

  it('drops a read that was under way when a call answered with something newer', async () => {
    platform.describeItConversation.mockResolvedValueOnce(describeConversation())

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.conversation?.id).toBe('')
    })

    const stale = held<DescribeConversation>()

    platform.describeItConversation.mockReturnValueOnce(stale.promise)
    act(() => {
      emit('describeit:updated', { conversationId: '', turn: 0 })
    })

    await act(async () => {
      await result.current.send({ text: 'Pin failing PRs.', attach: [] })
    })
    expect(result.current.conversation?.running).toBe(true)

    await act(async () => {
      stale.settle(describeConversation())
      await stale.promise
    })
    expect(result.current.conversation?.id).toBe(id)
    expect(result.current.conversation?.running).toBe(true)
  })

  it('stops the turn running, then reads the conversation again', async () => {
    platform.describeItConversation.mockResolvedValue(
      conversation([describeTurn({ state: 'running' })], { running: true }),
    )

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.conversation?.running).toBe(true)
    })

    platform.describeItConversation.mockResolvedValue(conversation([describeTurn({ state: 'stopped' })]))
    act(() => {
      result.current.stop()
    })
    expect(result.current.stopping).toBe(true)
    await waitFor(() => {
      expect(result.current.conversation?.turns[0]?.state).toBe('stopped')
    })
    expect(platform.stopDescribeIt).toHaveBeenCalledWith(id)
    expect(result.current.stopping).toBe(false)

    platform.stopDescribeIt.mockRejectedValueOnce(new Error('It had ended.'))
    act(() => {
      result.current.stop()
    })
    await waitFor(() => {
      expect(result.current.stopError).toBe('It had ended.')
    })
  })

  it('stops nothing while there is no conversation, and reads again when asked', async () => {
    const { result } = renderHook(() => useConversation())

    act(() => {
      result.current.stop()
    })
    expect(platform.stopDescribeIt).not.toHaveBeenCalled()

    await waitFor(() => {
      expect(result.current.conversation?.id).toBe('')
    })
    act(() => {
      result.current.stop()
      result.current.refresh()
    })
    await waitFor(() => {
      expect(platform.describeItConversation).toHaveBeenCalledTimes(2)
    })
    expect(platform.stopDescribeIt).not.toHaveBeenCalled()
  })

  it('starts a conversation anew with the agent chosen, leaving the old one’s events behind', async () => {
    platform.describeItConversation.mockResolvedValue(conversation())
    platform.describeItEvents.mockResolvedValue(page(1, 1, true))

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.eventsOf(1).loaded).toBe(true)
    })

    await act(async () => {
      await result.current.startNew({ profile: 'work', model: 'opus', effort: '' })
    })
    expect(platform.newDescribeItConversation).toHaveBeenCalledWith({ profile: 'work', model: 'opus', effort: '' })
    expect(result.current.conversation?.turns).toEqual([])
    expect(result.current.conversation?.agent).toEqual({ profile: 'work', model: 'opus', effort: '' })

    await act(async () => {
      await result.current.startNew({ profile: '', model: '', effort: '' })
    })

    platform.newDescribeItConversation.mockRejectedValueOnce(new Error('A turn is running.'))
    await act(async () => {
      await result.current.startNew({ profile: '', model: '', effort: '' })
    })
    expect(result.current.newError).toBe('A turn is running.')
    expect(result.current.starting).toBe(false)
  })

  it('shows another conversation’s turns afresh', async () => {
    platform.describeItConversation.mockResolvedValue(conversation())
    platform.describeItEvents.mockResolvedValue(page(1, 1, true))

    const { result } = renderHook(() => useConversation())

    await waitFor(() => {
      expect(result.current.eventsOf(1).events).toHaveLength(1)
    })

    platform.describeItConversation.mockResolvedValue(
      describeConversation({ id: '00000000-0000-4000-8000-000000000002', turns: [describeTurn()] }),
    )
    platform.describeItEvents.mockResolvedValue(page(1, 2, true))
    act(() => {
      emit('describeit:updated', { conversationId: id, turn: 0 })
    })
    await waitFor(() => {
      expect(result.current.eventsOf(1).events).toHaveLength(2)
    })
    expect(platform.describeItEvents.mock.lastCall).toEqual(['00000000-0000-4000-8000-000000000002', 1, 0])
  })
})
