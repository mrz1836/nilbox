// remToPx converts a length in rem to CSS pixels at the root's font size now, for the
// few lengths a script must know in pixels; they then follow the text size (see
// textSize.ts) like the stylesheet's own.
export function remToPx(rem: number, doc: Document = document): number {
  const view = doc.defaultView
  const size = view === null ? Number.NaN : Number.parseFloat(view.getComputedStyle(doc.documentElement).fontSize)

  return rem * (Number.isFinite(size) && size > 0 ? size : 16)
}
