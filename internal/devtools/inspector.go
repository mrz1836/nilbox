package devtools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
)

// ErrPointingOff is returned for pointing asked for while the user has it turned off.
var ErrPointingOff = errors.New("pointing is turned off in the window's Settings, under About → Inspector")

const (
	// inspectorDirName is the inspector's folder in the data folder, and notesDirName and
	// settingsFileName the notes' folder and the settings' file in it.
	inspectorDirName = "devtools"
	notesDirName     = "notes"
	settingsFileName = "settings.json"
)

// InspectorDir returns the inspector's folder in the data directory dataDir, where it keeps
// the notes the user leaves and its own settings, so they outlive the app. Only a build
// from source writes it.
func InspectorDir(dataDir string) string {
	return filepath.Join(dataDir, inspectorDirName)
}

// InspectorState is the inspector as the window's Settings and the menu bar show it:
// whether the user lets pointing start (Enabled), and how many notes are open, and kept in
// all, done or not.
type InspectorState struct {
	Enabled bool `json:"enabled"`
	Open    int  `json:"open"`
	Kept    int  `json:"kept"`
}

// inspectorSettings is what the inspector's settings file holds: whether pointing may
// start.
type inspectorSettings struct {
	Pointing bool `json:"pointing"`
}

// Open keeps the inspector's notes and settings in dir (InspectorDir) from now on,
// reading back what is kept there, and logs through logger what it can't keep there
// later. A settings file it can't read leaves pointing on, and the error says why.
func (p *Page) Open(dir string, logger *slog.Logger) error {
	settings, err := readSettings(filepath.Join(dir, settingsFileName))

	p.mu.Lock()
	p.dir, p.pointing = dir, settings.Pointing

	if logger != nil {
		p.logger = logger
	}
	p.mu.Unlock()

	return errors.Join(err, p.notes.Open(filepath.Join(dir, notesDirName)))
}

// State returns the inspector's state now.
func (p *Page) State() InspectorState {
	open, kept := p.notes.Counts()

	p.mu.Lock()
	defer p.mu.Unlock()

	return InspectorState{Enabled: p.pointing, Open: open, Kept: kept}
}

// OnChange has fn hear the inspector's state each time the page is told it: as the page
// loads, and after each change.
func (p *Page) OnChange(fn func(InspectorState)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.changed = append(p.changed, fn)
}

// SetPointing lets pointing start, or stops it from starting and ends it, and keeps the
// choice.
func (p *Page) SetPointing(on bool) error {
	p.mu.Lock()
	p.pointing = on
	dir := p.dir
	p.mu.Unlock()

	var err error
	if dir != "" {
		err = writeSettings(filepath.Join(dir, settingsFileName), inspectorSettings{Pointing: on})
	}

	p.sync()

	return err
}

// StartPointing turns pointing on in the window, as ⌥⌘P does there, or returns
// ErrPointingOff while the user has it turned off.
func (p *Page) StartPointing() error {
	if !p.State().Enabled {
		return ErrPointingOff
	}

	return p.Run("window.__appDevtools?.nb.point(true)")
}

// MarkDone marks done the open notes numbered ns, or every open note with all, and
// returns how many it marked.
func (p *Page) MarkDone(ns []int, all bool) (int, error) {
	n, err := p.notes.Done(ns, all)

	p.sync()

	return n, err
}

// ClearNotes drops every note kept, and returns how many there were.
func (p *Page) ClearNotes() (int, error) {
	n, err := p.notes.Clear()

	p.sync()

	return n, err
}

// choose carries out what the user chose in the inspector's part of the window's
// Settings: pointing on or off, or the notes cleared.
func (p *Page) choose(m agentMessage) {
	var err error

	if m.Enabled != nil {
		err = p.SetPointing(*m.Enabled)
	}

	if m.Clear {
		_, clearErr := p.ClearNotes()
		err = errors.Join(err, clearErr)
	}

	if err != nil {
		p.log().Warn("the inspector couldn't keep the user's choice", "error", err)
	}
}

// sync tells the agent, and each listener, the inspector's state.
func (p *Page) sync() {
	state := p.State()

	// A window that has not opened yet, or has gone away, is told as its page loads.
	if data, err := json.Marshal(state); err == nil {
		_ = p.Run("window.__appDevtools?.sync(" + string(data) + ")")
	}

	p.mu.Lock()
	listeners := slices.Clone(p.changed)
	p.mu.Unlock()

	for _, fn := range listeners {
		fn(state)
	}
}

// log returns the page's logger.
func (p *Page) log() *slog.Logger {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.logger
}

// readSettings reads the inspector's settings from path: pointing on when the file is
// missing, or can't be read, which the error then says.
func readSettings(path string) (inspectorSettings, error) {
	settings := inspectorSettings{Pointing: true}

	data, err := os.ReadFile(path) //nolint:gosec // the inspector's own folder
	if errors.Is(err, fs.ErrNotExist) {
		return settings, nil
	}

	if err != nil {
		return settings, fmt.Errorf("read the inspector's settings: %w", err)
	}

	if err = json.Unmarshal(data, &settings); err != nil {
		return inspectorSettings{Pointing: true}, fmt.Errorf("read the inspector's settings: %w", err)
	}

	return settings, nil
}

// writeSettings writes the inspector's settings to path, making its folder when it is
// missing.
func writeSettings(path string, settings inspectorSettings) error {
	if err := os.MkdirAll(filepath.Dir(path), noteDirPerm); err != nil {
		return fmt.Errorf("make the inspector's folder: %w", err)
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode the inspector's settings: %w", err)
	}

	return writeAtomic(path, append(data, '\n'))
}
