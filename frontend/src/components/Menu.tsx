import {
  Fragment,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react'

import { remToPx } from '../lib/rem'
import { usePopover } from '../lib/usePopover'
import { ChevronRightIcon } from './icons'
import { PopoverPanel } from './PopoverPanel'

export interface MenuItem {
  label: string
  icon?: ReactNode
  // hint is quieter text at the item's end, such as when a choice takes effect.
  hint?: ReactNode
  // shortcut is the keys that do the same from anywhere, such as ⌘,: shown at the item's
  // end, and left out of its accessible name.
  shortcut?: string
  // onSelect runs when the item is chosen. An item with a submenu needs none.
  onSelect?: () => void
  // items makes the item open a submenu of them beside it, as the pointer rests on it,
  // as it is clicked, or with the right arrow; the left arrow closes it again. A function
  // is called only while the submenu is open.
  items?: readonly MenuItem[] | (() => readonly MenuItem[])
  disabled?: boolean
  // title explains the item, such as why it is disabled.
  title?: string | undefined
  danger?: boolean
  // divided draws a line above the item, setting it apart from those before it.
  divided?: boolean
  // section is a caption above the item, naming the group it starts, such as "On GitHub".
  section?: string
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
  'flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm whitespace-nowrap hover:bg-raised focus:bg-raised focus-visible:outline-none disabled:cursor-default disabled:opacity-50 disabled:hover:bg-transparent aria-expanded:bg-raised'

// submenuGap is the least room a submenu keeps from the window's edges before it opens
// the other way, in rem, so it follows the text size.
const submenuGap = 0.5

// built returns a menu's items, building them when given a function.
function built(items: readonly MenuItem[] | (() => readonly MenuItem[])): readonly MenuItem[] {
  return typeof items === 'function' ? items() : items
}

// menuItems lists the enabled items of one menu in order, leaving out those of the
// submenus open inside it.
function menuItems(menu: Element | null): HTMLElement[] {
  return Array.from(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)') ?? []).filter(
    (item) => item.closest('[role="menu"]') === menu,
  )
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

// navigate moves focus among one menu's items with the arrows, Home, and End, and keeps
// the key from the menus around it.
function navigate(e: KeyboardEvent<HTMLDivElement>) {
  const target = nextItem(menuItems(e.currentTarget), e.key)

  if (target !== undefined) {
    e.preventDefault()
    e.stopPropagation()
    target.focus()
  }
}

interface SubmenuProps {
  id: string
  label: string
  items: readonly MenuItem[]
  // focusFirst moves focus to the first item as the submenu opens, as the keyboard does.
  focusFirst: boolean
  onChoose: (item: MenuItem) => void
  // onBack closes the submenu and puts focus back on the item that opened it.
  onBack: () => void
}

// Submenu is the menu an item opens beside it: to its right, or to its left when there is
// not enough room on the right, and up from the item's bottom when there is not enough
// room below.
function Submenu({ id, label, items, focusFirst, onChoose, onBack }: SubmenuProps) {
  const menuRef = useRef<HTMLDivElement>(null)
  const [flip, setFlip] = useState({ left: false, up: false })

  // Place it before it paints, so it never shows on the wrong side first.
  useLayoutEffect(() => {
    const rect = menuRef.current?.getBoundingClientRect()

    if (rect !== undefined) {
      const gap = remToPx(submenuGap)

      setFlip({
        left: rect.right > window.innerWidth - gap,
        up: rect.bottom > window.innerHeight - gap,
      })
    }
  }, [])

  useEffect(() => {
    if (focusFirst) {
      menuItems(menuRef.current)[0]?.focus()
    }
  }, [focusFirst])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'ArrowLeft') {
      e.preventDefault()
      e.stopPropagation()
      onBack()

      return
    }

    navigate(e)
  }

  const x = flip.left ? 'right-full mr-1' : 'left-full ml-1'
  const y = flip.up ? '-bottom-[calc(var(--spacing)*1.5+1px)]' : '-top-[calc(var(--spacing)*1.5+1px)]'

  return (
    <div
      ref={menuRef}
      id={id}
      role="menu"
      aria-label={label}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      className={`absolute z-10 min-w-48 rounded-lg border border-border bg-surface p-1.5 shadow-xl shadow-black/20 outline-none motion-safe:animate-pop-in ${x} ${y}`}
    >
      <MenuItems items={items} onChoose={onChoose} />
    </div>
  )
}

