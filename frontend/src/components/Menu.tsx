import { Fragment, useEffect, useId, useRef, type KeyboardEvent, type ReactNode } from 'react'

import { usePopover } from '../lib/usePopover'
import { PopoverPanel } from './PopoverPanel'

export interface MenuItem {
  label: string
  icon?: ReactNode
  // hint is quieter text at the item's end, such as when a choice takes effect.
  hint?: ReactNode
  // shortcut is the keys that do the same from anywhere, such as ⌘,: shown at the item's
  // end, and left out of its accessible name.
  shortcut?: string
  onSelect: () => void
  disabled?: boolean
  // title explains the item, such as why it is disabled.
  title?: string | undefined
  danger?: boolean
  // divided draws a line above the item, setting it apart from those before it.
  divided?: boolean
}

export interface MenuButtonProps {
  // label names the button for assistive tech; children draw it.
  label: string
  // menuLabel names the menu when the button's label says more, such as who is signed in;
  // it defaults to label.
  menuLabel?: string
  title?: string
  // items are the menu's actions, or a function that builds them, called only while the
  // menu is open: for a menu repeated on every row, whose items cost something to build.
  items: readonly MenuItem[] | (() => readonly MenuItem[])
  children: ReactNode
  className?: string
  // align puts the menu's right or left edge under the button's.
  align?: 'left' | 'right'
  // heading is a caption above the items.
  heading?: string
  // header is content above the items, set apart from them by a line, such as the account
  // the menu belongs to.
  header?: ReactNode
  // panelClassName sizes the menu: by default it is at least 13rem wide.
  panelClassName?: string
}

// noItems is a closed menu's items.
const noItems: readonly MenuItem[] = []

const itemClass =
  'flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm whitespace-nowrap hover:bg-raised focus:bg-raised focus-visible:outline-none disabled:cursor-default disabled:opacity-50 disabled:hover:bg-transparent'

// menuItems lists a menu's enabled items in order.
function menuItems(menu: HTMLElement | null): HTMLElement[] {
  return Array.from(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)') ?? [])
}

// nextItem is the item a navigation key moves to, wrapping at both ends, or undefined
// for any other key.
function nextItem(items: HTMLElement[], key: string): HTMLElement | undefined {
  const at = items.findIndex((item) => item === document.activeElement)

  switch (key) {
    case 'ArrowDown':
      return items[(at + 1) % items.length]
    case 'ArrowUp':
      return items[(at - 1 + items.length) % items.length]
    case 'Home':
      return items[0]
    case 'End':
      return items.at(-1)
    default:
      return undefined
  }
}

// MenuButton is a button that opens a menu of actions. It behaves like a native menu: the
// down and up arrows on the button open it at the first or the last item, arrows, Home,
// and End move between items, Escape closes it and returns focus to the button, and a
// click outside closes it. It opens upward when there is not enough room below the button.
export function MenuButton({
  label,
  menuLabel = label,
  title,
  items,
  children,
  className = '',
  align = 'right',
  heading,
  header,
  panelClassName = 'min-w-52',
}: MenuButtonProps) {
  const id = useId()
  const menuRef = useRef<HTMLDivElement>(null)
  // startAt is the item focused when the menu opens: the last one when opened with ArrowUp.
  const startAt = useRef<'first' | 'last'>('first')
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>({
    placement: 'flip',
    focus: (panel) => {
      const enabled = menuItems(panel)
      const item = startAt.current === 'last' ? enabled.at(-1) : enabled[0]

      startAt.current = 'first'

      return item
    },
  })
  const { open, toggle, show, close, triggerRef } = popover
  const shown = !open ? noItems : typeof items === 'function' ? items() : items

  // A menu that closes before its focus moved, or that was asked for the last item while
  // open, starts at the first item next time.
  useEffect(() => {
    if (!open) {
      startAt.current = 'first'
    }
  }, [open])

  // The arrows on the button open the menu at the first or the last item; with it already
  // open, they move focus there directly, since opening again would not.
  const onTriggerKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') {
      return
    }

    e.preventDefault()

    if (open) {
      const enabled = menuItems(menuRef.current)

      ;(e.key === 'ArrowUp' ? enabled.at(-1) : enabled[0])?.focus()

      return
    }

    startAt.current = e.key === 'ArrowUp' ? 'last' : 'first'
    show()
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const target = nextItem(menuItems(menuRef.current), e.key)

    if (target !== undefined) {
      e.preventDefault()
      target.focus()
    }
  }

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        title={title ?? label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        onKeyDown={onTriggerKeyDown}
        className={className}
      >
        {children}
      </button>
      <PopoverPanel
        state={popover}
        align={align}
        className={`z-40 shadow-black/20 ${panelClassName} ${header === undefined ? 'p-1.5' : ''}`}
      >
        {header}
        {heading !== undefined && (
          <p aria-hidden="true" className="px-2.5 pt-1 pb-1.5 text-xs font-medium text-muted">
            {heading}
          </p>
        )}
        <div
          ref={menuRef}
          id={id}
          role="menu"
          aria-label={menuLabel}
          tabIndex={-1}
          onKeyDown={onKeyDown}
          className={`outline-none ${header === undefined ? '' : 'border-t border-border p-1.5'}`}
        >
          {shown.map((item) => (
            <Fragment key={item.label}>
              {item.divided === true && <hr className="mx-1 my-1.5 border-border" />}
              <button
                type="button"
                role="menuitem"
                tabIndex={-1}
                disabled={item.disabled}
                title={item.title}
                onClick={() => {
                  close(true)
                  item.onSelect()
                }}
                className={`${itemClass} ${item.danger === true ? 'text-danger' : 'text-text'}`}
              >
                {item.icon}
                {item.label}
                {item.hint !== undefined && (
                  <>
                    <span className="sr-only">, </span>
                    <span className="ml-auto flex items-center gap-1.5 pl-6 text-xs text-muted">{item.hint}</span>
                  </>
                )}
                {item.shortcut !== undefined && (
                  <kbd aria-hidden="true" className="ml-auto font-sans text-xs text-muted">
                    {item.shortcut}
                  </kbd>
                )}
              </button>
            </Fragment>
          ))}
        </div>
      </PopoverPanel>
    </div>
  )
}
