import { useRef, useState } from 'react'

import { errorMessage } from './format'

// PickReply fills in the replies the user picks from a sheet's Replies menu.
export interface PickReply {
  // pick fills in the reply with replyId for the item, and hands its text over.
  pick: (replyId: number) => void
  // drop forgets a pick under way, as the user's own typing does, so its text never lands
  // over what they did since.
  drop: () => void
  // error says why the item's latest pick couldn't be filled in, or ''.
  error: string
}

// usePickReply fills in, with fill, each reply the user picks for the item with itemId, and
// hands its text to use: only the latest pick's, so one answered after another pick, or
// after drop, is never used. Why a pick failed shows for its own item alone.
export function usePickReply(
  itemId: number,
  fill: (itemId: number, replyId: number) => PromiseLike<string>,
  use: (text: string, replyId: number) => void,
): PickReply {
  const latest = useRef(0)
  const [failed, setFailed] = useState<{ id: number; message: string } | null>(null)

  const pick = (replyId: number) => {
    latest.current += 1

    const mine = latest.current

    setFailed(null)
    fill(itemId, replyId).then(
      (text) => {
        if (latest.current === mine) {
          use(text, replyId)
        }
      },
      (err: unknown) => {
        if (latest.current === mine) {
          setFailed({ id: itemId, message: errorMessage(err) })
        }
      },
    )
  }

  const drop = () => {
    latest.current += 1
  }

  return { pick, drop, error: failed?.id === itemId ? failed.message : '' }
}
