import { fireEvent, render, screen } from '@testing-library/react'
import { createRef, useId, type ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { domShape } from '../test/dom'
import { SearchIcon } from './icons'
import { SearchBox } from './SearchBox'

const noop = () => undefined

// The searches below are pinned copies of each page's own search as it was written before
// the page moved onto SearchBox, kept to check that SearchBox draws the same field for the
// props that page passes.

// The Scripts tab's repository search, on the Hooks and Release pages, and labelled for
// magex's targets.
function RepoSearchBefore({
  label = 'Search repositories',
  placeholder = 'Search repositories…',
}: {
  label?: string
  placeholder?: string
}) {
  const id = useId()

  return (
    <>
      <label htmlFor={id} className="sr-only">
        {label}
      </label>
      <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1 focus-within:border-accent/60">
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <input
          id={id}
          type="search"
          value="hooks"
          placeholder={placeholder}
          onChange={noop}
          className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
        />
      </div>
    </>
  )
}

// The prompt library's search, which ⌘F focuses through its ref.
function PromptLibraryBefore({ replies }: { replies: boolean }) {
  const id = useId()

  return (
    <>
      <label htmlFor={`${id}-search`} className="sr-only">
        {replies ? 'Search replies' : 'Search prompts'}
      </label>
      <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 focus-within:border-accent/60">
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <input
          id={`${id}-search`}
          type="search"
          value="hooks"
          placeholder="Search"
          onChange={noop}
          className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
        />
      </div>
    </>
  )
}

// The variables panel's search.
function VariablesPanelBefore() {
  const searchId = useId()

  return (
    <>
      <label htmlFor={searchId} className="sr-only">
        Search variables
      </label>
      <div className="flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 focus-within:border-accent/60">
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <input
          id={searchId}
          type="search"
          value="hooks"
          placeholder="Search variables"
          onChange={noop}
          className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
        />
      </div>
    </>
  )
}

// The Runs page's search, which grows to fill its row beside the filter.
function RunsPageBefore() {
  const id = useId()

  return (
    <>
      <label htmlFor={`${id}-search`} className="sr-only">
        Search repositories
      </label>
      <div className="flex min-w-40 flex-1 items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1 focus-within:border-accent/60">
        <SearchIcon className="size-3.5 shrink-0 text-muted" />
        <input
          id={`${id}-search`}
          type="search"
          value="hooks"
          placeholder="Search repositories…"
          onChange={noop}
          className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
        />
      </div>
    </>
  )
}

// shapeOf is the DOM a search draws.
function shapeOf(ui: ReactElement): string {
  const { container, unmount } = render(ui)
  const shape = domShape(container)

  unmount()

  return shape
}

describe('SearchBox', () => {
  it.each<{ page: string; before: ReactElement; shared: ReactElement }>([
    {
      page: 'the Hooks and Release pages',
      before: <RepoSearchBefore />,
      shared: <SearchBox value="hooks" onChange={noop} />,
    },
    {
      page: 'magex’s targets',
      before: <RepoSearchBefore label="Search magex targets" placeholder="Search magex targets…" />,
      shared: (
        <SearchBox value="hooks" onChange={noop} label="Search magex targets" placeholder="Search magex targets…" />
      ),
    },
    {
      page: 'the prompt library',
      before: <PromptLibraryBefore replies={false} />,
      shared: (
        <SearchBox
          value="hooks"
          onChange={noop}
          inputRef={createRef<HTMLInputElement>()}
          label="Search prompts"
          placeholder="Search"
          dense={false}
        />
      ),
    },
    {
      page: 'the prompt library’s replies',
      before: <PromptLibraryBefore replies />,
      shared: <SearchBox value="hooks" onChange={noop} label="Search replies" placeholder="Search" dense={false} />,
    },
    {
      page: 'the variables panel',
      before: <VariablesPanelBefore />,
      shared: (
        <SearchBox
          value="hooks"
          onChange={noop}
          label="Search variables"
          placeholder="Search variables"
          dense={false}
        />
      ),
    },
    {
      page: 'the Runs page',
      before: <RunsPageBefore />,
      shared: <SearchBox value="hooks" onChange={noop} className="min-w-40 flex-1" />,
    },
  ])('draws the search of $page as the page drew it before', ({ before, shared }) => {
    expect(shapeOf(shared)).toBe(shapeOf(before))
  })

  it('is a search field named by its label, a search of the page’s repositories by default', () => {
    render(<SearchBox value="hooks" onChange={noop} />)

    const field = screen.getByRole('searchbox', { name: 'Search repositories' })

    expect(field).toHaveValue('hooks')
    expect(field).toHaveAttribute('placeholder', 'Search repositories…')
    expect(screen.getByText('Search repositories')).toHaveClass('sr-only')
  })

  it('names the field and fills it as told', () => {
    render(<SearchBox value="" onChange={noop} label="Search variables" placeholder="Search variables" />)

    expect(screen.getByRole('searchbox', { name: 'Search variables' })).toHaveAttribute(
      'placeholder',
      'Search variables',
    )
  })

  it('draws a dense box by default, a taller one when not dense, and adds the classes it is given', () => {
    const box = (ui: ReactElement) => {
      const { unmount } = render(ui)
      const classes = screen.getByRole('searchbox').parentElement?.className

      unmount()

      return classes
    }

    expect(box(<SearchBox value="" onChange={noop} />)).toBe(
      'flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1 focus-within:border-accent/60',
    )
    expect(box(<SearchBox value="" onChange={noop} dense={false} />)).toBe(
      'flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 focus-within:border-accent/60',
    )
    expect(box(<SearchBox value="" onChange={noop} className="min-w-40 flex-1" />)).toBe(
      'min-w-40 flex-1 flex items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1 focus-within:border-accent/60',
    )
  })

  it('hands each edit to onChange', () => {
    const onChange = vi.fn<(value: string) => void>()

    render(<SearchBox value="hook" onChange={onChange} />)
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'hooks' } })

    expect(onChange).toHaveBeenCalledExactlyOnceWith('hooks')
  })

  it('hands its field to inputRef', () => {
    const ref = createRef<HTMLInputElement>()

    render(<SearchBox value="" onChange={noop} inputRef={ref} />)

    expect(ref.current).toBe(screen.getByRole('searchbox'))
  })

  it('gives each search an ID of its own', () => {
    render(
      <>
        <SearchBox value="" onChange={noop} label="First" />
        <SearchBox value="" onChange={noop} label="Second" />
      </>,
    )

    expect(screen.getByRole('searchbox', { name: 'First' }).id).not.toBe(
      screen.getByRole('searchbox', { name: 'Second' }).id,
    )
  })
})