interface MenuItemsProps {
  items: readonly MenuItem[]
  // onChoose closes every menu open and runs the item chosen.
  onChoose: (item: MenuItem) => void
}

// MenuItems are one menu's items, with the submenu of the one that has one open.
function MenuItems({ items, onChoose }: MenuItemsProps) {
  const baseId = useId()
  // open is the label of the item whose submenu is open, and focusFirst whether the
  // keyboard opened it.
  const [open, setOpen] = useState<{ label: string; focusFirst: boolean } | null>(null)
  const parentRefs = useRef(new Map<string, HTMLButtonElement>())

  const openSubmenu = (item: MenuItem, focusFirst: boolean) => {
    setOpen({ label: item.label, focusFirst })
  }

  return items.map((item, i) => {
    const hasSubmenu = item.items !== undefined
    const isOpen = hasSubmenu && open?.label === item.label
    const submenuId = `${baseId}-${String(i)}`

    const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
      if (hasSubmenu && item.disabled !== true && e.key === 'ArrowRight') {
        e.preventDefault()
        e.stopPropagation()
        openSubmenu(item, true)
      }
    }

    return (
      <Fragment key={item.label}>
        {item.divided === true && <hr className="mx-1 my-1.5 border-border" />}
        {item.section !== undefined && (
          <p
            aria-hidden="true"
            className="px-2.5 pt-1 pb-1 text-[0.6875rem] font-medium tracking-wide text-muted uppercase"
          >
            {item.section}
          </p>
        )}
        <div className="relative">
          <button
            ref={(button) => {
              if (button === null) {
                parentRefs.current.delete(item.label)
              } else {
                parentRefs.current.set(item.label, button)
              }
            }}
            type="button"
            role="menuitem"
            tabIndex={-1}
            disabled={item.disabled}
            title={item.title}
            aria-haspopup={hasSubmenu ? 'menu' : undefined}
            aria-expanded={hasSubmenu ? isOpen : undefined}
            aria-controls={isOpen ? submenuId : undefined}
            onPointerEnter={() => {
              if (hasSubmenu && item.disabled !== true) {
                openSubmenu(item, false)
              } else if (!hasSubmenu) {
                setOpen(null)
              }
            }}
            onFocus={() => {
              // The keyboard moving on to another item closes a submenu open beside this
              // menu, as the pointer does.
              if (open !== null && open.label !== item.label) {
                setOpen(null)
              }
            }}
            onKeyDown={onKeyDown}
            onClick={() => {
              // A click on an item with a submenu opens it and moves into it, even when the
              // pointer resting on the item opened it already, as on the Mac.
              if (hasSubmenu) {
                openSubmenu(item, true)
              } else {
                onChoose(item)
              }
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
            {hasSubmenu && <ChevronRightIcon className="ml-auto size-3.5 shrink-0 text-muted" />}
          </button>
          {isOpen && item.items !== undefined && (
            <Submenu
              id={submenuId}
              label={item.label}
              items={built(item.items)}
              focusFirst={open.focusFirst}
              onChoose={onChoose}
              onBack={() => {
                setOpen(null)
                parentRefs.current.get(item.label)?.focus()
              }}
            />
          )}
        </div>
      </Fragment>
    )
  })
}

// MenuButton is a button that opens a menu of actions. It behaves like a native menu: the
// down and up arrows on the button open it at the first or the last item, arrows, Home,
// and End move between items, Escape closes it and returns focus to the button, and a
// click outside closes it. It opens upward when there is not enough room below the button.
// An item can open a submenu beside it (MenuItem.items), which the right arrow enters and
// the left arrow leaves; Escape closes every menu at once, as on the Mac.
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
      const enabled = menuItems(panel.querySelector('[role="menu"]'))
      const item = startAt.current === 'last' ? enabled.at(-1) : enabled[0]

      startAt.current = 'first'

      return item
    },
  })
  const { open, toggle, show, close, triggerRef } = popover
  const shown = open ? built(items) : noItems

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

  const choose = (item: MenuItem) => {
    close(true)
    item.onSelect?.()
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
          onKeyDown={navigate}
          className={`outline-none ${header === undefined ? '' : 'border-t border-border p-1.5'}`}
        >
          <MenuItems items={shown} onChoose={choose} />
        </div>
      </PopoverPanel>
    </div>
  )
}
