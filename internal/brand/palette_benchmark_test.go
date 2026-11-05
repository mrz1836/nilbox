package brand

import "testing"

func BenchmarkContrastRatio(b *testing.B) {
	for b.Loop() {
		_, _ = ContrastRatio(Dark.Accent, Dark.Background)
	}
}
