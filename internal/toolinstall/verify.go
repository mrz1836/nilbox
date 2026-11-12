package toolinstall

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// maxProgramBytes caps the program's entry in an archive.
	maxProgramBytes = 256 << 20

	// maxUnpackedBytes caps everything an archive unpacks to, so a gzip bomb stops early.
	maxUnpackedBytes = 512 << 20
)

// limits returns the caps on the program's entry and on everything an archive unpacks to.
func (in *Installer) limits() (maxProgram, maxUnpacked int64) {
	maxProgram, maxUnpacked = in.maxProgram, in.maxUnpacked
	if maxProgram <= 0 {
		maxProgram = maxProgramBytes
	}

	if maxUnpacked <= 0 {
		maxUnpacked = maxUnpackedBytes
	}

	return maxProgram, maxUnpacked
}

// fetch downloads a into work under its own name, hashing it as it streams, and returns
// its SHA-256 in hex. Its size must be the size GitHub lists, at most maxBytes, and when
// GitHub lists a digest, the hash must match it. progress, when set, hears the bytes so far.
// The file is made through work, so a name that reaches out of it is refused, even one
// pickAssets let through.
func (in *Installer) fetch(ctx context.Context, work *os.Root, a Asset, maxBytes int64, progress func(int64)) (string, error) {
	if a.Size <= 0 || a.Size > maxBytes {
		return "", fmt.Errorf("%w: %s is listed at %d bytes, more than the %d allowed", ErrArchive, a.Name, a.Size, maxBytes)
	}

	f, err := work.OpenFile(a.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
	if err != nil {
		return "", fmt.Errorf("make the download file: %w", err)
	}

	sum := sha256.New()
	w := io.MultiWriter(f, sum)

	if progress != nil {
		w = &counter{w: w, progress: progress}
	}

	n, err := in.Download.DownloadAsset(ctx, a.URL, w, maxBytes)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return "", fmt.Errorf("download %s: %w", a.Name, err)
	}

	if n != a.Size {
		return "", fmt.Errorf("%w: %s is %d bytes, not the %d GitHub lists", ErrShortDownload, a.Name, n, a.Size)
	}

	got := hex.EncodeToString(sum.Sum(nil))

	if want, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && !strings.EqualFold(want, got) {
		return "", fmt.Errorf("%w: %s doesn't match GitHub's digest", ErrChecksum, a.Name)
	}

	return got, nil
}

// counter passes writes on and reports the bytes written so far.
type counter struct {
	w        io.Writer
	done     int64
	progress func(int64)
}

// Write passes p on and reports the total.
func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.done += int64(n)
	c.progress(c.done)

	return n, err //nolint:wrapcheck // the writer's own error, passed through
}

// verify checks the archive's SHA-256, sum, against the checksums file named checksums in
// work, which must list the archive's exact name exactly once, with 64 hex digits.
func verify(work *os.Root, checksums, archive, sum string) error {
	data, err := work.ReadFile(checksums)
	if err != nil {
		return fmt.Errorf("read the checksums: %w", err)
	}

	listed := parseChecksums(data, archive)

	switch {
	case len(listed) != 1:
		return fmt.Errorf("%w: the checksums list %s %d times, not once", ErrChecksum, archive, len(listed))
	case len(listed[0]) != sha256.Size*2 || !isHex(listed[0]):
		return fmt.Errorf("%w: the checksum for %s isn't a SHA-256", ErrChecksum, archive)
	case !strings.EqualFold(listed[0], sum):
		return fmt.Errorf("%w: %s", ErrChecksum, archive)
	}

	return nil
}

// parseChecksums returns, in order, the checksum of every line of a checksums file that
// lists archive: a line of two fields, the checksum then the name, which may carry the
// binary-mode "*" before it. The lines are read as bufio.Scanner reads them, so none past
// one longer than its limit is.
func parseChecksums(data []byte, archive string) []string {
	var listed []string

	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}

		if strings.TrimPrefix(fields[1], "*") == archive {
			listed = append(listed, fields[0])
		}
	}

	return listed
}

// isHex reports a string of hex digits.
func isHex(s string) bool {
	_, err := hex.DecodeString(s)

	return err == nil
}

