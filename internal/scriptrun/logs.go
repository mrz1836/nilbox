package scriptrun

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// The files of a run, under the log folder: <id> and one of these.
const (
	logSuffix     = ".log"
	pidSuffix     = ".pid"
	exitSuffix    = ".exit"
	exitTmpSuffix = ".exit.tmp"
	compactSuffix = ".log.compact"
	keptSuffix    = ".kept"
)

// readBlock is how much of a log one read takes.
const readBlock = 64 << 10

// cutWindow is how far around a compaction's cut a line break is looked for.
const cutWindow = 64 << 10

// runFile matches the name of a file of a run: its ID, then what the file is.
var runFile = regexp.MustCompile(`^([0-9]+)(\.log|\.pid|\.exit|\.exit\.tmp|\.log\.compact|\.kept)$`)

// logFiles are where a run's files are.
type logFiles struct {
	dir string
}

// log returns the path of a run's log.
func (f logFiles) log(id int64) string { return f.path(id, logSuffix) }

// pid returns the path of a terminal run's PID file.
func (f logFiles) pid(id int64) string { return f.path(id, pidSuffix) }

// exit returns the path of a terminal run's exit file.
func (f logFiles) exit(id int64) string { return f.path(id, exitSuffix) }

// kept returns the path of the file of what a run keeps beside its record (Spec.Keep).
func (f logFiles) kept(id int64) string { return f.path(id, keptSuffix) }

// path returns the path of a run's file with suffix.
func (f logFiles) path(id int64, suffix string) string {
	return filepath.Join(f.dir, strconv.FormatInt(id, 10)+suffix)
}

// ensureDir creates the log folder, readable only by the user.
func (f logFiles) ensureDir() error {
	if err := os.MkdirAll(f.dir, appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("create the runs' log folder: %w", err)
	}

	return nil
}

// create creates a run's log for its program to append to, readable only by the user. A
// file left at that name by a run the history no longer holds, as after the database was
// set aside, is removed first; nothing else is ever opened in its place.
func (f logFiles) create(id int64) (*os.File, error) {
	path := f.log(id)

	file, err := openNew(path)
	if errors.Is(err, fs.ErrExist) {
		if err = os.Remove(path); err == nil {
			file, err = openNew(path)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("create the run's log: %w", err)
	}

	return file, nil
}

// writeKept writes what a run keeps beside its record into its own file, readable only by
// the user, in place of any file a run the history no longer holds left at that name.
func (f logFiles) writeKept(id int64, data []byte) error {
	path := f.kept(id)

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("keep the run's state: %w", err)
	}

	file, err := openNew(path)
	if err != nil {
		return fmt.Errorf("keep the run's state: %w", err)
	}

	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		_ = os.Remove(path)

		return fmt.Errorf("keep the run's state: %w", err)
	}

	return nil
}

