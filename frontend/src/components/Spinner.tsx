// Spinner marks work under way: an accent arc turning on a faint ring. It is decorative,
// so the text beside it says what is happening; with reduced motion it holds still.
export function Spinner({ className = 'size-4' }: { className?: string }) {
  return (
    <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" className={`shrink-0 text-accent ${className}`}>
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.2" strokeWidth="2.5" />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        strokeWidth="2.5"
        strokeLinecap="round"
        className="origin-center motion-safe:animate-spin"
      />
    </svg>
  )
}
