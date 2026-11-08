import { describe, expect, it } from 'vitest'

import { burst, confettiColors, confettiGlyphs, lifetime, type Room } from './confetti'

// seeded is a small deterministic random source (a linear congruential generator), so a
// burst can be rolled the same way every run.
function seeded(seed: number): () => number {
  let state = seed

  return () => {
    state = (state * 1_664_525 + 1_013_904_223) % 2 ** 32

    return state / 2 ** 32
  }
}

const room: Room = { above: 400, below: 500, width: 1200 }

describe('burst', () => {
  it('rolls the number of particles asked for, each with its own id', () => {
    const particles = burst(120, room, seeded(1))

    expect(particles).toHaveLength(120)
    expect(new Set(particles.map((p) => p.id)).size).toBe(120)
    expect(burst(0, room, seeded(1))).toEqual([])
  })

  it('rolls the same burst from the same random source', () => {
    expect(burst(40, room, seeded(7))).toEqual(burst(40, room, seeded(7)))
    expect(burst(40, room, seeded(7))).not.toEqual(burst(40, room, seeded(8)))
  })

  it('mixes every kind, and only a glyph carries text', () => {
    const particles = burst(400, room, seeded(3))

    expect(new Set(particles.map((p) => p.kind))).toEqual(new Set(['paper', 'ribbon', 'dot', 'glyph']))

    for (const p of particles) {
      expect(confettiColors).toContain(p.color)
    }

    for (const p of particles.filter((q) => q.kind === 'glyph')) {
      expect(confettiGlyphs).toContain(p.glyph)
      expect(p.height).toBeGreaterThanOrEqual(12)
    }

    for (const p of particles.filter((q) => q.kind !== 'glyph')) {
      expect(p.glyph).toBe('')
      expect(p.width).toBeGreaterThan(0)
    }
  })

  it('throws upward, within the window, and rains down past its bottom', () => {
    for (const p of burst(400, room, seeded(5))) {
      expect(p.peak).toBeLessThanOrEqual(0)
      expect(p.peak).toBeGreaterThanOrEqual(-room.above * 0.9)
      expect(Math.abs(p.x)).toBeLessThanOrEqual(room.width * 0.48)
      expect(p.fall).toBeGreaterThan(room.below)
      expect(Math.abs(p.spin)).toBeGreaterThanOrEqual(360)
      expect(p.delay).toBeGreaterThanOrEqual(0)
      expect(p.duration).toBeGreaterThanOrEqual(2600)
    }
  })

  it('still throws high enough from near the top of the window, and no higher than a tall one allows', () => {
    const low = burst(200, { above: 20, below: 800, width: 1200 }, seeded(9))
    const high = burst(200, { above: 5000, below: 100, width: 1200 }, seeded(9))

    expect(Math.min(...low.map((p) => p.peak))).toBeLessThan(-100)
    expect(Math.min(...high.map((p) => p.peak))).toBeGreaterThanOrEqual(-560)
    // A burst near the bottom still falls far enough to leave the window.
    expect(Math.min(...high.map((p) => p.fall))).toBeGreaterThanOrEqual(200)
  })
})

describe('lifetime', () => {
  it('is when the last particle lands', () => {
    const particles = burst(30, room, seeded(11))
    const last = Math.max(...particles.map((p) => p.delay + p.duration))

    expect(lifetime(particles)).toBe(last)
    expect(lifetime([])).toBe(0)
  })
})
