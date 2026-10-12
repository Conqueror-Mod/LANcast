// Package lancast holds what a build knows about itself: the notice that
// travels with it and the notes of each release, embedded at build time so
// Settings › About describes the running version without asking any server
// (no phone-home).
//
// A package at the repository root because that is the only place go:embed
// can reach NOTICE from: an embed cannot climb out of its own directory.
package lancast

import (
	"embed"
	"strings"
)

// Notice is NOTICE: the licence and every third-party component in a build.
//
//go:embed NOTICE
var Notice string

//go:embed docs/releases
var releases embed.FS

// ReleaseNotes returns the committed notes for a version ("0.9.79" or
// "v0.9.79"), and false when there are none — a development build, or a
// release cut before notes were kept here.
func ReleaseNotes(version string) (string, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" || strings.ContainsAny(v, `/\`) {
		return "", false
	}
	b, err := releases.ReadFile("docs/releases/v" + v + ".md")
	if err != nil {
		return "", false
	}
	return string(b), true
}