// clear removes a run's files, if there are any.
func (f logFiles) clear(id int64) error {
	var errs []error

	for _, suffix := range []string{logSuffix, pidSuffix, exitSuffix, exitTmpSuffix, compactSuffix, keptSuffix} {
		if err := os.Remove(f.path(id, suffix)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// orphans returns the IDs of the runs whose files are in the log folder, other than
// those of keep.
func (f logFiles) orphans(keep map[int64]bool) ([]int64, error) {
	entries, err := os.ReadDir(f.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read the runs' log folder: %w", err)
	}

	seen := map[int64]bool{}

	var out []int64

	for _, entry := range entries {
		m := runFile.FindStringSubmatch(entry.Name())
		if m == nil || !entry.Type().IsRegular() {
			continue
		}

		id, parseErr := strconv.ParseInt(m[1], 10, 64)
		if parseErr != nil || keep[id] || seen[id] {
			continue
		}

		seen[id] = true
		out = append(out, id)
	}

	return out, nil
}

// size returns the size of a run's log, or 0 when there is none.
func (f logFiles) size(id int64) int64 {
	info, err := os.Stat(f.log(id))
	if err != nil {
		return 0
	}

	return info.Size()
}

// readExit reads a terminal run's exit status from its exit file. ok is false while the
// file doesn't exist; a file that doesn't hold a status reads as known false.
func (f logFiles) readExit(id int64) (code int, known, ok bool) {
	body, err := os.ReadFile(f.exit(id))
	if err != nil {
		return 0, false, false
	}

	code, err = strconv.Atoi(strings.TrimSpace(string(body)))

	return code, err == nil, true
}

// readPID reads the PID a terminal run's script recorded, or 0 while there is none.
func (f logFiles) readPID(id int64) int {
	body, err := os.ReadFile(f.pid(id))
	if err != nil {
		return 0
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || !runGroup(pid) {
		return 0
	}

	return pid
}

// compaction is how a finished log is cut down: a log over over bytes keeps its first
// head bytes and its last tail bytes, with a marker line between them.
type compaction struct {
	over, head, tail int64
}

// compact cuts the log at path down, when it is over c.over bytes, and returns its size.
// The cuts fall at line breaks near the sizes, or else between characters. A log that
// isn't there has size 0.
func (c compaction) compact(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("read the run's log: %w", err)
	}

	size := info.Size()
	if size <= c.over || c.head+c.tail >= size {
		return size, nil
	}

	src, err := os.Open(path) //nolint:gosec // the run's own log, under the app's data folder
	if err != nil {
		return 0, fmt.Errorf("read the run's log: %w", err)
	}

	defer func() { _ = src.Close() }()

	headEnd := headCut(src, c.head)
	tailStart := tailCut(src, size-c.tail, size)

	return c.rewrite(path, src, headEnd, tailStart, size)
}

// rewrite writes the log at path again as its bytes before headEnd, a marker, and its
// bytes from tailStart, through a new file renamed over it.
func (c compaction) rewrite(path string, src *os.File, headEnd, tailStart, size int64) (int64, error) {
	tmp := path[:len(path)-len(logSuffix)] + compactSuffix
	_ = os.Remove(tmp) // one left by a compaction cut short

	dst, err := openNew(tmp)
	if err != nil {
		return 0, fmt.Errorf("compact the run's log: %w", err)
	}

	marker := fmt.Sprintf("[%s of output left out here: a long log keeps its first %s and its last %s.]\n",
		sizeText(tailStart-headEnd), sizeText(c.head), sizeText(c.tail))
	if headEnd > 0 && !endsLine(src, headEnd) {
		marker = "\n" + marker
	}

	written, err := copyParts(dst, io.NewSectionReader(src, 0, headEnd), strings.NewReader(marker),
		io.NewSectionReader(src, tailStart, size-tailStart))
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(tmp, path)
	}

	if err != nil {
		_ = os.Remove(tmp)

		return 0, fmt.Errorf("compact the run's log: %w", err)
	}

	return written, nil
}

// copyParts copies each part into dst in turn and returns how much it wrote.
func copyParts(dst io.Writer, parts ...io.Reader) (int64, error) {
	var written int64

	for _, part := range parts {
		n, err := io.Copy(dst, part)
		written += n

		if err != nil {
			return written, err
		}
	}

	return written, nil
}

// headCut returns where the head of a log at most limit bytes long ends: after the last
// line break near limit, or else at the start of the character at limit.
func headCut(src io.ReaderAt, limit int64) int64 {
	from := max(0, limit-cutWindow)
	buf := make([]byte, limit-from)
	n, _ := src.ReadAt(buf, from)
	buf = buf[:n]

	if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
		return from + int64(i) + 1
	}

	return from + int64(completeEnd(buf))
}

// tailCut returns where the tail of a log that starts no earlier than from begins: after
// the first line break near from, or else at the start of the next character.
func tailCut(src io.ReaderAt, from, size int64) int64 {
	buf := make([]byte, min(cutWindow, size-from))
	n, _ := src.ReadAt(buf, from)
	buf = buf[:n]

	if i := bytes.IndexByte(buf, '\n'); i >= 0 {
		return from + int64(i) + 1
	}

	return from + int64(partialStart(buf))
}

// endsLine reports whether the byte before at is a line break.
func endsLine(src io.ReaderAt, at int64) bool {
	b := make([]byte, 1)
	n, _ := src.ReadAt(b, at-1)

	return n == 1 && b[0] == '\n'
}

// completeEnd returns how much of buf holds whole characters: all of it, or all up to a
// character its end cuts short.
func completeEnd(buf []byte) int {
	for back := 1; back <= utf8.UTFMax && back <= len(buf); back++ {
		i := len(buf) - back
		if !utf8.RuneStart(buf[i]) {
			continue
		}

		if utf8.FullRune(buf[i:]) {
			return len(buf)
		}

		return i
	}

	return len(buf)
}

// sizeText writes a size the way the window shows it: "4 MB", "512 KB", "12 bytes".
func sizeText(n int64) string {
	const kb, mb = 1 << 10, 1 << 20

	switch {
	case n >= mb && n%mb == 0:
		return strconv.FormatInt(n/mb, 10) + " MB"
	case n >= mb:
		return strconv.FormatFloat(float64(n)/mb, 'f', 1, 64) + " MB"
	case n >= kb && n%kb == 0:
		return strconv.FormatInt(n/kb, 10) + " KB"
	case n >= kb:
		return strconv.FormatFloat(float64(n)/kb, 'f', 1, 64) + " KB"
	case n == 1:
		return "1 byte"
	default:
		return strconv.FormatInt(n, 10) + " bytes"
	}
}

// openNew creates path for appending, readable only by the user; it fails when anything
// is already there, a link included.
func openNew(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, appdata.PrivateFilePerm) //nolint:gosec // a path the engine built from a run's ID
}
