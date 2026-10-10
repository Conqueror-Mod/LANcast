package store

/*
 * Change notices, for open windows (ADR 0079).
 *
 * The store methods that change what a screen shows announce it themselves,
 * after their write commits. A missing announcement is the same quiet bug as a
 * missing invalidation in the client, moved to the server, and putting it here
 * rather than at each caller is what stops a new caller forgetting: a worker,
 * a handler and a test all change peers through the same methods.
 *
 * The store knows topic names and nothing about who listens. internal/api
 * registers the one listener when it is built.
 */

// The topics the store announces. internal/api re-exports them as the event
// stream's topics, and docs/api.md lists them.
const (
	ChangePeers     = "peers"
	ChangeLibraries = "libraries"
)

type changeHook = func(topic string)

// OnChange registers fn to hear every announcement. A later call replaces the
// earlier one. Nil stops announcements.
func (s *Store) OnChange(fn func(topic string)) {
	if fn == nil {
		s.changed.Store(nil)
		return
	}
	s.changed.Store(&fn)
}

func (s *Store) announce(topic string) {
	if p := s.changed.Load(); p != nil {
		(*p)(topic)
	}
}
