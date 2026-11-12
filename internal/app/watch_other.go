//go:build !windows

package app

// watchFolder does not watch folders here: the library is read again when
// its page asks, or the listener does.
func watchFolder(dir string, changed func()) (stop func()) { return nil }
