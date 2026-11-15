//go:build !darwin

package desktop

// unquarantine does nothing outside macOS, where no file carries a quarantine mark.
func unquarantine(string) error {
	return nil
}

// translocates reports nothing outside macOS, where no app is translocated.
func translocates(string) (bool, error) {
	return false, nil
}
