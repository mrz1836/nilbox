import { useEffect, useId, useRef, type KeyboardEvent } from 'react'

import { usePopover } from '../lib/usePopover'
import { openInBrowser, type Account } from '../platform'
import { Avatar } from './Avatar'
import { ExternalIcon, SlidersIcon } from './icons'

export interface AccountMenuProps {
  // account is null before the inbox has loaded; its login is "" before the first refresh.
  account: Account | null
  onOpenSettings: () => void
}

const itemClass =
  'flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left text-sm text-text hover:bg-raised focus:bg-raised focus-visible:outline-none'

// menuItems lists a menu's items in order.
function menuItems(menu: HTMLElement | null): HTMLElement[] {
  return Array.from(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])
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

// AccountMenu is the toolbar's profile button. It shows the signed-in account's picture
// and opens a menu with who is signed in, Settings, and the account's GitHub profile. It
// behaves like a native menu: arrows, Home, and End move between items, Escape closes it
// and returns focus to the button, and a click outside closes it.
export function AccountMenu({ account, onOpenSettings }: AccountMenuProps) {
  const id = useId()
  const { open, show, toggle, close, triggerRef, panelRef } = usePopover<HTMLButtonElement, HTMLDivElement>()
  const menuRef = useRef<HTMLDivElement>(null)
  // startAt is the item focused when the menu opens: the last one when opened with ArrowUp.
  const startAt = useRef<'first' | 'last'>('first')

  const signedIn = account !== null && account.login !== ''
  const hasName = signedIn && account.name.trim() !== ''

  useEffect(() => {
    if (open) {
      const items = menuItems(menuRef.current)
      const item = startAt.current === 'last' ? items.at(-1) : items[0]

      item?.focus()
      startAt.current = 'first'
    }
  }, [open])

  const onTriggerKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      startAt.current = e.key === 'ArrowUp' ? 'last' : 'first'
      show()
    }
  }

  const onMenuKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const target = nextItem(menuItems(menuRef.current), e.key)

    if (target !== undefined) {
      e.preventDefault()
      target.focus()
    }
  }

  const openSettings = () => {
    close(true)
    onOpenSettings()
  }

  const openProfile = () => {
    close(true)

    if (signedIn) {
      openInBrowser(account.profileUrl).catch((err: unknown) => {
        console.error('could not open the GitHub profile in the browser', err)
      })
    }
  }

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-label={signedIn ? `Account: ${account.login}` : 'Account'}
        title={signedIn ? account.login : 'Account'}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={toggle}
        onKeyDown={onTriggerKeyDown}
        className="flex size-8 items-center justify-center rounded-full transition-colors hover:bg-raised aria-expanded:bg-raised"
      >
        <Avatar account={account} size="sm" />
      </button>
      {open && (
        <div
          ref={panelRef}
          className="absolute top-full right-0 z-40 mt-2 w-64 origin-top-right rounded-lg border border-border bg-surface shadow-xl shadow-black/20 motion-safe:animate-pop-in"
        >
          <div className="flex items-center gap-3 px-3.5 py-3">
            <Avatar account={account} size="md" />
            <div className="min-w-0">
              {signedIn ? (
                <>
                  <p className="truncate text-sm font-semibold">{hasName ? account.name : account.login}</p>
                  <p className="truncate text-xs text-muted">{hasName ? `@${account.login}` : 'Signed in to GitHub'}</p>
                </>
              ) : (
                <>
                  <p className="text-sm font-semibold">Not signed in yet</p>
                  <p className="text-xs text-muted">Your account appears after the first refresh.</p>
                </>
              )}
            </div>
          </div>
          <div
            ref={menuRef}
            id={id}
            role="menu"
            aria-label="Account"
            tabIndex={-1}
            onKeyDown={onMenuKeyDown}
            className="border-t border-border p-1.5 outline-none"
          >
            <button type="button" role="menuitem" tabIndex={-1} onClick={openSettings} className={itemClass}>
              <SlidersIcon className="size-4 text-muted" />
              Settings
              <kbd aria-hidden="true" className="ml-auto font-sans text-xs text-muted">
                ⌘,
              </kbd>
            </button>
            {signedIn && (
              <button type="button" role="menuitem" tabIndex={-1} onClick={openProfile} className={itemClass}>
                <ExternalIcon className="size-4 text-muted" />
                View GitHub profile
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
