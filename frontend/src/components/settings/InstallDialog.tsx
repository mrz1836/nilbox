import { brand } from '../../generated/brand'
import type { IntegrationTool } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'

// InstallKind is what a card asks to do with a tool's latest release: install it where
// there is no copy, update the copy to it, or install it again over the copy.
export type InstallKind = 'install' | 'update' | 'reinstall'

// confirmLabels name each kind's confirm button.
const confirmLabels: Readonly<Record<InstallKind, string>> = {
  install: 'Install',
  update: 'Update',
  reinstall: 'Reinstall',
}

// installTitle asks whether to go ahead, naming the version when the release is known.
function installTitle(kind: InstallKind, tool: IntegrationTool): string {
  const { version } = tool.install

  if (kind === 'update') {
    return version === '' ? `Update ${tool.name}?` : `Update ${tool.name} to ${version}?`
  }

  return `${`${confirmLabels[kind]} ${tool.name} ${version}`.trimEnd()}?`
}

// downloadText says what is downloaded, checked, and put where: the release's archive
// for this Mac, or, until the release is known, the latest release.
function downloadText(tool: IntegrationTool): string {
  const { archive, targetDir } = tool.install
  const what =
    archive === ''
      ? `the latest ${tool.name} release from GitHub`
      : `${archive} from the ${tool.repo} release on GitHub`

  return `${brand.name} downloads ${what}, checks it against the release’s SHA-256 checksums, and puts ${tool.binary} in ${targetDir}. Nothing else on your Mac changes.`
}

export interface InstallDialogProps {
  tool: IntegrationTool
  kind: InstallKind
  onConfirm: () => void
  onCancel: () => void
}

// InstallDialog asks before an install, an update, or a reinstall: what is downloaded
// and checked, where it goes, the copy it replaces, and, for a copy the app didn't
// install, whether to replace it at all. It only asks; the card runs the install and
// shows its progress.
export function InstallDialog({ tool, kind, onConfirm, onCancel }: InstallDialogProps) {
  const { binary } = tool
  const { version, replaces, replacesPath, foreign } = tool.install

  return (
    <ConfirmDialog
      title={installTitle(kind, tool)}
      confirmLabel={foreign ? 'Replace' : confirmLabels[kind]}
      tone={foreign ? 'danger' : 'primary'}
      onConfirm={onConfirm}
      onCancel={onCancel}
    >
      <p>{downloadText(tool)}</p>
      {replacesPath !== '' && (
        <p className="mt-2">
          Replaces <span className="font-mono text-text">{`${binary} ${replaces}`.trimEnd()}</span> at{' '}
          <span className="font-mono break-all text-text">{replacesPath}</span>.
        </p>
      )}
      {foreign && (
        <p className="mt-2 text-text">
          This copy wasn’t installed by {brand.name}. Replace it with {version === '' ? 'the latest release' : version}?
        </p>
      )}
    </ConfirmDialog>
  )
}
