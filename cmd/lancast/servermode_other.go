//go:build !windows

package main

// serverModeBindings offers nothing off Windows: there the service is a unit
// the operator wrote, in a folder they chose (ADR 0092).
func (l *launcher) serverModeBindings() map[string]any { return nil }