// extract unpacks the program from the archive named archive in work into work, as binary
// with mode 0755, and returns its path and SHA-256 in hex. The archive must hold it as
// exactly one regular file named binary or ./binary at its root; an entry with an absolute
// path or a .. anywhere, a second entry for the program, a link in its place, an entry past
// the size cap, or an archive that unpacks past its cap is refused.
func (in *Installer) extract(work *os.Root, archive, binary string) (string, string, error) {
	maxProgram, maxUnpacked := in.limits()

	f, err := work.Open(archive)
	if err != nil {
		return "", "", fmt.Errorf("open the archive: %w", err)
	}

	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrArchive, err)
	}

	defer func() { _ = gz.Close() }()

	unpacked := &io.LimitedReader{R: gz, N: maxUnpacked + 1}
	tr := tar.NewReader(unpacked)

	var program, sum string

	for {
		hdr, err := tr.Next()

		switch {
		case errors.Is(err, io.EOF) && unpacked.N > 0:
			return finished(program, sum, binary)
		case err != nil:
			return "", "", archiveError(unpacked, maxUnpacked, err)
		}

		if program, sum, err = takeEntry(tr, hdr, binary, work, maxProgram, program, sum); err != nil {
			return "", "", archiveError(unpacked, maxUnpacked, err)
		}
	}
}

// finished returns the program an archive held, or says it held none.
func finished(program, sum, binary string) (string, string, error) {
	if program == "" {
		return "", "", fmt.Errorf("%w: it has no %s", ErrArchive, binary)
	}

	return program, sum, nil
}

// takeEntry writes the archive's current entry out when it is the program, and returns the
// program's path and sum so far. Any other entry is skipped, unless its name is unsafe.
func takeEntry(tr *tar.Reader, hdr *tar.Header, binary string, work *os.Root, maxProgram int64, program, sum string,
) (string, string, error) {
	name, err := entryName(hdr.Name)

	switch {
	case err != nil:
		return "", "", err
	case name != binary:
		return program, sum, nil
	case program != "":
		return "", "", fmt.Errorf("%w: it holds %s twice", ErrArchive, binary)
	default:
		return writeProgram(tr, hdr, binary, work, maxProgram)
	}
}

// entryName returns an archive entry's name, cleaned, refusing one that could reach out of
// the folder it unpacks into.
func entryName(raw string) (string, error) {
	if raw == "" || path.IsAbs(raw) || strings.Contains(raw, `\`) || strings.Contains(raw, "\x00") {
		return "", fmt.Errorf("%w: an entry is named %q", ErrArchive, raw)
	}

	for part := range strings.SplitSeq(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: an entry is named %q", ErrArchive, raw)
		}
	}

	return path.Clean(raw), nil
}

// writeProgram writes the program's entry into work, after checking it is a regular file
// within the size cap, and returns its path and SHA-256.
func writeProgram(tr *tar.Reader, hdr *tar.Header, binary string, work *os.Root, maxProgram int64) (string, string, error) {
	switch {
	case hdr.Typeflag != tar.TypeReg:
		return "", "", fmt.Errorf("%w: %s isn't a plain file in it", ErrArchive, binary)
	case hdr.Size <= 0 || hdr.Size > maxProgram:
		return "", "", fmt.Errorf("%w: %s is %d bytes", ErrArchive, binary, hdr.Size)
	}

	path := filepath.Join(work.Name(), binary)

	out, err := work.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, execPerm)
	if err != nil {
		return "", "", fmt.Errorf("write %s: %w", binary, err)
	}

	sum := sha256.New()

	n, err := io.Copy(io.MultiWriter(out, sum), io.LimitReader(tr, maxProgram+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}

	switch {
	case err != nil:
		return "", "", fmt.Errorf("write %s: %w", binary, err)
	case n != hdr.Size:
		return "", "", fmt.Errorf("%w: %s is %d bytes, not the %d its header says", ErrArchive, binary, n, hdr.Size)
	}

	return path, hexSum(sum), nil
}

// hexSum returns a hash's sum in hex.
func hexSum(h hash.Hash) string {
	return hex.EncodeToString(h.Sum(nil))
}

// archiveError words a failure to read the archive, naming one that unpacks past its cap.
func archiveError(unpacked *io.LimitedReader, maxUnpacked int64, err error) error {
	if unpacked.N <= 0 {
		return fmt.Errorf("%w: it unpacks to more than %d bytes", ErrArchive, maxUnpacked)
	}

	if errors.Is(err, ErrArchive) {
		return err
	}

	return fmt.Errorf("%w: %w", ErrArchive, err)
}
