// byPointer says the user's last press was a pointer's (a click or a tap) rather than a
// key's. The window's capture phase hears each press before any handler of it runs, such
// as one that closes a menu, so it is current by the time focus goes back.
let byPointer = false

window.addEventListener(
  'pointerdown',
  () => {
    byPointer = true
  },
  true,
)

window.addEventListener(
  'keydown',
  () => {
    byPointer = false
  },
  true,
)

// focusBack gives focus back to el, the button a menu, panel, or dialog opened from, as it
// closes, so the keyboard carries on from there. After a key, el shows its focus ring, as
// the browser draws it. After a pointer, it shows none (focusVisible false), so the button
// isn't left ringed, nor its card's hover controls lit (card-lit in index.css), once the
// pointer moves on. WebKit rings any focus a script moves, and a closing <dialog> has
// already moved focus back itself, ringed, where a second focus() would change nothing;
// so focus is let go first, and moved again without the ring.
export function focusBack(el: HTMLElement): void {
  if (!byPointer) {
    el.focus()

    return
  }

  const held = el.ownerDocument.activeElement

  if (held instanceof HTMLElement) {
    held.blur()
  }

  el.focus({ focusVisible: false })
}
