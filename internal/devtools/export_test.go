package devtools

// CutShort is cutShort, for the tests: why an answer ended before its end line.
func CutShort(err error) error {
	return cutShort(err)
}

// Followers is how many follow the feed, for the tests.
func (f *Feed[T]) Followers() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.followers)
}
