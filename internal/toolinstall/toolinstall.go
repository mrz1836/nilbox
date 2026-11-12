// Package toolinstall turns a tool's GitHub release into a verified program in place: it
// picks the release's archive and checksums file, downloads both into a fresh private
// folder, checks the archive against the checksums file and GitHub's digests, extracts
// the program's one entry, tries it, and puts it in its folder so that the target is never
// missing, rolling back when the placed copy won't run.
//
// Nothing outside the download folder changes until the last step, and every refusal or
// cancel before it leaves nothing behind. It has no database and speaks no HTTP itself: the
// caller hands it a Downloader, and records what it placed.
package toolinstall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// maxChecksumsBytes caps a checksums file.
	maxChecksumsBytes = 1 << 20

	// maxArchiveBytes caps a release archive.
	maxArchiveBytes = 200 << 20

	// dirPerm and filePerm keep the download folder and its files the user's alone.
	dirPerm  = 0o700
	filePerm = 0o600

	// execPerm is a placed program's mode.
	execPerm = 0o755
)

var (
	// ErrNoBuild is returned when the release has no archive for this OS and architecture.
	ErrNoBuild = errors.New("the release has no build for this machine")

	// ErrNoChecksums is returned when the release has no checksums file: nothing is
	// installed unchecked.
	ErrNoChecksums = errors.New("the release has no checksums file")

	// ErrShortDownload is returned when a download's size isn't the size GitHub lists.
	ErrShortDownload = errors.New("the download was cut short")

	// ErrChecksum is returned when a download doesn't match its checksum or GitHub's
	// digest, or the checksums file doesn't list the archive exactly once.
	ErrChecksum = errors.New("the download didn't match its checksum")

	// ErrArchive is returned for an archive that doesn't hold the program as one plain
	// file at its root, or holds something unsafe.
	ErrArchive = errors.New("the archive isn't as expected")

	// ErrSmoke is returned when the downloaded program won't run or reports another
	// version. The download is thrown away and nothing changes.
	ErrSmoke = errors.New("the downloaded program didn't run")

	// ErrLinkTarget is returned when the target is a symbolic link, which is never
	// replaced.
	ErrLinkTarget = errors.New("the target is a link")

	// ErrRolledBack is returned when the placed program didn't run and the old copy was
	// put back, or the new one taken away when there was none.
	ErrRolledBack = errors.New("the new copy didn't run and was rolled back")

	// ErrRollbackFailed is returned when the placed program didn't run and putting the old
	// copy back failed too: the new copy is still in place.
	ErrRollbackFailed = errors.New("the new copy didn't run, and the old one couldn't be put back")

	// ErrWrite is returned when the target's folder can't be written.
	ErrWrite = errors.New("can't write the folder")
)

// Downloader streams a public release file into w, at most maxBytes, and returns how many
// bytes it wrote. *github.Client implements it.
type Downloader interface {
	DownloadAsset(ctx context.Context, rawURL string, w io.Writer, maxBytes int64) (int64, error)
}

// Asset is one file of a release, as GitHub lists it.
type Asset struct {
	Name string
	URL  string
	Size int64

	// Digest is GitHub's digest of it, as in sha256:<hex>, or "" when it has none.
	Digest string
}

// Step is where an install is.
type Step string

// The steps an install reports.
const (
	StepDownloading Step = "downloading"
	StepVerifying   Step = "verifying"
	StepTrying      Step = "trying"
	StepInstalling  Step = "installing"
)

// Progress is how far an install has got: its step, and while downloading, the bytes so
// far of the archive's total.
type Progress struct {
	Step  Step
	Done  int64
	Total int64
}

// Request is one install or update of a tool.
type Request struct {
	// Tool names the tool in the download folder's name, as in mage-x, and Binary is its
	// program, as in magex.
	Tool   string
	Binary string

	// Version is the release's version, without a leading v.
	Version string

	// Archive and Checksums are the names of the release's archive for this machine and
	// of its checksums file, and Assets the release's files.
	Archive   string
	Checksums string
	Assets    []Asset

	// Dir is the folder the program goes in. It is created when missing, and a link to a
	// folder is followed: the program goes in the folder it names.
	Dir string

	// Smoke runs the program at path and returns an error unless it reports Version. It
	// runs on the extracted copy, then on the placed one.
	Smoke func(ctx context.Context, path string) error

	// Progress hears each step and the download's bytes. It may be nil.
	Progress func(Progress)
}

