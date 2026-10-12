//go:build !slowscan

package scan

// slowWalk is nil: a real build walks at full speed. See slowscan.go.
var slowWalk func(libraryID int64)
