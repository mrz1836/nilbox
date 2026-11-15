import { questionWords, runText, type RestartStep } from '../lib/update'
import { ConfirmDialog } from './ConfirmDialog'

export interface UpdateRestartDialogProps {
  // flow is where the restart to update stands; the dialog shows only while it asks.
  flow: RestartStep
  onConfirm: () => void
  onCancel: () => void
}

// UpdateRestartDialog asks before restarting to update when the restart would lose
// something: the runs going in the app, which it stops, in the quit dialog's words, and
// the text not sent in the sheets, in one question. Restart to update goes ahead and stops
// them; Not now, the safe choice, which it starts on, leaves everything as it was. It
// shows nothing while the restart asks nothing.
export function UpdateRestartDialog({ flow, onConfirm, onCancel }: UpdateRestartDialogProps) {
  if (flow.step !== 'asking') {
    return null
  }

  const words = questionWords(flow.question)

  return (
    <ConfirmDialog
      title="Restart to update?"
      confirmLabel="Restart to update"
      cancelLabel="Not now"
      size={words.list.length > 0 ? 'wide' : 'normal'}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <p>{words.text}</p>
      {words.list.length > 0 && (
        <ul className="my-2 list-disc pl-5">
          {words.list.map((run) => (
            <li key={run.id} className="text-text">
              {runText(run)}
            </li>
          ))}
        </ul>
      )}
      {words.notes.length > 0 && <p className="mt-2">{words.notes.join(' ')}</p>}
    </ConfirmDialog>
  )
}
