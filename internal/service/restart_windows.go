//go:build windows

package service

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Restart stops the service, waits for it to actually be stopped, and starts it
// again.
//
// This exists because a Windows service cannot restart itself, and a staged
// update is applied on the way down (internal/selfupdate). Without it the
// updater could stage a new version and then had nowhere to go: the panel said
// "it takes effect the next time the server starts", and nothing ever restarts
// a service. The only way through was an elevated Stop-Service, which applied
// the update and left the machine with LANcast not running at all.
//
// The wait is the part that matters. Start on a service still in STOP_PENDING
// fails, and the failure would be reported after the old version had already
// gone — the worst possible moment to be wrong.
func (windowsManager) Restart(timeout time.Duration) error {
	if err := (windowsManager{}).Stop(); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		var state svc.State
		err := withService(func(s *mgr.Service) error {
			st, err := s.Query()
			if err != nil {
				return err
			}
			state = st.State
			return nil
		})
		if err != nil {
			return fmt.Errorf("waiting for the service to stop: %w", err)
		}
		if state == svc.Stopped {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service did not stop within %s (state %s)", timeout, svcStateName(state))
		}
		time.Sleep(250 * time.Millisecond)
	}
	return (windowsManager{}).Start()
}
