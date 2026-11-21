import { render, screen } from '@testing-library/react'
import { useEffect, useRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { followResizes, reduceMotion } from './stubs'

// The globals as the setup file leaves them, before any test stubs one.
const original = { matchMedia: globalThis.matchMedia, ResizeObserver: globalThis.ResizeObserver }

// Sized counts the resizes its box was told of.
function Sized() {
  const box = useRef<HTMLDivElement>(null)
  const [resizes, setResizes] = useState(0)

  useEffect(() => {
    const observer = new ResizeObserver(() => {
      setResizes((n) => n + 1)
    })

    if (box.current !== null) {
      observer.observe(box.current)
    }

    return () => {
      observer.disconnect()
    }
  }, [])

  return (
    <div ref={box} data-testid="box">
      {resizes} resizes
    </div>
  )
}

describe('reduceMotion', () => {
  it('asks for less motion, and for nothing else', () => {
    reduceMotion()

    expect(window.matchMedia('(prefers-reduced-motion: reduce)').matches).toBe(true)
    expect(window.matchMedia('(prefers-color-scheme: dark)').matches).toBe(false)
  })

  it('is put back before the next test', () => {
    expect(globalThis.matchMedia).toBe(original.matchMedia)
  })
})

describe('followResizes', () => {
  it('keeps each observer’s callback and what it follows, and resizes inside act', () => {
    const resizes = followResizes()
    const { unmount } = render(<Sized />)

    expect(resizes.callbacks).toHaveLength(1)
    expect(resizes.observed).toEqual([screen.getByTestId('box')])

    resizes.resize()
    resizes.resize()
    expect(screen.getByTestId('box')).toHaveTextContent('2 resizes')

    unmount()
    expect(resizes.observed).toEqual([])
  })

  it('stops following an element unobserved', () => {
    const resizes = followResizes()
    const observer = new ResizeObserver(vi.fn())
    const first = document.createElement('div')
    const second = document.createElement('div')

    observer.observe(first)
    observer.observe(second)
    observer.unobserve(first)
    observer.unobserve(first)

    expect(resizes.observed).toEqual([second])
  })

  it('is put back before the next test', () => {
    expect(globalThis.ResizeObserver).toBe(original.ResizeObserver)
  })
})
