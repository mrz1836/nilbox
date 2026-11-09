import { useId } from 'react'

import { brand } from '../generated/brand'

const { width, height, path, highlight } = brand.mark

export interface LogoProps {
  className?: string
}

// Logo is the product's logo: the mark beside the lowercase wordmark. Everywhere the
// product names itself as a title (the toolbar, the About page) it uses this, never the
// bare name. It sizes with the font size its className sets, so the mark and wordmark
// keep the same proportions at every size. Its accessible name is the product name.
export function Logo({ className = 'text-base' }: LogoProps) {
  return (
    <span className={`inline-flex items-center gap-[0.5em] leading-none ${className}`}>
      <LogoMark className="h-[1.125em] w-auto" />
      <span className="font-bold tracking-[-0.03em] lowercase">{brand.name}</span>
    </span>
  )
}

// LogoMark is the product's mark, the empty-set sign drawn as a box, in the theme's
// accent warming toward the mark's highlight at the top, as the app icon's glyph does. It
// is decorative: the wordmark beside it carries the name.
export function LogoMark({ className = 'h-[1.125rem] w-auto' }: LogoProps) {
  const fill = `${useId()}mark-fill`

  return (
    <svg aria-hidden="true" viewBox={`0 0 ${String(width)} ${String(height)}`} className={className}>
      <defs>
        <linearGradient id={fill} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" style={{ stopColor: `color-mix(in oklab, var(--color-accent) 60%, ${highlight})` }} />
          <stop offset="1" style={{ stopColor: 'var(--color-accent)' }} />
        </linearGradient>
      </defs>
      <path d={path} fill={`url(#${fill})`} />
    </svg>
  )
}
