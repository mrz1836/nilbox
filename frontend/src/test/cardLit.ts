// cardLitAway is index.css's card-lit selector without its :hover, which jsdom doesn't
// follow: what lights a card with the pointer off it, keyboard focus in it (:focus-visible)
// or a menu or panel it opened open.
export const cardLitAway = ':has(:focus-visible), :has([aria-haspopup][aria-expanded="true"])'

// shownAway reports whether a card's hover controls (its triage, or a compact card's hover
// row) show with the pointer off the card, by the variants they carry: card-lit while the
// card is lit; group-focus-within, as the cards once had it, while any focus is in it,
// even the focus a click leaves; or opacity-100 while the card is busy.
export function shownAway(row: HTMLElement): boolean {
  const controls = row.querySelector('[data-triage], [data-hover-row]')

  if (controls === null) {
    throw new Error('the card has no hover controls')
  }

  const has = (name: string) => controls.classList.contains(name)

  return (
    has('opacity-100') ||
    (has('card-lit:opacity-100') && row.matches(cardLitAway)) ||
    (has('group-focus-within:opacity-100') && row.matches(':focus-within'))
  )
}
