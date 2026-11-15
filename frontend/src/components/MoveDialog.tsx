import { brand } from '../generated/brand'
import type { MoveControl, MoveDialogState } from '../lib/useMove'
import { ConfirmDialog } from './ConfirmDialog'
import { quietButtonClass } from './controls'
import { FolderIcon } from './icons'

export interface MoveDialogProps {
  move: MoveControl
}

// working says what the dialog is doing, on its main button and to a screen reader, or
// '' while it waits for the user.
function working({ variant, work, leaving }: MoveDialogState): string {
  if (work === null) {
    return ''
  }

  if (variant === 'exists') {
    return 'Opening it…'
  }

  return leaving ? 'Reopening from Applications…' : 'Moving to Applications…'
}

// told is what the dialog's status tells a screen reader as it changes: what it is doing
// while it works, and, once the question turns to the copy in Applications, that one is
// there, since the buttons it was on went off as it worked.
function told(dialog: MoveDialogState): string {
  const doing = working(dialog)

  if (doing === '' && dialog.variant === 'exists') {
    return `${brand.name} is already in Applications.`
  }

  return doing
}

// MoveDialog offers to move the app into Applications, the one place it can update itself
// from, on every launch from anywhere else until it is moved: Move to Applications copies
// it there, and the app reopens from the copy; Not now, the safe choice, which it starts
// on, puts it away for this launch. When a copy is in Applications already, it offers to
// open that one instead (Open it). Either way the copy the user opened stays where it is,
// which they can show in Finder and delete, unless macOS opened it from a hidden folder.
// While the move runs, its buttons are off and its main button says so; then focus is back
// on Not now, and its status says what changed. A failure shows its words, and leaves
// everything as it was. It shows nothing while closed.
export function MoveDialog({ move }: MoveDialogProps) {
  const { dialog, location } = move

  if (dialog === null || location === null) {
    return null
  }

  const exists = dialog.variant === 'exists'
  const doing = working(dialog)

  return (
    <ConfirmDialog
      title={exists ? `Open ${brand.name} from Applications?` : 'Move to Applications?'}
      confirmLabel={doing !== '' ? doing : exists ? 'Open it' : 'Move to Applications'}
      cancelLabel="Not now"
      tone="primary"
      busy={dialog.work !== null}
      onConfirm={move.confirm}
      onCancel={move.dismiss}
    >
      <p>
        {exists
          ? `${brand.name} is already in Applications.`
          : `${brand.name} can only update itself from your Applications folder. It will reopen from there.`}
      </p>
      <p className="mt-2">The copy you opened stays where it is, and you can delete it.</p>
      {location.kind !== 'translocated' && (
        <button type="button" onClick={move.reveal} className={`${quietButtonClass} mt-1.5 -ml-1.5`}>
          <FolderIcon className="size-3.5" />
          Show in Finder
        </button>
      )}
      {dialog.error !== '' && (
        <p role="alert" className="mt-2 text-danger">
          {dialog.error}
        </p>
      )}
      <p role="status" className="sr-only">
        {told(dialog)}
      </p>
    </ConfirmDialog>
  )
}
