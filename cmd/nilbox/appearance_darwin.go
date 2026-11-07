//go:build desktop && darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>

// The app's appearance: follow the system, or pin light or dark.
enum { appearanceSystem = 0, appearanceLight = 1, appearanceDark = 2 };

// setAppAppearance sets the appearance every window, menu, and web view of the app draws
// with; nil follows the system. AppKit requires the main thread.
static void setAppAppearance(int appearance) {
	@autoreleasepool {
		NSAppearance *named = nil;

		if (appearance == appearanceLight) {
			named = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
		} else if (appearance == appearanceDark) {
			named = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
		}

		[NSApp setAppearance:named];
	}
}
*/
import "C"

import "github.com/mrz1836/nilbox/internal/settings"

// setNativeAppearance makes the app's native appearance follow a theme: the window frame,
// its menus (a <select>'s popup among them), and the web view's prefers-color-scheme
// all take the pinned light or dark appearance, or follow the system for ThemeSystem.
// It must run on the main thread.
func setNativeAppearance(theme string) {
	appearance := C.appearanceSystem

	switch theme {
	case settings.ThemeLight:
		appearance = C.appearanceLight
	case settings.ThemeDark:
		appearance = C.appearanceDark
	}

	C.setAppAppearance(C.int(appearance))
}
