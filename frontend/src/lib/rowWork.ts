import { useRef, useState } from 'react'

// RowWork is what an inbox row is doing with GitHub: reading its item again ('refresh'),
// or an edit, taking the user off its assignees ('unassign') or off its requested
// reviewers ('review'); null while it does nothing. A row does one at a time, so a second
// click never sends a second request.
export type RowWork = 'refresh' | 'unassign' | 'review' | null

// StartWork begins work on a row, unless some is under way: it marks the row busy with
// what, and clears the mark once call settles. call reports its own failures, so it
// never rejects.
export type StartWork = (what: Exclude<RowWork, null>, call: () => Promise<void>) => void

// useRowWork keeps what a row is doing with GitHub, which its quiet controls and its menu
// share: either one starts work, and both show it.
export function useRowWork(): [RowWork, StartWork] {
  const [work, setWork] = useState<RowWork>(null)
  // busy is set at once, before the state above renders, so two starts in one moment
  // still run one call.
  const busy = useRef(false)

  const start: StartWork = (what, call) => {
    if (busy.current) {
      return
    }

    busy.current = true
    setWork(what)

    void call().finally(() => {
      busy.current = false
      setWork(null)
    })
  }

  return [work, start]
}
