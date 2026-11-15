package appdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ResetRequestVersion is the version of the reset request RequestReset writes.
const ResetRequestVersion = 1

// maxResetRequestBytes bounds the reset request ReadResetRequest reads.
const maxResetRequestBytes = 4 << 10

// ResetBy says where the user asked to start over.
type ResetBy string

// Where a reset is asked for.
const (
	// ResetBySettings is Settings → About → Reset.
	ResetBySettings ResetBy = "settings"

	// ResetByMagex is magex app:reset.
	ResetByMagex ResetBy = "magex"
)

// ErrNoResetVersion is returned for a reset request with no version.
var ErrNoResetVersion = errors.New("the reset request has no version")

// ResetRequest is the user's request to start over, which the next start of the app
// carries out before anything opens the database: it sets the database aside and starts a
// fresh one, bringing back the prompts, the launch defaults, and the user's own scripts.
type ResetRequest struct {
	// Version is ResetRequestVersion for a request this build wrote.
	Version int `json:"version"`

	// RequestedAt is when the user asked.
	RequestedAt time.Time `json:"requestedAt"`

	// By is where they asked.
	By ResetBy `json:"by"`
}

// RequestReset writes a request to start over to path, readable only by the user,
// replacing any request there, and creating its folder when the app never made it: the
// next start of the app carries it out.
func RequestReset(path string, by ResetBy, now time.Time) error {
	data, err := json.MarshalIndent(ResetRequest{Version: ResetRequestVersion, RequestedAt: now.UTC(), By: by}, "", "  ")
	if err != nil {
		return fmt.Errorf("write the reset request: %w", err)
	}

	if err = os.MkdirAll(filepath.Dir(path), PrivateDirPerm); err != nil {
		return fmt.Errorf("write the reset request: %w", err)
	}

	if err = WriteFile(path, append(data, '\n')); err != nil {
		return fmt.Errorf("write the reset request: %w", err)
	}

	return nil
}

// ReadResetRequest reads the request to start over at path, and reports whether there is
// one. A file that is not a request, as one a newer build wrote in a form this build
// can't read, is an error, and holds no request.
func ReadResetRequest(path string) (ResetRequest, bool, error) {
	data, err := ReadFile(path, maxResetRequestBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return ResetRequest{}, false, nil
	}

	if err != nil {
		return ResetRequest{}, false, fmt.Errorf("read the reset request: %w", err)
	}

	var req ResetRequest
	if err = json.Unmarshal(data, &req); err != nil {
		return ResetRequest{}, false, fmt.Errorf("read the reset request: %w", err)
	}

	if req.Version < 1 {
		return ResetRequest{}, false, ErrNoResetVersion
	}

	return req, true, nil
}

// ClearResetRequest removes the request to start over at path, once it was carried out or
// could not be read. A request that is not there is already cleared.
func ClearResetRequest(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the reset request: %w", err)
	}

	return nil
}
