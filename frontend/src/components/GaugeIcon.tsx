// The gauge's geometry on a 24-unit grid: a half dial centered low in the box.
const cx = 12
const cy = 15
const radius = 8
const needle = 5

export interface GaugeIconProps {
  // percent is the share left, 0–100; the needle points left at empty, right at full.
  percent: number
  // known is false before the first reading, which draws an empty dial.
  known: boolean
  className?: string
}

// point is the spot at length along the dial's angle for a share left.
function point(share: number, length: number): [number, number] {
  const angle = Math.PI * (1 - share)

  return [cx + length * Math.cos(angle), cy - length * Math.sin(angle)]
}

// round keeps the path data short and stable.
function round(n: number): string {
  return String(Math.round(n * 100) / 100)
}

// GaugeIcon is a small dial in the current text color: a faint track, the filled share
// left, and a needle. The needle's angle carries the level as well as the color does,
// so the signal does not depend on telling colors apart.
export function GaugeIcon({ percent, known, className = 'size-5' }: GaugeIconProps) {
  const share = Math.min(1, Math.max(0, percent / 100))
  const [ax, ay] = point(share, radius)
  const [nx, ny] = point(share, needle)
  const start = `M ${String(cx - radius)} ${String(cy)}`

  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      className={className}
    >
      <path
        d={`${start} A ${String(radius)} ${String(radius)} 0 0 1 ${String(cx + radius)} ${String(cy)}`}
        opacity={0.3}
      />
      {known && share > 0 && (
        <path data-part="fill" d={`${start} A ${String(radius)} ${String(radius)} 0 0 1 ${round(ax)} ${round(ay)}`} />
      )}
      {known && <line data-part="needle" x1={cx} y1={cy} x2={round(nx)} y2={round(ny)} strokeWidth={1.75} />}
      <circle cx={cx} cy={cy} r={1.6} fill="currentColor" stroke="none" />
    </svg>
  )
}
