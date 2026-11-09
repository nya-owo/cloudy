package app

import (
	"os"
	"path/filepath"
)

// writeFileAtomic writes b to path through a file beside it, renamed over
// path once it is whole, so a crash or a power cut halfway through leaves
// the old file, never half of the new one.
func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	part := path + ".part"
	if err := os.WriteFile(part, b, perm); err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Rename(part, path); err != nil {
		os.Remove(part)
		return err
	}
	return nil
}
