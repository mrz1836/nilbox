package update

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// maxUninstallable is how many updates that failed for good are remembered: the newest.
const maxUninstallable = 5

// ErrUninstallable is returned by Download and Restart for an update a restart failed to
// install for good: it failed its signature check, couldn't be installed, or wouldn't open,
// as it would every time. Its release page is the way to get it.
var ErrUninstallable = errors.New("the update failed to install before, the same way it would again")

// uninstallable is an update a restart failed to install for good: its version and its
// zip's SHA-512, which tell it apart from the same version rebuilt, and why, in words.
type uninstallable struct {
	Version string `json:"version"`
	SHA512  string `json:"sha512"`
	Reason  string `json:"reason"`
}

// decodeUninstallable reads KeyUninstallable; anything unreadable remembers nothing.
func decodeUninstallable(stored string) []uninstallable {
	var list []uninstallable
	if stored == "" || json.Unmarshal([]byte(stored), &list) != nil {
		return nil
	}

	return list
}

// lookup finds the update of version with the zip sha512 in list.
func lookup(list []uninstallable, version, sha512 string) (uninstallable, bool) {
	i := slices.IndexFunc(list, func(u uninstallable) bool { return u.Version == version && u.SHA512 == sha512 })
	if i < 0 {
		return uninstallable{}, false
	}

	return list[i], true
}

// blocked reports whether m's update failed to install for good. A list that can't be read
// blocks nothing, logged.
func (e *Engine) blocked(ctx context.Context, m Manifest) bool {
	stored, err := e.memory.GetMany(ctx, KeyUninstallable)
	if err != nil {
		if ctx.Err() == nil {
			e.logger.WarnContext(ctx, "could not read the updates that failed to install", "error", err)
		}

		return false
	}

	_, ok := lookup(decodeUninstallable(stored[KeyUninstallable]), m.Version, m.SHA512)

	return ok
}

// rememberUninstallable remembers the update of version with the zip sha512 as one that
// failed to install for good, for reason, the newest maxUninstallable kept. A failure to
// remember it is logged: the update is then offered again, and fails again.
func (e *Engine) rememberUninstallable(ctx context.Context, version, sha512, reason string) {
	stored, err := e.memory.GetMany(ctx, KeyUninstallable)
	if err != nil {
		e.logger.WarnContext(ctx, "could not read the updates that failed to install", "error", err)

		return
	}

	list := decodeUninstallable(stored[KeyUninstallable])
	if _, ok := lookup(list, version, sha512); ok {
		return
	}

	list = append(list, uninstallable{Version: version, SHA512: sha512, Reason: reason})
	if len(list) > maxUninstallable {
		list = list[len(list)-maxUninstallable:]
	}

	body, err := json.Marshal(list)
	if err == nil {
		err = e.memory.SetMany(ctx, map[string]string{KeyUninstallable: string(body)})
	}

	if err != nil {
		e.logger.WarnContext(ctx, "could not remember the update that failed to install", "version", version, "error", err)

		return
	}

	e.logger.InfoContext(ctx, "the update failed to install for good; it isn't offered again", "version", version)
}

// failedForGood is the failure of found, the newest version found, when stored (the
// KeyUninstallable list) holds it: an earlier run's restart failed to install it for good.
// Otherwise it is none.
func (e *Engine) failedForGood(stored string, found Manifest) InstallFailure {
	u, ok := lookup(decodeUninstallable(stored), found.Version, found.SHA512)
	if !ok {
		return InstallFailure{}
	}

	return InstallFailure{Version: u.Version, Reason: u.Reason, Log: e.logKept(u.Version)}
}

// logKept reports whether the helper's log of the install of version is in the logs folder.
func (e *Engine) logKept(version string) bool {
	if e.logs == "" {
		return false
	}

	info, err := os.Stat(e.installLog(version))

	return err == nil && info.Mode().IsRegular()
}

// uninstallableError refuses to download or install version, which failed to install for
// good.
func uninstallableError(version string) error {
	return uierr.New(version+" didn't install before, and would fail the same way again. Download it from its "+
		"release page instead.", ErrUninstallable)
}
