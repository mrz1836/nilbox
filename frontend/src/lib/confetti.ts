// Confetti for the inbox-nil party: a burst of paper, ribbons, dots, and code glyphs
// thrown up from one point, then falling with gravity and tumbling in 3D. This module only
// rolls the dice, from a random source a test can fix; components/Confetti.tsx draws each
// particle with CSS animations, so a burst costs no frame loop.

// ParticleKind is a particle's shape: a square of paper, a thin ribbon, a round dot, or a
// bit of code.
export type ParticleKind = 'paper' | 'ribbon' | 'dot' | 'glyph'

// Particle is one piece of confetti and its flight. Distances are in px from the burst's
// origin, times in ms.
export interface Particle {
  id: number
  kind: ParticleKind
  // glyph is the text a glyph shows, and "" for every other kind.
  glyph: string
  // color is a CSS color: a brand token, so the confetti follows the theme.
  color: string
  // x is how far the particle drifts sideways by the end of its flight.
  x: number
  // peak is the height it is thrown to (negative is up), and fall where it ends up.
  peak: number
  fall: number
  // spin is how far it turns, in degrees, about the axis (ax, ay, 1).
  spin: number
  ax: number
  ay: number
  // width and height size a shape; a glyph's height is its font size.
  width: number
  height: number
  delay: number
  duration: number
}

// confettiColors are the brand's festive colors. --nil-highlight is the logo mark's warm
// highlight, which the confetti layer defines.
export const confettiColors: readonly string[] = [
  'var(--color-accent)',
  'var(--nil-highlight)',
  'var(--color-success)',
  'var(--color-info)',
  'var(--color-warning)',
  'var(--color-caution)',
  'var(--color-danger)',
]

// confettiGlyphs are the bits of code mixed in with the paper.
export const confettiGlyphs: readonly string[] = [
  'nil',
  '{}',
  '∅',
  '0',
  ':=',
  '</>',
  ';',
  '&&',
  '=>',
  '++',
  '[]',
  '//',
  'go',
  '✓',
]

const degree = Math.PI / 180

// pick returns a random element of a non-empty list.
function pick(list: readonly string[], random: () => number): string {
  return list[Math.min(Math.floor(random() * list.length), list.length - 1)] ?? ''
}

// kindAt maps a roll to a kind: mostly paper, then glyphs, ribbons, and dots.
function kindAt(roll: number): ParticleKind {
  if (roll < 0.5) {
    return 'paper'
  }

  if (roll < 0.72) {
    return 'glyph'
  }

  return roll < 0.88 ? 'ribbon' : 'dot'
}

// sizeOf is a kind's width and height in px.
function sizeOf(kind: ParticleKind, between: (lo: number, hi: number) => number): [number, number] {
  switch (kind) {
    case 'paper':
      return [between(6, 10), between(9, 15)]
    case 'ribbon':
      return [between(3, 4.5), between(14, 24)]
    case 'dot': {
      const d = between(5, 9)

      return [d, d]
    }
    case 'glyph':
      return [0, Math.round(between(12, 20))]
  }
}

// Room is the space around a burst's origin, in px: up to the top of the window, down to
// its bottom, and its full width.
export interface Room {
  above: number
  below: number
  width: number
}

// minRise is the least height a burst is thrown to, however near the top it goes off.
const minRise = 160

// burst rolls count particles to fill room. Each is thrown in a cone that opens upward, no
// higher than the top of the window and no wider than it, then rains down past the bottom.
export function burst(count: number, room: Room, random: () => number = Math.random): Particle[] {
  const between = (lo: number, hi: number) => lo + random() * (hi - lo)
  const rise = Math.min(Math.max(room.above * 0.9, minRise), 560)
  const spread = Math.min(room.width * 0.48, 760)
  const particles: Particle[] = []

  for (let id = 0; id < count; id++) {
    const kind = kindAt(random())
    const [width, height] = sizeOf(kind, between)
    const angle = between(-165, -15) * degree
    const reach = between(0.3, 1)

    particles.push({
      id,
      kind,
      glyph: kind === 'glyph' ? pick(confettiGlyphs, random) : '',
      color: pick(confettiColors, random),
      x: Math.round(Math.cos(angle) * reach * spread),
      peak: Math.round(Math.sin(angle) * reach * rise),
      fall: Math.round(Math.max(room.below, 200) + between(20, 160)),
      spin: Math.round((random() < 0.5 ? -1 : 1) * between(360, 1440)),
      ax: Number(between(-1, 1).toFixed(2)),
      ay: Number(between(-1, 1).toFixed(2)),
      width: Number(width.toFixed(1)),
      height: Number(height.toFixed(1)),
      delay: Math.round(between(0, 220)),
      duration: Math.round(between(2600, 4400)),
    })
  }

  return particles
}

// lifetime is how long a burst takes to land, in ms: its last particle's delay and flight.
export function lifetime(particles: readonly Particle[]): number {
  return particles.reduce((longest, p) => Math.max(longest, p.delay + p.duration), 0)
}
