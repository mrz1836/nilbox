import { useState, type ReactNode } from 'react'

import { usePicture } from '../lib/pictures'
import type { Account } from '../platform'
import { PersonIcon } from './icons'

type AvatarSize = 'xs' | 'sm' | 'md' | 'lg'

// Each size's box and the initial's type size, spelled out in full so Tailwind finds them.
const boxes: Readonly<Record<AvatarSize, string>> = {
  xs: 'size-5 text-badge',
  sm: 'size-6 text-caption',
  md: 'size-9 text-sm',
  lg: 'size-14 text-xl',
}

const glyphs: Readonly<Record<AvatarSize, string>> = {
  xs: 'size-3.5',
  sm: 'size-4',
  md: 'size-5',
  lg: 'size-7',
}

// PictureShape is a picture's outline: a circle for a person, a rounded square for a bot.
type PictureShape = 'circle' | 'square'

const outlines: Readonly<Record<PictureShape, string>> = {
  circle: 'rounded-full',
  square: 'rounded-lg',
}

interface PictureProps {
  // src is the picture, a data: URL, or '' while there is none.
  src: string
  // className is the picture's own classes: its size, and any ring.
  className: string
  shape?: PictureShape
  // children is what stands in for the picture.
  children: ReactNode
}

// Picture is a profile picture from src, a data: URL, in a circle, or a rounded square for a
// bot. What stands in for it shows until it decodes, and instead of it when there is none or
// it fails to decode; meanwhile it waits hidden, so it never shows as an empty box. React
// drops an image's load that comes before it mounts the image, which a picture the page
// already holds often beats, so an image already loaded when it mounts counts as decoded.
export function Picture({ src, className, shape = 'circle', children }: PictureProps) {
  // decoded is the picture that decoded, and failed the one that could not, so a new
  // picture waits for its own decoding and gets a fresh try.
  const [decoded, setDecoded] = useState('')
  const [failed, setFailed] = useState('')
  const loading = src !== '' && src !== failed
  const shown = loading && src === decoded

  return (
    <>
      {!shown && children}
      {loading && (
        <img
          ref={(img) => {
            if (img?.complete === true && img.naturalWidth > 0) {
              setDecoded(src)
            }
          }}
          src={src}
          alt=""
          draggable={false}
          hidden={!shown}
          onLoad={() => {
            setDecoded(src)
          }}
          onError={() => {
            setFailed(src)
          }}
          className={`${className} shrink-0 ${outlines[shape]} bg-raised object-cover`}
        />
      )}
    </>
  )
}

// Initial is what stands in for a person's picture: a letter, or a glyph, in a circle.
function Initial({ size, children }: { size: AvatarSize; children: ReactNode }) {
  return (
    <span
      aria-hidden="true"
      className={`${boxes[size]} inline-flex shrink-0 items-center justify-center rounded-full bg-raised font-semibold text-muted`}
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
// not decoded yet, or it failed to decode) it shows the account's initial, and before
// there is an account, a person glyph. It is decorative; the control around it names the account.
export function Avatar({ account, size }: AvatarProps) {
  return (
    <Picture src={account?.avatar ?? ''} className={boxes[size]}>
      <Initial size={size}>
        {account === null || account.login === '' ? <PersonIcon className={glyphs[size]} /> : initial(account)}
      </Initial>
    </Picture>
  )
}

// PersonAvatar is someone's profile picture, small by default, beside their name: the one
// the sheet read for their login (PicturesContext), or their login's first letter until it
// comes, or should it never, and a person glyph for no login, as for a deleted account. It
// is decorative; the name beside it says who.
export function PersonAvatar({ login, size = 'xs' }: { login: string; size?: AvatarSize }) {
  return (
    <Picture src={usePicture(login)} className={boxes[size]}>
      <Initial size={size}>
        {login === '' ? <PersonIcon className={glyphs[size]} /> : login.charAt(0).toUpperCase()}
      </Initial>
    </Picture>
  )
}
