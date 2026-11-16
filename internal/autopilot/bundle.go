package autopilot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
)

const (
	// BundleVersion is the version of the export format this build writes, and the newest
	// it reads. A change an older build would misread raises it; adding a field does not.
	BundleVersion = 1

	// bundlePrefix starts the format's name, which ends in its version.
	bundlePrefix = brand.Slug + "-autopilot/"

	// BundleFormat names the export format this build writes: "<slug>-autopilot/1".
	BundleFormat = bundlePrefix + "1"

	// MaxBundleBytes caps the size of a bundle the app reads.
	MaxBundleBytes = 8 << 20
)

var (
	// ErrNotBundle is returned for a file that holds no routines in the export format.
	ErrNotBundle = errors.New("the file doesn't hold " + brand.Name + " routines")

	// ErrNewerBundle is returned for routines a newer build exported in a format this
	// build can't read.
	ErrNewerBundle = errors.New("the routines were saved by a newer version of " + brand.Name +
		"; update " + brand.Name + " to read them")
)

// Bundle is routines in the export format: the copy kept beside the database, a backup's
// Autopilot section, and an export. It holds each routine's definition, name, description,
// mode, and switch, and never its history or its account. Prompts are named by nickname
// and profiles by agent and name, as the spec names them, so a bundle works on another
// Mac. docs/backup-files.md specifies the format:
//
//	{
//	  "format": "<slug>-autopilot/1",
//	  "exportedAt": "2026-11-16T09:00:00Z",
//	  "app": "v0.5.0",
//	  "routines": [
//	    {"name": "...", "description": "...", "mode": "ask", "enabled": false, "template": "...", "spec": {...}}
//	  ]
//	}
type Bundle struct {
	// Format is BundleFormat when this build wrote it.
	Format string `json:"format"`

	// ExportedAt is when it was written, as RFC 3339, and App the version that wrote it.
	ExportedAt string `json:"exportedAt"`
	App        string `json:"app"`

	// Routines are the routines, in the user's order.
	Routines []BundleRoutine `json:"routines"`
}

// BundleRoutine is one routine in the export format.
type BundleRoutine struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Mode        autopilotprefs.Mode `json:"mode"`

	// Enabled is whether it was on where it was exported; a routine brought in always
	// arrives off.
	Enabled bool `json:"enabled"`

	// Template names the template it was made from, "" for none.
	Template string `json:"template,omitempty"`

	// Spec is its definition, as stored.
	Spec json.RawMessage `json:"spec"`
}

// bundleOf returns rows in the export format, written at now by the version app. A row
// whose spec isn't JSON at all, which nothing can read, is left out.
func bundleOf(rows []queries.AutopilotRoutine, app string, now time.Time) Bundle {
	out := Bundle{
		Format: BundleFormat, ExportedAt: clock.Format(now), App: app,
		Routines: make([]BundleRoutine, 0, len(rows)),
	}

	for _, r := range rows {
		if !json.Valid([]byte(r.Spec)) {
			continue
		}

		out.Routines = append(out.Routines, BundleRoutine{
			Name: r.Name, Description: r.Description, Mode: storedMode(r.Mode), Enabled: r.Enabled,
			Template: r.TemplateKey, Spec: json.RawMessage(r.Spec),
		})
	}

	return out
}

// EncodeBundle writes b as indented JSON ending in a newline.
func EncodeBundle(b Bundle) ([]byte, error) {
	if b.Routines == nil {
		b.Routines = []BundleRoutine{}
	}

	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(b); err != nil {
		return nil, fmt.Errorf("write the routines: %w", err)
	}

	return buf.Bytes(), nil
}

// DecodeBundle reads routines in the export format, of any version this build reads,
// refusing anything else. Its list is never nil. It checks no routine: a restore or an
// import checks each one as it stores it.
func DecodeBundle(data []byte) (Bundle, error) {
	if len(data) > MaxBundleBytes {
		return Bundle{}, fmt.Errorf("%w: it is too large", ErrNotBundle)
	}

	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, fmt.Errorf("%w: %w", ErrNotBundle, err)
	}

	version, ok := strings.CutPrefix(b.Format, bundlePrefix)
	if !ok {
		return Bundle{}, ErrNotBundle
	}

	n, err := strconv.Atoi(version)

	switch {
	case err != nil || n < 1 || version != strconv.Itoa(n):
		return Bundle{}, ErrNotBundle
	case n > BundleVersion:
		return Bundle{}, ErrNewerBundle
	}

	if b.Routines == nil {
		b.Routines = []BundleRoutine{}
	}

	return b, nil
}