// Placed is what an install put in place.
type Placed struct {
	// Path is the program's path, in the folder Dir resolves to.
	Path string

	// SHA256 is the placed program's SHA-256, in hex.
	SHA256 string

	// Version and Archive are the release's version and the archive it came from.
	Version string
	Archive string

	// Replaced reports that a program was there before and was replaced.
	Replaced bool
}

// Installer installs releases. Downloads is the folder the downloads go in, each in a
// fresh folder of its own; Slug names those folders and the files placed beside a target,
// so cleanup finds only its own.
type Installer struct {
	Downloads string
	Download  Downloader
	Slug      string

	// maxProgram and maxUnpacked cap the program's entry and everything the archive
	// unpacks to; zero means the defaults. Tests shrink them.
	maxProgram, maxUnpacked int64
}

// Install downloads, checks, tries, and places the release req names, and returns what it
// placed. On any failure before the program is placed, nothing outside the download
// folder has changed, and that folder is removed. Every file in the download folder is
// made and read through an os.Root of it, so no name, an asset's from GitHub included,
// reaches out of it.
func (in *Installer) Install(ctx context.Context, req Request) (Placed, error) {
	archive, checksums, err := pickAssets(req)
	if err != nil {
		return Placed{}, err
	}

	work, err := in.workDir(req)
	if err != nil {
		return Placed{}, err
	}

	defer func() {
		_ = work.Close()
		_ = os.RemoveAll(work.Name())
	}()

	progress := func(p Progress) {
		if req.Progress != nil {
			req.Progress(p)
		}
	}

	if _, err = in.fetch(ctx, work, checksums, maxChecksumsBytes, nil); err != nil {
		return Placed{}, err
	}

	archiveSum, err := in.fetch(ctx, work, archive, maxArchiveBytes, func(done int64) {
		progress(Progress{Step: StepDownloading, Done: done, Total: archive.Size})
	})
	if err != nil {
		return Placed{}, err
	}

	progress(Progress{Step: StepVerifying})

	if err = verify(work, checksums.Name, archive.Name, archiveSum); err != nil {
		return Placed{}, err
	}

	program, programSum, err := in.extract(work, archive.Name, req.Binary)
	if err != nil {
		return Placed{}, err
	}

	progress(Progress{Step: StepTrying})

	if err = req.Smoke(ctx, program); err != nil {
		return Placed{}, fmt.Errorf("%w: %w", ErrSmoke, err)
	}

	progress(Progress{Step: StepInstalling})

	path, replaced, err := in.place(ctx, work, req)
	if err != nil {
		return Placed{}, err
	}

	return Placed{Path: path, SHA256: programSum, Version: req.Version, Archive: archive.Name, Replaced: replaced}, nil
}

// pickAssets finds the release's archive and checksums file by name.
func pickAssets(req Request) (archive, checksums Asset, err error) {
	find := func(name string) (Asset, bool) {
		i := slices.IndexFunc(req.Assets, func(a Asset) bool { return a.Name == name })
		if i < 0 {
			return Asset{}, false
		}

		return req.Assets[i], true
	}

	archive, ok := find(req.Archive)
	if !ok {
		return Asset{}, Asset{}, fmt.Errorf("%w: no %s", ErrNoBuild, req.Archive)
	}

	checksums, ok = find(req.Checksums)
	if !ok {
		return Asset{}, Asset{}, fmt.Errorf("%w: no %s", ErrNoChecksums, req.Checksums)
	}

	for _, a := range []Asset{archive, checksums} {
		if !safeName(a.Name) {
			return Asset{}, Asset{}, fmt.Errorf("%w: the file name %q", ErrArchive, a.Name)
		}
	}

	return archive, checksums, nil
}

// safeName reports a file name that is one plain name, safe to create in a folder.
func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`+"\x00")
}

// workDir makes a fresh private folder for one install's downloads, and returns it opened
// as an os.Root, which every file in it is made and read through: its Name is the folder's
// path.
func (in *Installer) workDir(req Request) (*os.Root, error) {
	if err := os.MkdirAll(in.Downloads, dirPerm); err != nil {
		return nil, fmt.Errorf("make the downloads folder: %w", err)
	}

	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("name the download folder: %w", err)
	}

	dir := filepath.Join(in.Downloads, in.Slug+"-"+req.Tool+"-"+req.Version+"-"+hex.EncodeToString(random))
	if err := os.Mkdir(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("make the download folder: %w", err)
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		_ = os.Remove(dir)

		return nil, fmt.Errorf("open the download folder: %w", err)
	}

	return root, nil
}
