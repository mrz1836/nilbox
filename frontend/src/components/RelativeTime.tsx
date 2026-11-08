import { useNowAs } from '../lib/useNow'

export interface RelativeTimeProps {
  // iso is the time shown, which also goes in the element's dateTime.
  iso: string
  // show words the time against now, such as formatAge or formatAgo.
  show: (now: Date, iso: string) => string
  className?: string
}

// RelativeTime shows a time against the window's clock ("5m", "3h ago"), kept current as
// the clock moves on. It renders again only when its words change, and nothing around it
// renders for a tick.
export function RelativeTime({ iso, show, className }: RelativeTimeProps) {
  const text = useNowAs((now) => show(now, iso))

  return (
    <time dateTime={iso} className={className}>
      {text}
    </time>
  )
}
