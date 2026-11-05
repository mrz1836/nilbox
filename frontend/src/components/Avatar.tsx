import { useState } from 'react'

import type { Account } from '../platform'
import { PersonIcon } from './icons'

type AvatarSize = 'sm' | 'md' | 'lg'

// Each size's box and the initial's type size, spelled out in full so Tailwind finds them.
const boxes: Readonly<Record<AvatarSize, string>> = {
  sm: 'size-6 text-[11px]',
  md: 'size-9 text-sm',
  lg: 'size-14 text-xl',
}

const glyphs: Readonly<Record<AvatarSize, string>> = {
  sm: 'size-4',
  md: 'size-5',
  lg: 'size-7',
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
  // failed is the picture that could not be shown, so a new picture gets a fresh try.
  const [failed, setFailed] = useState('')
  const box = `${boxes[size]} shrink-0 rounded-full`
  const src = account?.avatar ?? ''

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
      {account === null || account.login === '' ? <PersonIcon className={glyphs[size]} /> : initial(account)}
    </span>
  )
}
