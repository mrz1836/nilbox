// BetaBadge marks a part of the app still being built, as Autopilot is while Autopilot
// (beta) is on in Settings → Labs: a small pill in the accent's tint, beside the name it
// marks, never inside it, so the name stays what a control or a heading is called.
export function BetaBadge({ className = '' }: { className?: string }) {
  return (
    <span
      title="Beta: still being built"
      className={`inline-flex shrink-0 items-center rounded-full border border-accent/40 bg-accent/10 px-1.5 text-badge leading-4 font-semibold tracking-wide text-accent uppercase ${className}`.trim()}
    >
      Beta
    </span>
  )
}
