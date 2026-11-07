// Motion preferences shared by everything that animates in script: the inbox nil party
// and the theme switch. CSS animations ask the same question with motion-safe: variants.

// reducedMotionQuery matches when the system asks for less motion.
const reducedMotionQuery = '(prefers-reduced-motion: reduce)'

// prefersReducedMotion reports whether the system asks for less motion, in which case
// nothing should move. Without matchMedia it reports false.
export function prefersReducedMotion(win: Window = window): boolean {
  return typeof win.matchMedia === 'function' && win.matchMedia(reducedMotionQuery).matches
}
