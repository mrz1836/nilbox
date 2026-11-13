import { useState, type ReactNode } from 'react'

import { usePicture } from '../lib/pictures'
import type { Account } from '../platform'
import { PersonIcon } from './icons'

type AvatarSize = 'xs' | 'sm' | 'md' | 'lg'

// Each size's box and the initial's type size, spelled out in full so Tailwind finds them.
const boxes: Readonly<Record<AvatarSize, string>> = {
  xs: 'size-5 text-[0.625rem]',
  sm: 'size-6 text-[0.6875rem]',
  md: 'size-9 text-sm',
  lg: 'size-14 text-xl',
}

const glyphs: Readonly<Record<AvatarSize, string>> = {
  xs: 'size-3.5',
  sm: 'size-4',
  md: 'size-5',
  lg: 'size-7',
}

// Picture is a round profile picture from src, a data: URL, or, without one or should it
// fail to decode, what stands in for it.
function Picture({ src, size, children }: { src: string; size: AvatarSize; children: ReactNode }) {
  // failed is the picture that could not be shown, so a new picture gets a fresh try.
  const [failed, setFailed] = useState('')
  const box = `${boxes[size]} shrink-0 rounded-full`

  if (src !== '' && src !== failed) {
    return (
      <img
        src={src}
        alt=""
        draggable={false}
        onError={() => {
          setFailed(src)
        }}
        className={`${box} bg-raised object-cover`}
      />
    )
  }

  return (
    <span
      aria-hidden="true"
      className={`${box} inline-flex items-center justify-center bg-raised font-semibold text-muted`}
    >
      {children}
    </span>
  )
}

export interface AvatarProps {
  // account is null before the first refresh.
  account: Account | null
  size: AvatarSize
}

// initial is the first letter of the display name, or of the login when there is none.
function initial(account: Account): string {
  const source = account.name.trim() === '' ? account.login : account.name.trim()

  return source.charAt(0).toUpperCase()
}

// Avatar shows the account's cached profile picture. Without one (not downloaded yet,
// or it failed to decode) it shows the account's initial, and before there is an
// account, a person glyph. It is decorative; the control around it names the account.
export function Avatar({ account, size }: AvatarProps) {
  return (
    <Picture src={account?.avatar ?? ''} size={size}>
      {account === null || account.login === '' ? <PersonIcon className={glyphs[size]} /> : initial(account)}
    </Picture>
  )
}

// PersonAvatar is someone's profile picture, small, beside their name: the one the sheet
// read for their login (PicturesContext), or their login's first letter until it comes, or
// should it never, and a person glyph for no login, as for a deleted account. It is
// decorative; the name beside it says who.
export function PersonAvatar({ login }: { login: string }) {
  return (
    <Picture src={usePicture(login)} size="xs">
      {login === '' ? <PersonIcon className={glyphs.xs} /> : login.charAt(0).toUpperCase()}
    </Picture>
  )
}
