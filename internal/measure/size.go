package measure

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// megabyte is a decimal megabyte, the unit the size target is stated in.
const megabyte = 1_000_000

// SizeReport is the size of the built app.
type SizeReport struct {
	// BinaryBytes is the size of the app's executable.
	BinaryBytes int64

	// BundleBytes is the total size of every file in the app bundle.
	BundleBytes int64
}

// Sizes measures the executable exe and the bundle directory that contains it.
func Sizes(bundle, exe string) (SizeReport, error) {
	info, err := os.Stat(exe)
	if err != nil {
		return SizeReport{}, fmt.Errorf("read the binary size: %w", err)
	}

	report := SizeReport{BinaryBytes: info.Size()}

	err = filepath.WalkDir(bundle, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			return err
		}

		report.BundleBytes += fi.Size()

		return nil
	})
	if err != nil {
		return SizeReport{}, fmt.Errorf("read the bundle size: %w", err)
	}

	return report, nil
}

// Lines renders the report as key=value lines, in bytes and decimal megabytes.
func (r SizeReport) Lines() []string {
	return []string{
		fmt.Sprintf("size.binary_bytes=%d binary_mb=%.1f", r.BinaryBytes, float64(r.BinaryBytes)/megabyte),
		fmt.Sprintf("size.bundle_bytes=%d bundle_mb=%.1f", r.BundleBytes, float64(r.BundleBytes)/megabyte),
	}
}
