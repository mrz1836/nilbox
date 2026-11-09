import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef, useState, type RefObject } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { suggest, type Facet, type QuerySpec, type Suggester } from '../lib/query'
import { SearchField } from './SearchField'

const spec: QuerySpec = { keys: ['is', 'label'], multi: ['is', 'label'] }

interface Thing {
  labels: string[]
  pr: boolean
}

const things: Thing[] = [
  { labels: ['bug', 'good first issue'], pr: true },
  { labels: ['bug'], pr: false },
]

const facets: Facet[] = [
  { label: 'Pull requests', key: 'is', value: 'pr' },
  { label: 'Label', key: 'label', noun: ['label', 'labels'] },
]

// suggester offers the facets and labels of two things.
const suggester: Suggester = (src, caret) =>
  suggest(src, caret, {
    spec,
    items: things,
    matches: (t, q) => q.filters.every((f) => (f.key === 'is' ? t.pr : f.values.some((v) => t.labels.includes(v)))),
    values: (t, key) => (key === 'label' ? t.labels : t.pr ? ['pr'] : []),
    facets,
    hint: 'Matches titles · -word excludes',
  })

interface HarnessProps {
  initial?: string
  matches?: number | null
  withSuggestions?: boolean
  withSpec?: boolean
  onChange?: (value: string) => void
  inputRef?: RefObject<HTMLInputElement | null>
}

// Harness holds the field's query as the window does.
function Harness({
  initial = '',
  matches = null,
  withSuggestions = true,
  withSpec = true,
  onChange,
  inputRef,
}: HarnessProps) {
  const [value, setValue] = useState(initial)

  return (
    <>
      <button type="button">elsewhere</button>
      <SearchField
        value={value}
        onChange={(v) => {
          setValue(v)
          onChange?.(v)
        }}
        placeholder="Filter inbox"
        spec={withSpec ? spec : undefined}
        matches={matches}
        suggest={withSuggestions ? suggester : undefined}
        inputRef={inputRef}
      />
    </>
  )
}

function field(): HTMLInputElement {
  return screen.getByRole('combobox', { name: 'Filter inbox' })
}

function options(): string[] {
  return screen.queryAllByRole('option').map((o) => o.textContent)
}

// escapes records whether each Escape that reaches the document was taken by the field.
const escapes: boolean[] = []

function recordEscape(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    escapes.push(e.defaultPrevented)
  }
}

beforeEach(() => {
  escapes.length = 0
  document.addEventListener('keydown', recordEscape)
})

afterEach(() => {
  document.removeEventListener('keydown', recordEscape)
})

