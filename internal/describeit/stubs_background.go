package describeit

import "context"

// The background work of the conversation's half, until it is built.

// start readies the describe folder as the app starts: a turn a crash left running closed
// from its stream, conversations older than 30 days pruned, and the snapshot and the
// instructions written.
func (s *Service) start(_ context.Context) {}

// keepContext writes the snapshot again after each burst of changes, until ctx ends.
func (s *Service) keepContext(ctx context.Context, _ <-chan struct{}) {
	<-ctx.Done()
}

// shutdown stops a turn still running as the app quits.
func (s *Service) shutdown() {}
