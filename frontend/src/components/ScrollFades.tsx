import type { Edges } from '../lib/useScrollEdges'

// fadeClass is what both fades share: laid over an end of the scroller, over its content
// (z-5, which a card or row made relative would otherwise paint over) but under the menus
// and popovers that open from it (z-20 and up), taking no clicks, and easing in and out as
// the end comes and goes.
const fadeClass = 'pointer-events-none absolute inset-x-0 z-5 transition-opacity duration-150'

// ScrollFades fades a scroller's content into the page's background at each end with more
// beyond it (useScrollEdges), so a list cut off by the window shows that it goes on. It is
// laid over the scroller, so it goes beside it in a positioned (relative) parent.
export function ScrollFades({ above, below }: Edges) {
  return (
    <>
      <div
        aria-hidden="true"
        data-scroll-fade="above"
        data-shown={above || undefined}
        className={`${fadeClass} top-0 h-8 bg-linear-to-b from-background to-transparent ${above ? 'opacity-100' : 'opacity-0'}`}
      />
      <div
        aria-hidden="true"
        data-scroll-fade="below"
        data-shown={below || undefined}
        className={`${fadeClass} bottom-0 h-12 bg-linear-to-t from-background to-transparent ${below ? 'opacity-100' : 'opacity-0'}`}
      />
    </>
  )
}
