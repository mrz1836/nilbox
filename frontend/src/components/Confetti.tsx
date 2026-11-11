import { useEffect, useEffectEvent, useRef, useState, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'

import { brand } from '../generated/brand'
import { burst, lifetime, type Particle, type Room } from '../lib/confetti'

// Point is a spot in the window, in px from its top left corner.
export interface Point {
  x: number
  y: number
}

// ConfettiBurst is one throw of confetti from a point. flash lights the window up from
// that point as it goes off.
export interface ConfettiBurst {
  id: number
  origin: Point
  particles: Particle[]
  flash: boolean
}

// settleMs is how long a burst stays after its last particle lands, so the fade finishes.
const settleMs = 150

// roomAround measures the window around a point.
function roomAround({ y }: Point): Room {
  return { above: y, below: window.innerHeight - y, width: window.innerWidth }
}

// useConfetti holds the bursts in flight. fire throws one from a point, sized to the
// window; a burst removes itself once it lands.
export function useConfetti(random: () => number = Math.random) {
  const [bursts, setBursts] = useState<ConfettiBurst[]>([])
  const seq = useRef(0)

  const fire = (origin: Point, count: number, flash = false) => {
    seq.current += 1
    const next: ConfettiBurst = {
      id: seq.current,
      origin,
      particles: burst(count, roomAround(origin), random),
      flash,
    }

    setBursts((current) => [...current, next])
  }

  const land = (id: number) => {
    setBursts((current) => current.filter((b) => b.id !== id))
  }

  return { bursts, fire, land }
}

// particleStyle hands a particle's flight to the stylesheet as custom properties.
function particleStyle(p: Particle): CSSProperties {
  return {
    '--x': `${String(p.x)}px`,
    '--peak': `${String(p.peak)}px`,
    '--fall': `${String(p.fall)}px`,
    '--spin': `${String(p.spin)}deg`,
    '--ax': String(p.ax),
    '--ay': String(p.ay),
    '--delay': `${String(p.delay)}ms`,
    '--dur': `${String(p.duration)}ms`,
  } as CSSProperties
}

// shapeStyle sizes and colors a particle: a glyph is colored text, every other kind a
// colored shape.
function shapeStyle(p: Particle): CSSProperties {
  return p.kind === 'glyph'
    ? { color: p.color, fontSize: `${String(p.height)}px` }
    : { backgroundColor: p.color, width: `${String(p.width)}px`, height: `${String(p.height)}px` }
}

interface BurstViewProps {
  burst: ConfettiBurst
  onLand: (id: number) => void
}

// BurstView draws one burst and removes it once its last particle has landed.
function BurstView({ burst: b, onLand }: BurstViewProps) {
  // The burst lands with the latest onLand, so a new one never restarts its flight.
  const landed = useEffectEvent(() => {
    onLand(b.id)
  })

  useEffect(() => {
    const timer = setTimeout(
      () => {
        landed()
      },
      lifetime(b.particles) + settleMs,
    )

    return () => {
      clearTimeout(timer)
    }
  }, [b])

  const { x, y } = b.origin

  return (
    <>
      {b.flash && (
        <span
          data-testid="confetti-flash"
          className="nil-flash"
          style={{ '--fx': `${String(x)}px`, '--fy': `${String(y)}px` } as CSSProperties}
        />
      )}
      <span className="absolute" style={{ left: x, top: y }}>
        {b.particles.map((p) => (
          <span key={p.id} data-testid="confetti" data-kind={p.kind} className="nil-confetti" style={particleStyle(p)}>
            <span>
              <span className={`nil-confetti-${p.kind}`} style={shapeStyle(p)}>
                {p.glyph}
              </span>
            </span>
          </span>
        ))}
      </span>
    </>
  )
}

export interface ConfettiProps {
  bursts: readonly ConfettiBurst[]
  onLand: (id: number) => void
  // container holds the layer: the body by default, or a modal dialog, which the browser
  // draws above everything on the body, so confetti thrown over it holds its layer there.
  container?: Element | null
}

// Confetti draws the bursts in flight over the whole window, toolbar included, from a
// layer on the body that takes no clicks. It is decorative, so assistive technology skips it.
export function Confetti({ bursts, onLand, container = null }: ConfettiProps) {
  if (bursts.length === 0) {
    return null
  }

  return createPortal(
    <div
      aria-hidden="true"
      className="pointer-events-none fixed inset-0 z-[60] overflow-hidden"
      style={{ '--nil-highlight': brand.mark.highlight } as CSSProperties}
    >
      {bursts.map((b) => (
        <BurstView key={b.id} burst={b} onLand={onLand} />
      ))}
    </div>,
    container ?? document.body,
  )
}
