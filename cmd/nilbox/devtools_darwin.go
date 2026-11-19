//go:build desktop && devtools && darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework WebKit

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// snapshot is a picture a web view took, as PNG bytes the caller frees, or why it took
// none.
typedef struct {
	void *bytes;
	long length;
	char error[512];
} snapshot;

// findWebView returns the first web view in view's tree, or nil.
static WKWebView *findWebView(NSView *view) {
	if ([view isKindOfClass:[WKWebView class]]) {
		return (WKWebView *)view;
	}

	for (NSView *sub in view.subviews) {
		WKWebView *found = findWebView(sub);
		if (found != nil) {
			return found;
		}
	}

	return nil;
}

// pngOf returns image as PNG bytes, malloc'd, and their length.
static void *pngOf(NSImage *image, long *length) {
	CGImageRef cg = [image CGImageForProposedRect:NULL context:nil hints:nil];
	if (cg == NULL) {
		return NULL;
	}

	NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:cg];
	NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
	void *bytes = malloc(png.length);

	if (bytes != NULL) {
		memcpy(bytes, png.bytes, png.length);
		*length = (long)png.length;
	}

	[rep release];

	return bytes;
}

// takeSnapshot has the web view in window take a picture of what it draws, on the main
// thread, and waits for it. It must not run on the main thread itself.
static snapshot takeSnapshot(void *window) {
	__block snapshot result = { NULL, 0, "" };
	dispatch_semaphore_t done = dispatch_semaphore_create(0);

	dispatch_async(dispatch_get_main_queue(), ^{
		WKWebView *webView = findWebView([(NSWindow *)window contentView]);
		if (webView == nil) {
			snprintf(result.error, sizeof result.error, "the window has no web view");
			dispatch_semaphore_signal(done);
			return;
		}

		[webView takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image, NSError *error) {
			@autoreleasepool {
				if (image != nil) {
					result.bytes = pngOf(image, &result.length);
				}

				if (result.bytes == NULL) {
					const char *why = error != nil ? error.localizedDescription.UTF8String : "the web view drew nothing";
					snprintf(result.error, sizeof result.error, "%s", why);
				}
			}

			dispatch_semaphore_signal(done);
		}];
	});

	dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
	dispatch_release(done);

	return result;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/mrz1836/nilbox/internal/devtools"
)

// snapshotWebView returns a picture, as PNG, of what the web view in window, a native
// window, draws: the page as it is on screen, at the screen's scale. It waits for the
// main thread, where the web view takes it, so it must not run there.
func snapshotWebView(window unsafe.Pointer) ([]byte, error) {
	if window == nil {
		return nil, devtools.ErrNoWindow
	}

	shot := C.takeSnapshot(window)
	if shot.bytes == nil {
		return nil, fmt.Errorf("%w: %s", errNoSnapshot, C.GoString(&shot.error[0]))
	}

	defer C.free(shot.bytes)

	return C.GoBytes(shot.bytes, C.int(shot.length)), nil
}
