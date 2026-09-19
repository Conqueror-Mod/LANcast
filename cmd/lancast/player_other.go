//go:build !windows

package main

import "lancast/internal/clientwindow"

// nativePlayer has no implementation off Windows; the page keeps the browser's
// player (ADR 0067).
type nativePlayer struct{ origin, pin string }

func (n *nativePlayer) attach(clientwindow.Controller) {}

func (n *nativePlayer) bindings() map[string]any { return nil }
