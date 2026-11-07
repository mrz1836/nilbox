//go:build desktop && darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>

// activateApp brings the app to the front, as a click on its menu bar icon or one of its
// notifications asks. AppKit requires the main thread.
static void activateApp(void) {
	@autoreleasepool {
		if (@available(macOS 14.0, *)) {
			[NSApp activate];
		} else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
			[NSApp activateIgnoringOtherApps:YES];
#pragma clang diagnostic pop
		}
	}
}

// menuSectionHeaders reports whether menus can show native section headers (macOS 14+).
static bool menuSectionHeaders(void) {
	if (@available(macOS 14.0, *)) {
		return true;
	}

	return false;
}
*/
import "C"

// activateNative brings the app to the front. It must run on the main thread.
func activateNative() {
	C.activateApp()
}

// nativeSectionHeaders reports whether menus can show native section headers.
func nativeSectionHeaders() bool {
	return bool(C.menuSectionHeaders())
}
