// caretPosition finds where a character of a textarea's text is drawn, so a popup can
// open beside the caret. It copies the textarea's text up to the character into a hidden
// element styled the same way, the standard technique, since browsers do not report the
// caret's position in a textarea.

// mirroredStyles are the properties that decide where text wraps and lands.
const mirroredStyles = [
  'boxSizing',
  'width',
  'paddingTop',
  'paddingRight',
  'paddingBottom',
  'paddingLeft',
  'borderTopWidth',
  'borderRightWidth',
  'borderBottomWidth',
  'borderLeftWidth',
  'fontFamily',
  'fontSize',
  'fontWeight',
  'fontStyle',
  'letterSpacing',
  'lineHeight',
  'tabSize',
  'textIndent',
  'textTransform',
  'wordSpacing',
] as const

export interface CaretPosition {
  // top and left place the character's top-left corner relative to the textarea's
  // padding box, after its scrolling; height is a line's height.
  top: number
  left: number
  height: number
}

export function caretPosition(textarea: HTMLTextAreaElement, index: number): CaretPosition {
  const style = window.getComputedStyle(textarea)
  const mirror = document.createElement('div')

  for (const prop of mirroredStyles) {
    mirror.style[prop] = style[prop]
  }

  mirror.style.position = 'absolute'
  mirror.style.visibility = 'hidden'
  mirror.style.whiteSpace = 'pre-wrap'
  mirror.style.overflowWrap = 'break-word'
  mirror.style.top = '0'
  mirror.style.left = '-9999px'
  mirror.textContent = textarea.value.slice(0, index)

  const marker = document.createElement('span')
  marker.textContent = textarea.value.slice(index, index + 1) || '.'
  mirror.appendChild(marker)

  document.body.appendChild(mirror)

  const lineHeight = Number.parseFloat(style.lineHeight)
  const position = {
    top: marker.offsetTop - textarea.scrollTop,
    left: marker.offsetLeft - textarea.scrollLeft,
    height: Number.isFinite(lineHeight) ? lineHeight : marker.offsetHeight,
  }

  mirror.remove()

  return position
}
