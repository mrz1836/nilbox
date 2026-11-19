package devtools

import "sync"

// followerBuffer is how many entries a follower can fall behind before it misses some:
// the app never waits on a slow reader.
const followerBuffer = 256

// Feed keeps the last entries of one kind, oldest first, and hands each new one to every
// follower as it is added. It is safe for concurrent use.
type Feed[T any] struct {
	mu        sync.Mutex
	capacity  int
	entries   []T
	followers map[int]chan T
	next      int
}

// NewFeed returns a feed that keeps the last capacity entries.
func NewFeed[T any](capacity int) *Feed[T] {
	return &Feed[T]{capacity: max(capacity, 1), followers: map[int]chan T{}}
}

// Add keeps entry, dropping the oldest once the feed is full, and hands it to every
// follower that has room for it.
func (f *Feed[T]) Add(entry T) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.entries = append(f.entries, entry)

	// The slice grows to twice the capacity before the oldest half goes, so adding costs
	// a copy only once every capacity entries.
	if len(f.entries) >= 2*f.capacity {
		f.entries = append([]T(nil), f.entries[len(f.entries)-f.capacity:]...)
	}

	for _, follower := range f.followers {
		select {
		case follower <- entry:
		default:
		}
	}
}

// Last returns the last n entries kept, oldest first; n of 0 or less returns them all.
func (f *Feed[T]) Last(n int) []T {
	f.mu.Lock()
	defer f.mu.Unlock()

	kept := f.entries
	if len(kept) > f.capacity {
		kept = kept[len(kept)-f.capacity:]
	}

	if n > 0 && len(kept) > n {
		kept = kept[len(kept)-n:]
	}

	return append([]T{}, kept...)
}

// Len returns how many entries the feed keeps now.
func (f *Feed[T]) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return min(len(f.entries), f.capacity)
}

// Follow returns a channel that receives every entry added from now on, and the function
// that stops it and closes the channel. A follower that falls followerBuffer entries
// behind misses the entries added while it is full.
func (f *Feed[T]) Follow() (<-chan T, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id := f.next
	f.next++

	follower := make(chan T, followerBuffer)
	f.followers[id] = follower

	var once sync.Once

	return follower, func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()

			delete(f.followers, id)
			close(follower)
		})
	}
}
