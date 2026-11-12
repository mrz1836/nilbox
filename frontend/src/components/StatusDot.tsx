export interface StatusDotProps {
  // color is the dot's background class, as in "bg-warning", spelled out in full so
  // Tailwind finds it.
  color: string
  // ping swells a ring slowly out of the dot, for something at work.
  ping?: boolean
  // pulse pulses the dot, for something starting.
  pulse?: boolean
}

// StatusDot is a small status dot: a ring swells slowly out of it while what it marks is
// at work, it pulses while that starts, and it holds still otherwise, as it does everywhere
// with reduced motion. A working session and a running check share it, so work under way
// looks alike across the app. It sits in a row or in a line of text, centered on the text.
// It is decorative: the text beside it says the state. While it moves it carries
// data-moving, which keeps the inbox card it is on rendering (index.css).
export function StatusDot({ color, ping = false, pulse = false }: StatusDotProps) {
  return (
    <span
      aria-hidden="true"
      data-moving={ping || pulse || undefined}
      className="relative inline-flex size-2 shrink-0 items-center justify-center align-middle"
    >
      {ping && (
        <span className={`absolute inset-0 rounded-full ${color} [animation-duration:2.2s] motion-safe:animate-ping`} />
      )}
      <span className={`relative size-2 rounded-full ${color} ${pulse ? 'motion-safe:animate-pulse' : ''}`} />
    </span>
  )
}
