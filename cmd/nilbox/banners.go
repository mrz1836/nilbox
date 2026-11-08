//go:build desktop

package main

import "github.com/mrz1836/nilbox/internal/notify"

// poster is the notification center core posts banners to, or nil when this build cannot
// show them; a nil interface, never a nil *banners, so core sees that there is none.
func (b *banners) poster() notify.Poster {
	if b == nil {
		return nil
	}

	return b
}
