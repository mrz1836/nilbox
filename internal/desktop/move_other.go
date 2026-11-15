//go:build !darwin

package desktop

// unquarantine does nothing outside macOS, where no file carries a quarantine mark.
func unquarantine(string) error {
	return nil
}
