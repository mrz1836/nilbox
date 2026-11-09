import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { MAX_LENGTH, renderMarkdown } from './markdown'

// show renders text, and returns its container and the openLink spy it was given.
function show(text: string) {
  const openLink = vi.fn<(url: string) => void>()
  const { container } = render(<article>{renderMarkdown(text, { openLink })}</article>)

  return { root: container.firstElementChild as HTMLElement, openLink }
}

// tags lists the element names of root's children, in order.
function tags(root: Element): string[] {
  return [...root.children].map((child) => child.tagName.toLowerCase())
}

// textOf returns an element's text, with each line break as a newline.
function textOf(element: Element | null | undefined): string {
  const copy = element?.cloneNode(true)

  if (!(copy instanceof Element)) {
    return ''
  }

  for (const br of copy.querySelectorAll('br')) {
    br.replaceWith('\n')
  }

  return copy.textContent
}

// expectInert asserts that nothing under root can load or run anything.
function expectInert(root: Element) {
  expect(root.querySelector('script,iframe,style,img,object,embed,video,audio,source,link,svg,a')).toBeNull()

  for (const element of root.querySelectorAll('*')) {
    for (const attribute of element.getAttributeNames()) {
      expect(['class', 'type', 'title', 'aria-hidden', 'aria-label', 'disabled', 'checked', 'start', 'open']).toContain(
        attribute,
      )
    }
  }
}

