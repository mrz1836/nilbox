package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// Format names the backup file format, so a backup can be told from any other JSON
	// file.
	Format = brand.Slug + ".backup"

	// Version is the version of the format this build writes, and the newest it reads. A
	// change an older build would misread raises it; adding a section or a field does not.
	Version = 1

	// MaxBytes caps the size of a backup the app reads.
	MaxBytes = 16 << 20
)

var (
	// ErrNotBackup is returned for a file that is not a backup.
	ErrNotBackup = errors.New("the file isn't a " + brand.Name + " backup")

	// ErrNewerBackup is returned for a backup a newer build wrote in a format this build
	// cannot read.
	ErrNewerBackup = errors.New("the backup was made by a newer version of " + brand.Name +
		"; update " + brand.Name + " to import it")

	// ErrTooLarge is returned for a backup larger than MaxBytes.
	ErrTooLarge = fmt.Errorf("the file is too large: a backup is at most %d MB", MaxBytes>>20)
)

// File is a backup: the user's setup in portable form, one section per category the user
// ticked, each absent when it wasn't. The prompts and the launch defaults sections are
// the app's own portable formats, each with its own format and version, exactly as the
// copies it keeps beside its database (prompts.json and launch-defaults.json); both name
// prompts by nickname. docs/backup-files.md specifies the format:
//
//	{
//	  "format": "<slug>.backup",
//	  "version": 1,
//	  "exportedAt": "2026-11-14T17:00:00Z",
//	  "app": "v0.4.0",
//	  "settings": {...},
//	  "repositories": {...},
//	  "prompts": {"format": "<slug>.prompts", ...},
//	  "launchDefaults": {"format": "<slug>.launch-defaults", ...},
//	  "scripts": {"scripts": [...]}
//	}
type File struct {
	// Format is Format.
	Format string `json:"format"`

	// Version is the format's version, Version when this build wrote it.
	Version int `json:"version"`

	// ExportedAt is when the file was written, as RFC 3339.
	ExportedAt string `json:"exportedAt"`

	// App is the version of the app that wrote the file.
	App string `json:"app"`

	Settings       json.RawMessage `json:"settings,omitempty"`
	Repositories   json.RawMessage `json:"repositories,omitempty"`
	Prompts        json.RawMessage `json:"prompts,omitempty"`
	LaunchDefaults json.RawMessage `json:"launchDefaults,omitempty"`
	Scripts        json.RawMessage `json:"scripts,omitempty"`
}

// Section returns the section s of the file, or nil when the file doesn't hold it.
func (f File) Section(s Section) json.RawMessage {
	switch s {
	case SectionSettings:
		return present(f.Settings)
	case SectionRepositories:
		return present(f.Repositories)
	case SectionPrompts:
		return present(f.Prompts)
	case SectionLaunchDefaults:
		return present(f.LaunchDefaults)
	case SectionScripts:
		return present(f.Scripts)
	default:
		return nil
	}
}

// withSection returns f with raw as its section s.
func (f File) withSection(s Section, raw json.RawMessage) File {
	switch s {
	case SectionSettings:
		f.Settings = raw
	case SectionRepositories:
		f.Repositories = raw
	case SectionPrompts:
		f.Prompts = raw
	case SectionLaunchDefaults:
		f.LaunchDefaults = raw
	case SectionScripts:
		f.Scripts = raw
	}

	return f
}

// present returns raw, or nil for a section written as null.
func present(raw json.RawMessage) json.RawMessage {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}

	return raw
}

// Encode writes f as indented JSON ending in a newline. Characters HTML treats specially
// stay as they are, so a prompt's text reads in the file as it was written.
func Encode(f File) ([]byte, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("write the backup: %w", err)
	}

	return buf.Bytes(), nil
}

// Decode reads a backup. It refuses a file that is not one, one larger than MaxBytes, and
// one in a newer format than this build reads. Fields it does not know are ignored, so a
// file a newer build wrote in the same format still reads.
func Decode(data []byte) (File, error) {
	if len(data) > MaxBytes {
		return File{}, ErrTooLarge
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("%w: %w", ErrNotBackup, err)
	}

	switch {
	case f.Format != Format || f.Version < 1:
		return File{}, ErrNotBackup
	case f.Version > Version:
		return File{}, ErrNewerBackup
	}

	for _, s := range Sections() {
		raw := f.Section(s)
		if raw != nil && bytes.TrimSpace(raw)[0] != '{' {
			return File{}, fmt.Errorf("%w: its %s aren't an object", ErrNotBackup, s)
		}

		f = f.withSection(s, raw)
	}

	return f, nil
}

// Hash returns the SHA-256 of data, in hex, which an import checks the file against.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// read reads the backup at path, with its bytes' hash.
func read(path string) (File, string, error) {
	data, err := appdata.ReadFile(path, MaxBytes)

	switch {
	case errors.Is(err, appdata.ErrTooLarge):
		return File{}, "", ErrTooLarge
	case errors.Is(err, appdata.ErrNotAFile):
		return File{}, "", fmt.Errorf("%w: %w", ErrNotBackup, err)
	case err != nil:
		return File{}, "", err
	}

	f, err := Decode(data)
	if err != nil {
		return File{}, "", err
	}

	return f, Hash(data), nil
}

// EncodeSection returns v as a section of a backup: JSON in which the characters HTML treats
// specially stay as they are.
func EncodeSection(v any) (json.RawMessage, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("write the backup: %w", err)
	}

	return bytes.TrimSpace(buf.Bytes()), nil
}
