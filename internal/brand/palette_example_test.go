package brand_test

import (
	"fmt"

	"github.com/mrz1836/nilbox/internal/brand"
)

// ExampleContrastRatio shows how to check a color pair against the WCAG AA text minimum.
func ExampleContrastRatio() {
	ratio, err := brand.ContrastRatio(brand.Dark.Text, brand.Dark.Background)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Printf("%.1f:1 meets AA: %t\n", ratio, ratio >= brand.MinTextContrast)
	// Output: 15.3:1 meets AA: true
}
