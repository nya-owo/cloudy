//go:build !windows

package app

import "errors"

// canRegisterOpenWith reports whether Cloudy can list itself to open songs:
// elsewhere its package does.
const canRegisterOpenWith = false

func registerOpenWith() error { return errors.New("not supported") }
