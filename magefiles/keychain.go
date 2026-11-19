//go:build mage

package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

// errKeychainList is returned when security prints a line of the keychain search list
// that isn't a quoted path.
var errKeychainList = errors.New("unexpected line in the keychain search list")

// withKeychain runs fn with keychain first on the user's keychain search list, and puts
// the list back exactly as it was afterwards, whether fn succeeds or fails, or the target
// is interrupted. codesign finds an identity only in a keychain on that list, even when
// it is named with --keychain. With no keychain it just runs fn.
func (r runner) withKeychain(keychain string, fn func() error) (err error) {
	if keychain == "" {
		return fn()
	}

	restore, err := r.useKeychain(keychain)
	if err != nil {
		return err
	}

	stop := onInterrupt(func() { _ = restore() })

	defer func() {
		stop()

		if restoreErr := restore(); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("put the keychain search list back: %w", restoreErr))
		}
	}()

	return fn()
}

// useKeychain puts keychain, an absolute path, first on the user's keychain search list
// and returns a function that sets the list back to exactly what it was, once however
// often it is called.
func (r runner) useKeychain(keychain string) (func() error, error) {
	original, err := r.keychainSearchList()
	if err != nil {
		return nil, err
	}

	list := []string{keychain}

	for _, k := range original {
		if k != keychain {
			list = append(list, k)
		}
	}

	if err = r.setKeychainSearchList(list); err != nil {
		// The list may have changed partway, so set it back all the same.
		return nil, errors.Join(fmt.Errorf("add the keychain to the search list: %w", err), r.setKeychainSearchList(original))
	}

	var (
		once       sync.Once
		restoreErr error
	)

	return func() error {
		once.Do(func() { restoreErr = r.setKeychainSearchList(original) })

		return restoreErr
	}, nil
}

// keychainSearchList returns the user's keychain search list, in order.
func (r runner) keychainSearchList() ([]string, error) {
	out, err := r.output("security", "list-keychains", "-d", "user")
	if err != nil {
		return nil, fmt.Errorf("read the keychain search list: %w", err)
	}

	return parseKeychainList(out)
}

// parseKeychainList reads the keychain search list as security prints it: one path a
// line, each in double quotes and indented. A path may hold spaces, so a line is read
// whole, never split on them.
func parseKeychainList(out string) ([]string, error) {
	list := []string{}

	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if len(line) < 3 || line[0] != '"' || line[len(line)-1] != '"' {
			return nil, fmt.Errorf("%w: %q", errKeychainList, line)
		}

		list = append(list, line[1:len(line)-1])
	}

	return list, nil
}

// setKeychainSearchList makes list the user's keychain search list, in order.
func (r runner) setKeychainSearchList(list []string) error {
	if err := r.run("security", append([]string{"list-keychains", "-d", "user", "-s"}, list...)...); err != nil {
		return fmt.Errorf("set the keychain search list: %w", err)
	}

	return nil
}

// onInterrupt runs fn when the process is interrupted before stop is called, and lets
// the target clean up after itself where it can:
//
//   - Control-C (SIGINT): fn runs, and the signal is left to mage, which stops the target
//     and gives it time to clean up. Control-C at a terminal stops the build's tools too,
//     so the target fails on its own and its deferred cleanup runs. Sending SIGINT again
//     would be mage's second Control-C, which exits at once and skips that cleanup.
//   - SIGTERM or SIGHUP, which mage leaves alone: fn runs, then the signal is delivered
//     again with its default action, ending the process. What it leaves (the slices and
//     the disk image's folder) is in the release's own folder, which the next run clears.
//
// stop ends the hook; after it, fn never runs.
func onInterrupt(fn func()) (stop func()) {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})

	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		select {
		case sig := <-signals:
			fn()
			signal.Stop(signals)

			if s, ok := sig.(syscall.Signal); ok && s != syscall.SIGINT {
				_ = syscall.Kill(os.Getpid(), s)
			}
		case <-done:
		}
	}()

	var once sync.Once

	return func() {
		once.Do(func() {
			signal.Stop(signals)
			close(done)
		})
	}
}
