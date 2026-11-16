// Package sharedopen opens a file for reading without locking its owner out.
//
// Anything the server holds open for longer than a moment — a film being
// streamed, a comic on the shelf — must still be deletable and replaceable by
// the person who owns it. On Windows os.Open does not allow that; Open does.
package sharedopen
