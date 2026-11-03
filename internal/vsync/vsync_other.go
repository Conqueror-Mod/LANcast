//go:build !windows

package vsync

// Waiter is never made off Windows; the page keeps its own frame clock.
type Waiter struct{}

// Open is unsupported here.
func Open(string) (*Waiter, error) { return nil, ErrUnsupported }

// Wait is unsupported here.
func (*Waiter) Wait() error { return ErrUnsupported }

// Close does nothing.
func (*Waiter) Close() {}
