// The DOM as a test compares two renders of it: one written as a component draws it today,
// the other through the shared component that takes its place.

// idAttributes hold an element's ID or point at others. useId makes them differ from one
// render to the next, so domShape numbers them by where each first appears.
const idAttributes = new Set(['id', 'for', 'aria-controls', 'aria-describedby', 'aria-labelledby'])

// domShape writes root and everything in it one node per line, indented by depth: each
// element's tag and attributes, sorted by name, its classes sorted and its IDs numbered,
// and each run of text, quoted. Two renders read alike when they differ only in the order
// of classes or attributes, in their IDs, or in how a run of text splits into nodes, none
// of which the window shows.
export function domShape(root: Element): string {
  const ids = new Map<string, string>()
  const number = (value: string) =>
    value
      .split(/\s+/)
      .filter((v) => v !== '')
      .map((v) => {
        const known = ids.get(v) ?? `id-${String(ids.size + 1)}`

        ids.set(v, known)

        return known
      })
      .join(' ')

  const attribute = ({ name, value }: Attr) => {
    if (name === 'class') {
      return `class="${value
        .split(/\s+/)
        .filter((c) => c !== '')
        .sort()
        .join(' ')}"`
    }

    return `${name}="${idAttributes.has(name) ? number(value) : value}"`
  }

  const lines: string[] = []

  const walk = (el: Element, depth: number) => {
    const indent = '  '.repeat(depth)
    const attrs = [...el.attributes].sort((a, b) => a.name.localeCompare(b.name)).map(attribute)
    let text = ''

    lines.push(`${indent}<${[el.tagName.toLowerCase(), ...attrs].join(' ')}>`)

    const flush = () => {
      if (text !== '') {
        lines.push(`${indent}  ${JSON.stringify(text)}`)
        text = ''
      }
    }

    for (const child of el.childNodes) {
      if (child instanceof Element) {
        flush()
        walk(child, depth + 1)
      } else if (child.nodeType === Node.TEXT_NODE) {
        text += child.textContent ?? ''
      }
    }

    flush()
  }

  walk(root, 0)

  return lines.join('\n')
}
