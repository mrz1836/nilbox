import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useDrafts } from './drafts'

interface Note {
  body: string
}

const empty: Note = Object.freeze({ body: '' })

describe('useDrafts', () => {
  it('keeps a draft per item, starting from the empty one, until it is cleared', () => {
    const { result } = renderHook(() => useDrafts(empty))

    expect(result.current.get(12)).toBe(empty)

    act(() => {
      result.current.update(12, (d) => ({ ...d, body: 'Can you share a trace?' }))
      result.current.update(12, (d) => ({ ...d, body: `${d.body} Thanks.` }))
    })

    expect(result.current.get(12)).toEqual({ body: 'Can you share a trace? Thanks.' })
    expect(result.current.get(13)).toBe(empty)

    act(() => {
      result.current.clear(12)
      result.current.clear(13)
    })

    expect(result.current.get(12)).toBe(empty)
  })
})