describe('SearchField', () => {
  it('waits, idle, with what it does and its shortcut', () => {
    render(<Harness />)

    expect(field()).toHaveAttribute('placeholder', 'Filter inbox')
    expect(field()).toHaveAttribute('type', 'text')
    expect(field()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByText('⌘F')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Clear the search' })).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('')
  })

  it('lists what can narrow the page, counted, when focused and empty', async () => {
    render(<Harness />)
    await userEvent.click(field())

    const list = screen.getByRole('listbox', { name: 'Filters' })

    expect(field()).toHaveAttribute('aria-expanded', 'true')
    expect(field()).toHaveAttribute('aria-controls', list.id)
    expect(field()).not.toHaveAttribute('aria-activedescendant')
    expect(options()).toEqual(['Pull requestsis:pr1', 'Labellabel:2 labels'])
    expect(screen.getByText('Matches titles · -word excludes')).toBeInTheDocument()
    expect(screen.queryByText('⌘F')).not.toBeInTheDocument()
  })

  it('chooses with the arrows and inserts with Enter, then waits for the next word', async () => {
    const onChange = vi.fn<(value: string) => void>()

    render(<Harness onChange={onChange} />)
    await userEvent.click(field())
    await userEvent.keyboard('{ArrowDown}{ArrowDown}{ArrowDown}')

    const first = screen.getByRole('option', { name: /Pull requests/ })

    expect(first).toHaveAttribute('aria-selected', 'true')
    expect(field()).toHaveAttribute('aria-activedescendant', first.id)

    await userEvent.keyboard('{ArrowDown}{ArrowUp}')

    expect(first).toHaveAttribute('aria-selected', 'true')

    // ↑ from the first wraps to the last.
    await userEvent.keyboard('{ArrowUp}{Enter}')

    expect(onChange).toHaveBeenLastCalledWith('label:')
    expect(field()).toHaveValue('label:')
    expect(field().selectionStart).toBe(6)
    // A key waits for its value, whose list starts at the most common one.
    expect(screen.getByRole('listbox', { name: 'label' })).toBeInTheDocument()
    expect(options()).toEqual(['bug2', 'good first issue1'])
    expect(screen.getByRole('option', { name: /bug/ })).toHaveAttribute('aria-selected', 'true')

    await userEvent.keyboard('{ArrowDown}{Tab}')

    expect(field()).toHaveValue('label:"good first issue" ')
    expect(field().selectionStart).toBe(25)
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('leaves Enter and Tab alone while nothing is chosen', async () => {
    const onChange = vi.fn<(value: string) => void>()

    render(<Harness onChange={onChange} />)
    await userEvent.click(field())
    await userEvent.keyboard('{Enter}')

    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole('listbox')).toBeInTheDocument()

    await userEvent.keyboard('{Tab}')

    expect(screen.getByRole('button', { name: 'elsewhere' })).not.toHaveFocus()
    expect(field()).not.toHaveFocus()
  })

  it('chooses with the mouse, keeping the caret in the field', async () => {
    render(<Harness />)
    await userEvent.click(field())
    await userEvent.hover(screen.getByRole('option', { name: /Label/ }))

    expect(screen.getByRole('option', { name: /Label/ })).toHaveAttribute('aria-selected', 'true')

    await userEvent.click(screen.getByRole('option', { name: /Pull requests/ }))

    expect(field()).toHaveValue('is:pr ')
    expect(field()).toHaveFocus()
  })

  it('just filters on plain words, and opens the list on ↓', async () => {
    render(<Harness />)
    await userEvent.click(field())
    await userEvent.keyboard('lab')

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    await userEvent.keyboard('{ArrowDown}')

    expect(options()).toEqual(['Labellabel:2 labels'])
    expect(screen.getByRole('option')).toHaveAttribute('aria-selected', 'true')

    // Typing on closes it again.
    await userEvent.keyboard('e')

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    await userEvent.keyboard('{ArrowDown}{Enter}')

    expect(field()).toHaveValue('label:')
  })

  it('leaves Enter alone on plain words', async () => {
    const onChange = vi.fn<(value: string) => void>()

    render(<Harness initial="cache" onChange={onChange} />)
    await userEvent.click(field())
    await userEvent.keyboard('{Enter}')

    expect(onChange).not.toHaveBeenCalled()
  })

  it('opens nothing on ↓ without suggestions, and ↑ moves nothing while closed', async () => {
    render(<Harness withSuggestions={false} initial="x" />)
    await userEvent.click(field())
    await userEvent.keyboard('{ArrowUp}{ArrowDown}')

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('takes Escape in steps: the list, then the query, then the field', async () => {
    render(<Harness initial="label:b" />)
    await userEvent.click(field())

    expect(screen.getByRole('listbox')).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(field()).toHaveValue('label:b')

    await userEvent.keyboard('{Escape}')

    expect(field()).toHaveValue('')
    expect(field()).toHaveFocus()
    // Escape closed the empty field's list on its way out of the query, so it stays shut.
    expect(screen.getByRole('listbox')).toBeInTheDocument()

    await userEvent.keyboard('{Escape}{Escape}')

    expect(field()).not.toHaveFocus()
    expect(escapes).toEqual([true, true, true, true])
  })

  it('opens its list afresh once it is left and focused again', async () => {
    render(<Harness />)
    await userEvent.click(field())
    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'elsewhere' }))
    await userEvent.click(field())

    expect(screen.getByRole('listbox')).toBeInTheDocument()
  })

  it('clears with ✕ and stays in the field', async () => {
    const onChange = vi.fn<(value: string) => void>()

    render(<Harness initial="cache" onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: 'Clear the search' }))

    expect(onChange).toHaveBeenLastCalledWith('')
    expect(field()).toHaveValue('')
    expect(field()).toHaveFocus()
  })

  it.each([
    [5, '5 matches'],
    [1, '1 match'],
    [0, 'No matches'],
  ])('says it matches %i', (matches, said) => {
    const { container } = render(<Harness initial="cache" matches={matches} />)

    expect(screen.getByRole('status')).toHaveTextContent(said)
    // The words show while the field is wide enough, the number alone when it is narrow.
    expect(screen.getByText(said, { selector: 'span[class*="@[13rem]:inline"]' })).toBeInTheDocument()
    expect(container.querySelector('.border-accent\\/70')).not.toBeNull()
  })

  it('tints the keys the page understands, behind the field', () => {
    const { container } = render(<Harness initial="-label:bug cache" />)
    const backdrop = container.querySelector('[aria-hidden="true"].whitespace-pre')

    expect(backdrop).toHaveTextContent('-label:bug cache')
    expect(within(backdrop as HTMLElement).getByText('-label:')).toHaveClass('text-accent')
    expect(field()).toHaveClass('text-transparent')

    // A scroll of the field scrolls the tinted copy with it.
    fireEvent.scroll(field())
  })

  it.each([
    ['a page with no keys', 'label:bug', false],
    ['a query with no key', 'plain words', true],
  ])('draws plain text for %s', (_name, initial, withSpec) => {
    const { container } = render(<Harness initial={initial} withSpec={withSpec} />)

    expect(container.querySelector('.whitespace-pre')).toBeNull()
    expect(field()).toHaveClass('text-text')
  })

  it('hands its text field to the window, for ⌘F', () => {
    const ref = createRef<HTMLInputElement>()

    render(<Harness inputRef={ref} />)

    expect(ref.current).toBe(field())
  })
})