describe('renderMarkdown blocks', () => {
  it('renders ATX headings, small, with closing #s dropped', () => {
    const { root } = show(
      '# One\n## Two ##\n### Three\n#### C#\n##### Five\n###### Six\n#\n# ##\n####### Seven\n#Eight',
    )

    expect(tags(root)).toEqual(['h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'h1', 'h1', 'p'])
    expect(screen.getByRole('heading', { level: 1, name: 'One' })).toHaveClass('font-semibold', 'text-[0.9375rem]')
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent(/^Two$/)
    expect(screen.getByRole('heading', { level: 4 })).toHaveTextContent(/^C#$/)
    expect(screen.getByRole('heading', { level: 6 })).toHaveClass('text-muted')
    expect(root.children[6]).toHaveTextContent(/^$/)
    expect(root.children[7]).toHaveTextContent(/^$/)
    expect(textOf(root.children[8])).toBe('####### Seven\n#Eight')
  })

  it('renders setext headings', () => {
    const { root } = show('Title\n===\n\nSub\ntitle\n---')

    expect(tags(root)).toEqual(['h1', 'h2'])
    expect(root.children[1]?.querySelector('br')).not.toBeNull()
  })

  it('breaks lines inside a paragraph, as GitHub does, and separates paragraphs at blank lines', () => {
    const { root } = show('one  \ntwo\\\nthree\n\nfour\\')

    expect(tags(root)).toEqual(['p', 'p'])
    expect(root.children[0]?.querySelectorAll('br')).toHaveLength(2)
    expect(textOf(root.children[0])).toBe('one\ntwo\nthree')
    expect(root.children[1]).toHaveTextContent(/^four\\$/)
  })

  it('continues a paragraph on a line indented as code', () => {
    const { root } = show('text\n    indented')

    expect(tags(root)).toEqual(['p'])
    expect(textOf(root.children[0])).toBe('text\nindented')
  })

  it('reads CRLF and CR line ends and tabs', () => {
    const { root } = show('# Title\r\n\r\nline\rnext\n\n\tcode\n\n-\titem')

    expect(tags(root)).toEqual(['h1', 'p', 'pre', 'ul'])
    expect(textOf(root.children[1])).toBe('line\nnext')
    expect(root.querySelector('li')).toHaveTextContent(/^item$/)
    expect(root.querySelector('pre')).toHaveTextContent(/^code$/)
  })

  it('renders bullet lists, nested by indentation', () => {
    const { root } = show('- one\n  - nested\n    - deeper\n- two\n* star\n+ plus')

    expect(tags(root)).toEqual(['ul', 'ul', 'ul'])

    const outer = root.children[0] as HTMLElement
    expect(outer).toHaveClass('list-disc', 'pl-5')
    expect(within(outer).getAllByRole('listitem')).toHaveLength(4)
    expect(outer.querySelector('ul ul ul')).toHaveTextContent(/^deeper$/)
    expect(root.children[1]).toHaveTextContent(/^star$/)
    expect(root.children[2]).toHaveTextContent(/^plus$/)
  })

  it('renders ordered lists from their first number, and a new list at a new delimiter', () => {
    const { root } = show('3. three\n4. four\n\n1) one\n2) two')

    expect(tags(root)).toEqual(['ol', 'ol'])
    expect(root.children[0]).toHaveAttribute('start', '3')
    expect(root.children[0]).toHaveClass('list-decimal')
    expect(root.children[1]).toHaveAttribute('start', '1')
    expect(within(root.children[1] as HTMLElement).getAllByRole('listitem')).toHaveLength(2)
  })

  it('keeps loose items, lazy lines, empty items, and code inside items', () => {
    const { root } = show('- first\nlazy\n\n- second\n\n  more\n-\n-      code\n- last\n\nafter')

    expect(tags(root)).toEqual(['ul', 'p'])

    const items = within(root.children[0] as HTMLElement).getAllByRole('listitem')
    expect(items).toHaveLength(5)
    expect(textOf(items[0])).toBe('first\nlazy')
    expect(items[1]?.querySelectorAll('p')).toHaveLength(2)
    expect(items[2]).toHaveTextContent(/^$/)
    expect(items[3]?.querySelector('pre')?.textContent).toBe(' code')
    expect(root.children[1]).toHaveTextContent(/^after$/)
  })

  it('ends a list at a rule, and lets only a list from 1 interrupt a paragraph', () => {
    const { root } = show('- a\n* * *\ntext\n2. not a list\n\ntext\n1. a list\n\ntext\n-\nmore')

    expect(tags(root)).toEqual(['ul', 'hr', 'p', 'p', 'ol', 'h2', 'p'])
    expect(textOf(root.children[2])).toBe('text\n2. not a list')
  })

  it('ends an empty item at an unindented line', () => {
    const { root } = show('-\n  inside\n-\noutside')

    expect(tags(root)).toEqual(['ul', 'p'])
    expect(within(root.children[0] as HTMLElement).getAllByRole('listitem')[0]).toHaveTextContent(/^inside$/)
    expect(root.children[1]).toHaveTextContent(/^outside$/)
  })

  it('renders task items as disabled checkboxes, without bullets', () => {
    const { root } = show('- [ ] todo\n- [x] done\n- [X] also done\n- [ ]\n- [y] not a task')

    const boxes = screen.getAllByRole('checkbox')
    expect(boxes.map((box) => (box as HTMLInputElement).checked)).toEqual([false, true, true, false])
    expect(boxes[0]).toBeDisabled()
    expect(boxes[0]).toHaveAccessibleName('Not done')
    expect(boxes[1]).toHaveAccessibleName('Done')
    expect(boxes[0]).toHaveClass('checkbox')
    expect(boxes[0]?.closest('li')).toHaveClass('list-none')
    expect(boxes[0]?.closest('li')).toHaveTextContent(/^todo$/)
    expect(within(root).getAllByRole('listitem')[4]).toHaveTextContent('[y] not a task')
  })

  it('renders fenced code blocks, with or without a language, closed or not', () => {
    const { root } = show(
      '```ts\nconst a = 1\n\n  indented\n```\n~~~\ntilde\n````\n~~~\n  ```\n  fenced\n    deeper\n ```\n```\nopen\n# not a heading',
    )

    const blocks = root.querySelectorAll('pre')
    expect(blocks).toHaveLength(4)
    expect(blocks[0]?.textContent).toBe('const a = 1\n\n  indented')
    expect(blocks[0]).toHaveClass('font-mono', 'overflow-x-auto', 'bg-raised', 'border-border')
    expect(blocks[1]?.textContent).toBe('tilde\n````')
    expect(blocks[2]?.textContent).toBe('fenced\n  deeper')
    expect(blocks[3]?.textContent).toBe('open\n# not a heading')
    expect(root.querySelector('h1')).toBeNull()
  })

  it('reads ``` with a backtick in its info as text, not a fence', () => {
    const { root } = show('``` a`b\ntext')

    expect(tags(root)).toEqual(['p'])
  })

  it('renders indented code blocks', () => {
    const { root } = show('    line one\n\n    line two\n  \n\ntext')

    expect(tags(root)).toEqual(['pre', 'p'])
    expect(root.querySelector('pre')?.textContent).toBe('line one\n\nline two')
  })

  it('renders blockquotes with nested blocks and lazy lines', () => {
    const { root } = show('> quoted **bold**\nlazy\n> > nested\n>\n> - item\n\noutside')

    expect(tags(root)).toEqual(['blockquote', 'p'])

    const quote = root.children[0] as HTMLElement
    expect(quote).toHaveClass('border-l-2', 'border-muted/50', 'text-muted')
    expect(tags(quote)).toEqual(['p', 'blockquote', 'ul'])
    expect(textOf(quote.children[0])).toBe('quoted bold\nlazy')
    expect(quote.querySelector('strong')).toHaveTextContent('bold')
  })

  it('ends a quote at a block that a lazy line would start', () => {
    const { root } = show('> quoted\n# heading')

    expect(tags(root)).toEqual(['blockquote', 'h1'])
  })

  it('renders horizontal rules', () => {
    const { root } = show('a\n\n---\n\n***\n\n_ _ _')

    expect(tags(root)).toEqual(['p', 'hr', 'hr', 'hr'])
  })

  it('renders tables with alignment and inline content', () => {
    const { root } = show(
      'intro\n| Left | Center | Right | None |\n|:-----|:------:|------:|------|\n| `a\\|b` | **b** | c | d | extra |\n| short |\n\nafter',
    )

    expect(tags(root)).toEqual(['p', 'div', 'p'])
    expect(root.children[1]).toHaveClass('overflow-x-auto')

    const table = within(screen.getByRole('table'))
    const heads = table.getAllByRole('columnheader')
    expect(heads.map((head) => head.textContent)).toEqual(['Left', 'Center', 'Right', 'None'])
    expect(heads.map((head) => head.className.split(' ').at(-1))).toEqual([
      'text-left',
      'text-center',
      'text-right',
      'text-left',
    ])
    expect(heads[0]).toHaveClass('border', 'border-border', 'font-semibold')

    const rows = table.getAllByRole('row')
    expect(rows).toHaveLength(3)

    const first = within(rows[1] as HTMLElement).getAllByRole('cell')
    expect(first.map((cell) => cell.textContent)).toEqual(['a|b', 'b', 'c', 'd'])
    expect(first[0]?.querySelector('code')).not.toBeNull()
    expect(first[1]?.querySelector('strong')).not.toBeNull()
    expect(
      within(rows[2] as HTMLElement)
        .getAllByRole('cell')
        .map((cell) => cell.textContent),
    ).toEqual(['short', '', '', ''])
  })

  it('ends a table at a block, and reads text that is no table as a paragraph', () => {
    const { root } = show(
      'a | b\n--- | ---\n1 | 2\n> quote\n\nx | y\n--- | --- | ---\n\nx | y\n-- -- | --\n\nx | y\n---',
    )

    expect(tags(root)).toEqual(['div', 'blockquote', 'p', 'p', 'h2'])
    expect(screen.getAllByRole('row')).toHaveLength(2)
  })
})

describe('renderMarkdown details', () => {
  it('renders <details> natively, with its summary and the markdown inside it', () => {
    const { root } = show('<details>\n<summary>Release **notes**</summary>\n\n- one\n- two\n\n</details>\nafter')

    expect(tags(root)).toEqual(['details', 'p'])

    const details = root.children[0] as HTMLDetailsElement
    expect(details.open).toBe(false)
    expect(details).toHaveClass('rounded-md', 'border-border')
    expect(details.querySelector('summary')).toHaveTextContent(/^Release notes$/)
    expect(details.querySelector('summary strong')).toHaveTextContent('notes')
    expect(within(details).getAllByRole('listitem', { hidden: true })).toHaveLength(2)
    expect(root.children[1]).toHaveTextContent(/^after$/)
  })

  it('reads one on a single line, open, and keeps the text after it', () => {
    const { root } = show('<details open><summary>Hi</summary>Body text</details> tail')

    expect(tags(root)).toEqual(['details', 'p'])
    expect((root.children[0] as HTMLDetailsElement).open).toBe(true)
    expect(root.querySelector('summary')).toHaveTextContent(/^Hi$/)
    expect(root.querySelector('details p')).toHaveTextContent(/^Body text$/)
    expect(root.children[1]).toHaveTextContent(/^tail$/)
  })

  it('reads "Details" without a summary, and the rest as the summary when it never closes', () => {
    const { root } = show('<details>\nJust text\n</details>\n\n<details><summary>Never closed\nmore')

    const summaries = [...root.querySelectorAll('summary')].map((summary) => summary.textContent)
    expect(summaries).toEqual(['Details', 'Never closed more'])
    expect(root.querySelector('details p')).toHaveTextContent(/^Just text$/)
  })

  it('nests details inside details', () => {
    const { root } = show(
      '<details><summary>Outer</summary>\n<details><summary>Inner</summary>\ninner text\n</details>\nouter text\n</details>',
    )

    expect(tags(root)).toEqual(['details'])

    const inner = root.querySelector('details details') as HTMLElement
    expect(within(inner).getByText('Inner')).toBeInTheDocument()
    expect(within(inner).getByText('inner text')).toBeInTheDocument()
    expect(inner.nextElementSibling).toHaveTextContent(/^outer text$/)
  })
})

describe('renderMarkdown HTML', () => {
  it('drops comments, on one line or several', () => {
    const { root } = show(
      '<!-- hidden -->\ntext\n\n<!--\nmulti\nline\n-->\n\nafter <!-- inline --> end\n\nopen <!-- never <!-- closed',
    )

    expect(tags(root)).toEqual(['p', 'p', 'p'])
    expect(root.children[2]).toHaveTextContent(/^open <!-- never <!-- closed$/)
    expect(root).not.toHaveTextContent(/hidden|multi|inline/)
    expect(root.children[1]).toHaveTextContent(/^after end$/)
  })

  it('keeps the text of tags it drops, and the meaning of tags it knows', () => {
    const { root } = show(
      '<p align="center">\n  <img src="https://example.com/logo.png" alt="Logo" width="100">\n</p>\n\n<div>\n<b>bold</b> &amp; <i>it</i><br>\n<span style="color:red">next</span></div>',
    )

    expect(tags(root)).toEqual(['div', 'div'])
    expect(root.querySelector('p')).toContainElement(screen.getByRole('button', { name: 'Logo' }))
    expect(root.querySelector('strong')).toHaveTextContent('bold')
    expect(root.querySelector('em')).toHaveTextContent('it')
    expect(root.querySelector('br')).not.toBeNull()
    expect(root.children[1]).toHaveTextContent('bold & it next')
    expectInert(root)
  })

  it('renders a <pre> block as code, keeping its spacing', () => {
    const { root } = show('<pre>\nline  one\n  two\n</pre>\nafter')

    expect(tags(root)).toEqual(['div', 'p'])
    expect(root.querySelector('pre')).toHaveClass('font-mono')
    expect(root.querySelector('pre')?.textContent).toBe('\nline  one\n  two\n')
  })

  it('renders a lone tag as an HTML block, but lets it continue a paragraph', () => {
    const { root } = show('<img src="https://example.com/a.png" alt="Diagram">\n\ntext\n<span>\nmore')

    expect(tags(root)).toEqual(['div', 'p'])
    expect(screen.getByRole('button', { name: 'Diagram' })).toHaveAttribute('title', 'https://example.com/a.png')
    expect(root.children[1]).toHaveTextContent('textmore')
  })

  it('drops the rest of a block after a comment left open, and keeps a stray <', () => {
    const { root } = show('<div>\na < b <!-- drop\nthis\n\nshown')

    expect(root.children[0]).toHaveTextContent(/^a < b$/)
    expect(root.children[1]).toHaveTextContent(/^shown$/)
  })

  it('builds lists, tables, and definition lists, closing tags as a browser would', () => {
    const { root } = show(
      '<ul>\n<li>one\n<li>two</li>\n</ul>\n<table>\n<tr><th>H</th></tr>\n<tr><td>C<td>D</td></tr>\n</table>\n<p>first<p>second</p></span>\n<dl><dt>Term<dd>Definition</dl>\n<div>a<hr>b</br></div>',
    )

    const items = within(root.querySelector('ul') as HTMLElement).getAllByRole('listitem')
    expect(items.map((item) => item.textContent)).toEqual(['one\n', 'two'])
    expect(root.querySelector('ul')?.childNodes).toHaveLength(2)

    const table = screen.getByRole('table')
    expect(table.parentElement).toHaveClass('overflow-x-auto')
    expect(table.querySelector('tbody')).not.toBeNull()
    expect(within(table).getByRole('columnheader')).toHaveTextContent('H')
    expect(
      within(table)
        .getAllByRole('cell')
        .map((cell) => cell.textContent),
    ).toEqual(['C', 'D'])
    expect([...root.querySelectorAll('p')].map((p) => p.textContent)).toEqual(['first', 'second'])
    expect(root.querySelector('div.pl-4')).toHaveTextContent('Definition')
    expect(root.querySelector('hr')).not.toBeNull()
  })

  it('renders inline tags', () => {
    const { root } = show(
      'H<sub>2</sub>O, x<sup>2</sup>, <kbd>Ctrl</kbd>, <code>c</code>, <del>d</del>, <mark>m</mark>, <u>u</u>, <unknown>kept</unknown>',
    )

    for (const tag of ['sub', 'sup', 'kbd', 'code', 'del', 'mark', 'u']) {
      expect(root.querySelector(tag)).not.toBeNull()
    }

    expect(root.querySelector('unknown')).toBeNull()
    expect(root).toHaveTextContent('H2O, x2, Ctrl, c, d, m, u, kept')
  })

  it('makes HTML links that open outside, and leaves the rest as text', async () => {
    const { root, openLink } = show(
      '<div>\n<a href="https://example.com/x">x</a> <a href=\'https://example.com/y\'>y</a> <a href=https://example.com/z>z</a> <a href="javascript:alert(1)">bad</a> <a name="n">none</a> <a href="https://example.com/o"><a href="https://example.com/inner">nested</a></a> <a href="https://example.com/"><img src="https://example.com/i.png" alt="Badge"></a> <img src="javascript:x" alt="Bad"> <img alt="No source"> <img src="https://example.com/no-alt.png"> <details open><summary>S</summary>body</details>\n</div>',
    )

    expect(screen.getAllByRole('button').map((button) => button.textContent)).toEqual([
      'x',
      'y',
      'z',
      'nested',
      '🖼 Badge',
    ])
    expect(screen.getByRole('button', { name: 'nested' })).toHaveAttribute('title', 'https://example.com/o')
    expect(screen.getByRole('button', { name: 'Badge' })).toHaveAttribute('title', 'https://example.com/')
    expect(root).toHaveTextContent('bad none')
    expect(root).toHaveTextContent('🖼 Bad 🖼 No source')
    expect(root.querySelector('details summary')).toHaveTextContent('S')

    await userEvent.click(screen.getByRole('button', { name: 'z' }))

    expect(openLink).toHaveBeenCalledExactlyOnceWith('https://example.com/z')
    expectInert(root)
  })

  it('drops block tags where only inline content may go, keeping their text', () => {
    const { root } = show(
      [
        'text <div>inside</div> <p>para</p> a<hr>b end',
        '',
        '[<div>label</div>](https://example.com/l)',
        '',
        '<div>',
        '<p>one<div>two</div></p><p><b>three<div>four</div><hr></b></p><a href="https://example.com/a"><ul><li>five</li></ul></a>',
        '</div>',
        '',
        '| cell |',
        '| --- |',
        '| <ul><li>six</li></ul> |',
      ].join('\n'),
    )

    expect(root.querySelector('p div, p p, p hr, p ul, button div, button ul, b div, strong div')).toBeNull()
    expect(root.children[0]).toHaveTextContent('text inside para ab end')
    expect(screen.getByRole('button', { name: 'label' })).toHaveAttribute('title', 'https://example.com/l')
    expect(screen.getByText('two').tagName).toBe('DIV')
    expect(screen.getByText('two').previousElementSibling).toHaveTextContent(/^one$/)
    expect(root.querySelector('strong')).toHaveTextContent(/^threefour$/)
    expect(screen.getByRole('button', { name: 'five' })).toBeInTheDocument()
    expect(root.querySelector('td ul li')).toHaveTextContent('six')
  })

  it('caps how deep tags nest', () => {
    const { root } = show(`<div>${'<div>'.repeat(100)}deep${'</div>'.repeat(100)}</div>after`)

    expect(root).toHaveTextContent('deepafter')
    expect(root.querySelectorAll('div').length).toBeLessThan(40)
  })
})

describe('renderMarkdown inline', () => {
  it('renders bold, italic, and strikethrough', () => {
    const { root } = show('**b1** __b2__ *i1* _i2_ ***both*** ~~gone~~ ~one~ ~~~three~~~ ~~ spaced ~~')

    expect([...root.querySelectorAll('strong')].map((strong) => strong.textContent)).toEqual(['b1', 'b2', 'both'])
    expect([...root.querySelectorAll('em')].map((em) => em.textContent)).toEqual(['i1', 'i2', 'both'])
    expect(root.querySelector('em > strong')).toHaveTextContent('both')
    expect(root.querySelector('strong')).toHaveClass('font-semibold')
    expect([...root.querySelectorAll('del')].map((del) => del.textContent)).toEqual(['gone'])
    expect(root).toHaveTextContent('~one~ ~~~three~~~ ~~ spaced ~~')
  })

  it('leaves underscores inside words alone', () => {
    const { root } = show('snake_case_word and _foo_bar_ and __init__')

    expect(root).toHaveTextContent('snake_case_word and foo_bar and init')
    expect(root.querySelector('em')).toHaveTextContent(/^foo_bar$/)
    expect(root.querySelector('strong')).toHaveTextContent(/^init$/)
  })

  it('reads emphasis next to punctuation', () => {
    const { root } = show('*"quoted"* _(a)_. ._(b)_ **a *b* c** ***a** x')

    expect([...root.querySelectorAll('em')].map((em) => em.textContent)).toEqual(['"quoted"', '(a)', '(b)', 'b'])
    expect([...root.querySelectorAll('strong')].map((strong) => strong.textContent)).toEqual(['a b c', 'a'])
    expect(root).toHaveTextContent('*a x')
  })

  it('shows unmatched markers as text, and gives up open markers a closer skips', () => {
    const { root } = show('**open and *also\n\n*a _b* c_\n\n** spaced **\n\n_a *b_ c*')

    const paragraphs = root.querySelectorAll('p')
    expect(paragraphs[0]).toHaveTextContent(/^\*\*open and \*also$/)
    expect(paragraphs[1]?.querySelector('em')).toHaveTextContent(/^a _b$/)
    expect(paragraphs[1]).toHaveTextContent(/^a _b c_$/)
    expect(paragraphs[2]).toHaveTextContent(/^\*\* spaced \*\*$/)
    expect(paragraphs[3]?.querySelector('em')).toHaveTextContent(/^a \*b$/)
  })

  it('caps how many markers stay open', () => {
    const { root } = show(`${'*a '.repeat(100)}end*`)

    expect(root).toHaveTextContent('end')
  })

  it('renders code spans', () => {
    const { root } = show('`code` ``a ` b`` ` padded ` `  ` `multi\nline` `unclosed ``also')

    expect([...root.querySelectorAll('code')].map((code) => code.textContent)).toEqual([
      'code',
      'a ` b',
      'padded',
      '  ',
      'multi line',
    ])
    expect(root.querySelector('code')).toHaveClass('font-mono', 'bg-raised')
    expect(root).toHaveTextContent('`unclosed ``also')
  })

  it('shows a backtick run as text once no run can close it', () => {
    const { root } = show('`a\\``')

    expect(root.querySelector('code')).toBeNull()
    expect(root).toHaveTextContent(/^`a``$/)
  })

  it('reads backslash escapes', () => {
    const { root } = show('\\*not em\\* \\# \\a \\\\ \\&amp; ]')

    expect(root.querySelector('em')).toBeNull()
    expect(root).toHaveTextContent(/^\*not em\* # \\a \\ &amp; ]$/)
  })

  it('decodes character references', () => {
    const { root } = show('&amp; &lt;b&gt; &copy; &#169; &#x1F600; &#X41; &#0; &#xD800; &bogus; & alone')

    expect(root).toHaveTextContent('& <b> © © 😀 A � � &bogus; & alone')
    expect(root.querySelector('b')).toBeNull()
  })
})

describe('renderMarkdown links', () => {
  it('makes http, https, and mailto links that open outside, with their URL as title', async () => {
    const { root, openLink } = show(
      '[Docs](https://example.com/docs "Title") [Mail](mailto:dev@example.com) [Bad](javascript:alert(1)) [Rel](/relative) [Data](data:text/html,x) [Empty]() [Broken](https://)',
    )

    const docs = screen.getByRole('button', { name: 'Docs' })
    expect(docs).toHaveAttribute('title', 'https://example.com/docs')
    expect(docs).toHaveAttribute('type', 'button')
    expect(docs).toHaveClass('text-accent')
    expect(screen.getByRole('button', { name: 'Mail' })).toHaveAttribute('title', 'mailto:dev@example.com')
    expect(screen.getAllByRole('button')).toHaveLength(2)
    expect(root).toHaveTextContent('Bad Rel Data Empty Broken')

    await userEvent.click(docs)

    expect(openLink).toHaveBeenCalledExactlyOnceWith('https://example.com/docs')
  })

  it('stops a click from doing anything else', () => {
    const { openLink } = show('[Docs](https://example.com/docs)')
    const click = new MouseEvent('click', { bubbles: true, cancelable: true })

    screen.getByRole('button', { name: 'Docs' }).dispatchEvent(click)

    expect(click.defaultPrevented).toBe(true)
    expect(openLink).toHaveBeenCalledOnce()
  })

  it('reads autolinks and bare URLs, without trailing punctuation', () => {
    const { root } = show(
      '<https://example.com/auto> <dev@example.com> <javascript:alert(1)>\n\nSee https://example.com/a_b_c. (https://example.com/p(1)) xhttps://example.com/no https://example.com/q?x=1! *https://example.com/e* http:// Here',
    )

    expect(screen.getAllByRole('button').map((button) => button.title)).toEqual([
      'https://example.com/auto',
      'mailto:dev@example.com',
      'https://example.com/a_b_c',
      'https://example.com/p(1)',
      'https://example.com/q?x=1',
      'https://example.com/e',
    ])
    expect(screen.getByRole('button', { name: 'dev@example.com' })).toBeInTheDocument()
    expect(root).toHaveTextContent('javascript:alert(1)')
    expect(root).toHaveTextContent('xhttps://example.com/no')
    expect(root.querySelector('em')).not.toBeNull()
  })

  it('reads link targets in angle brackets, with titles, and with parentheses', () => {
    const { root } = show(
      '[a](<https://example.com/s p>) [b](https://example.com/b \'t\') [c]( https://example.com/c (t) ) [d](https://example.com/(x)) [g](https://example.com/g\\)x) [h](https://example.com/h "a \\" b")',
    )

    expect(screen.getAllByRole('button').map((button) => button.title)).toEqual([
      'https://example.com/s%20p',
      'https://example.com/b',
      'https://example.com/c',
      'https://example.com/(x)',
      'https://example.com/g)x',
      'https://example.com/h',
    ])
    expect(root).toHaveTextContent(/^a b c d g h$/)
  })

  it('shows a link whose target does not close as text', () => {
    const { root } = show(
      '[e](mailto:e@example.com "unclosed) [f](<mailto:f@example.com [i](mailto:i@example.com x) [j](<mailto:j@example.com\nx>)',
    )

    expect(screen.queryByRole('button')).toBeNull()
    expect(root).toHaveTextContent(
      '[e](mailto:e@example.com "unclosed) [f](<mailto:f@example.com [i](mailto:i@example.com x)',
    )
  })

  it('makes no link inside a link', () => {
    show('[outer [inner](https://example.com/in) <https://example.com/auto>](https://example.com/out)')

    const link = screen.getByRole('button')
    expect(link).toHaveAttribute('title', 'https://example.com/out')
    expect(link).toHaveTextContent('outer [inner](https://example.com/in) https://example.com/auto')
  })

  it('resolves reference links and hides their definitions', () => {
    const { root } = show(
      '[Full][ref] [Collapsed][] [ref] [Missing][nope] [ref](not a link) [nope]\n\n[ref]: https://example.com/ref "Title"\n[collapsed]: <https://example.com/c>\n[ref]: https://example.com/second\n[//]: # (dependabot-automerge-start)\n\nafter',
    )

    expect(screen.getAllByRole('button').map((button) => [button.textContent, button.title])).toEqual([
      ['Full', 'https://example.com/ref'],
      ['Collapsed', 'https://example.com/c'],
      ['ref', 'https://example.com/ref'],
      ['ref', 'https://example.com/ref'],
    ])
    expect(tags(root)).toEqual(['p', 'p'])
    expect(root).toHaveTextContent('[Missing][nope]')
    expect(root).toHaveTextContent('(not a link) [nope]')
    expect(root).not.toHaveTextContent('dependabot-automerge')
  })

  it('looks up no reference label longer than CommonMark allows', () => {
    const label = 'x'.repeat(1000)

    show(`[${label}]\n\n[${label.slice(1)}]: https://example.com/long\n[short]: https://example.com/s`)

    expect(screen.queryByRole('button')).toBeNull()
  })
})

describe('renderMarkdown images', () => {
  it('turns markdown images into links labelled with their alt text, never loading them', () => {
    const { root } = show(
      '![Diagram](https://example.com/d.png) ![](https://example.com/e.png) ![Bad](javascript:x) ![x] ![Ref][img] ! end\n\n[img]: https://example.com/r.png',
    )

    expect(root.querySelector('img')).toBeNull()
    expect(screen.getAllByRole('button').map((button) => [button.textContent, button.title])).toEqual([
      ['🖼 Diagram', 'https://example.com/d.png'],
      ['🖼 image', 'https://example.com/e.png'],
      ['🖼 Ref', 'https://example.com/r.png'],
    ])
    expect(screen.getByRole('button', { name: 'Diagram' })).toBeInTheDocument()
    expect(root).toHaveTextContent('🖼 Bad ![x]')
  })

  it('labels a linked image, as a badge is, inside its one link', () => {
    show('[![CI status](https://example.com/badge.svg)](https://example.com/ci)')

    const link = screen.getByRole('button', { name: 'CI status' })
    expect(link).toHaveAttribute('title', 'https://example.com/ci')
    expect(screen.getAllByRole('button')).toHaveLength(1)
  })
})

describe('renderMarkdown safety', () => {
  it('renders no element that loads or runs anything, and passes no attribute through', () => {
    const { root } = show(
      [
        '<script>alert(1)</script>',
        '',
        'text <script>alert(2)</script> more <SCRIPT>alert(3)',
        '',
        '<style>p { color: red }</style>',
        '<iframe src="https://evil.example"></iframe>',
        '<img src=x onerror="alert(4)">',
        '<object data="https://evil.example/x"></object><embed src="https://evil.example/y">',
        '<video src="https://evil.example/v.mp4" poster="https://evil.example/p.png"></video>',
        '<svg><image href="https://evil.example/s.png" /></svg>',
        '<link rel="stylesheet" href="https://evil.example/s.css">',
        '',
        '<div onclick="alert(5)" style="background:url(https://evil.example/b.png)" class="evil" id="x">styled</div>',
        '',
        '<a href="https://example.com/ok" onclick="alert(6)" style="color:red" target="_blank">ok</a>',
        '',
        '![x](https://evil.example/i.png "t") <img src="https://evil.example/j.png" alt="J" onerror="alert(7)">',
        '',
        '[js](javascript:alert(8)) [vb](vbscript:x) [file](file:///etc/passwd) <a href="data:text/html,x">data</a>',
      ].join('\n'),
    )

    expectInert(root)
    expect(root).not.toHaveTextContent('alert')
    expect(root).toHaveTextContent('styled')
    expect(root.querySelector('.evil')).toBeNull()
    expect(screen.getAllByRole('button').map((button) => button.title)).toEqual([
      'https://example.com/ok',
      'https://evil.example/i.png',
      'https://evil.example/j.png',
    ])
    expect(root).toHaveTextContent('js vb file data')
  })

  it('keeps text that looks like HTML as text', () => {
    const { root } = show('a <b onmouseover=alert(1)>b</b> &lt;script&gt;alert(1)&lt;/script&gt; 1 < 2 <3 <a:b>')

    expect(root.querySelector('script')).toBeNull()
    expect(root).toHaveTextContent('a b <script>alert(1)</script> 1 < 2 <3 <a:b>')
    expect(root.querySelector('strong')).not.toHaveAttribute('onmouseover')
  })

  it('cuts text past MAX_LENGTH, and says so', () => {
    const { root } = show('a'.repeat(MAX_LENGTH + 10))

    expect(root.querySelector('p')?.textContent).toHaveLength(MAX_LENGTH)
    expect(root.lastElementChild).toHaveTextContent('… The rest is too long to show here.')
    expect(root.lastElementChild).toHaveClass('text-muted')
  })

  it('renders deeply nested and unbalanced text without throwing', () => {
    const inputs = [
      `${'> '.repeat(5000)}deep`,
      '> '.repeat(40),
      '> '.repeat(13),
      `${'- '.repeat(500)}item`,
      Array.from({ length: 40 }, (_, n) => `${'  '.repeat(n)}- level ${String(n)}`).join('\n'),
      `${'['.repeat(20000)}x${']'.repeat(20000)}`,
      '[a]('.repeat(20000),
      '*'.repeat(50000),
      '_a '.repeat(20000),
      '`'.repeat(10000),
      Array.from({ length: 500 }, (_, n) => '`'.repeat(n + 1)).join(' '),
      '<a x="'.repeat(10000),
      '<!--'.repeat(10000),
      '<details>'.repeat(5000),
      `<a href="https://example.com/">${'<b>'.repeat(5000)}`,
      '&#'.repeat(20000),
      `https://example.com/${')'.repeat(20000)}`,
      `https://example.com/${'.'.repeat(20000)}`,
      '|'.repeat(20000),
      `${'| a '.repeat(2000)}|\n${'|---'.repeat(2000)}|`,
      '\\'.repeat(20001),
      '\t\t\u0000\r\r\n',
    ]

    for (const input of inputs) {
      expect(() => show(input)).not.toThrow()
    }
  })

  it('renders random text without throwing', () => {
    const pieces = [
      '*',
      '_',
      '~',
      '`',
      '[',
      ']',
      '(',
      ')',
      '!',
      '<',
      '>',
      '\\',
      '#',
      '-',
      '|',
      ':',
      '\n',
      ' ',
      'a',
      '1.',
    ]
    const words = [
      '<details>',
      '</details>',
      '<summary>',
      '<b>',
      '</b>',
      'https://x.example',
      '&amp;',
      '```',
      '> ',
      '- [ ] ',
    ]
    let seed = 7
    const next = () => {
      seed = (seed * 1103515245 + 12345) % 2147483648

      return seed
    }

    for (let round = 0; round < 200; round += 1) {
      let text = ''

      for (let n = 0; n < 120; n += 1) {
        const value = next()

        text += value % 5 === 0 ? (words[value % words.length] ?? '') : (pieces[value % pieces.length] ?? '')
      }

      expect(() => show(text)).not.toThrow()
    }
  })
})

describe('renderMarkdown with a Dependabot body', () => {
  const body = [
    'Bumps [left-pad](https://github.com/acme-org/left-pad) from 1.0.0 to 1.1.0.',
    '<details>',
    '<summary>Release notes</summary>',
    '<p><em>Sourced from <a href="https://github.com/acme-org/left-pad/releases">left-pad\'s releases</a>.</em></p>',
    '<blockquote>',
    '<h2>v1.1.0</h2>',
    '<h2>What&#39;s Changed</h2>',
    '<ul>',
    '<li>Pad faster by <a href="https://github.com/octo-user"><code>@​octo-user</code></a> in <a href="https://redirect.github.com/acme-org/left-pad/pull/12">acme-org/left-pad#12</a></li>',
    '</ul>',
    '<p><strong>Full Changelog</strong>: <a href="https://github.com/acme-org/left-pad/compare/v1.0.0...v1.1.0">https://github.com/acme-org/left-pad/compare/v1.0.0...v1.1.0</a></p>',
    '</blockquote>',
    '</details>',
    '<details>',
    '<summary>Commits</summary>',
    '<ul>',
    '<li><a href="https://github.com/acme-org/left-pad/commit/abc1234"><code>abc1234</code></a> Release 1.1.0</li>',
    '<li>See full diff in <a href="https://github.com/acme-org/left-pad/compare/v1.0.0...v1.1.0">compare view</a></li>',
    '</ul>',
    '</details>',
    '<br />',
    '',
    '| Package | From | To |',
    '| --- | --- | --- |',
    '| [left-pad](https://github.com/acme-org/left-pad) | `1.0.0` | `1.1.0` |',
    '',
    '[![Dependabot compatibility score](https://dependabot-badges.githubapp.com/badges/compatibility_score?dependency-name=left-pad&package-manager=npm_and_yarn&previous-version=1.0.0&new-version=1.1.0)](https://docs.github.com/en/github/managing-security-vulnerabilities/about-dependabot-security-updates#about-compatibility-scores)',
    '',
    "Dependabot will resolve any conflicts with this PR as long as you don't alter it yourself. You can also trigger a rebase manually by commenting `@dependabot rebase`.",
    '',
    '[//]: # (dependabot-automerge-start)',
    '[//]: # (dependabot-automerge-end)',
    '',
    '---',
    '',
    '<details>',
    '<summary>Dependabot commands and options</summary>',
    '<br />',
    '',
    'You can trigger Dependabot actions by commenting on this PR:',
    '- `@dependabot rebase` will rebase this PR',
    '- `@dependabot recreate` will recreate this PR, overwriting any edits that have been made to it',
    '',
    '',
    '</details>',
  ].join('\n')

  it('renders its notes, commits, table, badge, and commands sensibly', async () => {
    const { root, openLink } = show(body)

    expect(tags(root)).toEqual(['p', 'details', 'details', 'div', 'div', 'p', 'p', 'hr', 'details'])
    expect([...root.querySelectorAll('summary')].map((summary) => summary.textContent)).toEqual([
      'Release notes',
      'Commits',
      'Dependabot commands and options',
    ])

    const [notes, commits, commands] = [...root.querySelectorAll('details')]
    expect(notes?.querySelector('blockquote h2')).toHaveTextContent('v1.1.0')
    expect(notes?.querySelectorAll('h2')[1]).toHaveTextContent("What's Changed")
    expect(notes?.querySelector('blockquote li code')).toHaveTextContent('@​octo-user')
    expect(commits?.querySelectorAll('li')).toHaveLength(2)
    expect(commits?.querySelector('li code')).toHaveTextContent('abc1234')
    expect(commands?.querySelectorAll('li')).toHaveLength(2)
    expect(
      within(screen.getByRole('table'))
        .getAllByRole('cell')
        .map((cell) => cell.textContent),
    ).toEqual(['left-pad', '1.0.0', '1.1.0'])
    expect(root).not.toHaveTextContent('automerge')
    expectInert(root)

    await userEvent.click(screen.getByRole('button', { name: 'Dependabot compatibility score' }))

    expect(openLink).toHaveBeenCalledExactlyOnceWith(
      'https://docs.github.com/en/github/managing-security-vulnerabilities/about-dependabot-security-updates#about-compatibility-scores',
    )
  })
})
