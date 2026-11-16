//go:build windows && amd64

// msdf-probe verifies a locally rebuilt bridge before it is packaged.
package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/egoist/mygo/ui"
)

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: msdf-probe absolute-dll-path SHA256")
	}
	path, want := os.Args[1], os.Args[2]
	if !filepath.IsAbs(path) {
		return fmt.Errorf("DLL path must be absolute")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%X", sha256.Sum256(data)) != want {
		return fmt.Errorf("DLL SHA mismatch before loading")
	}
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return err
	}
	defer dll.Release()
	for name, want := range map[string]uintptr{"cm_abi": 1, "cm_revision": 4} {
		proc, err := dll.FindProc(name)
		if err != nil {
			return err
		}
		got, _, _ := proc.Call()
		if got != want {
			return fmt.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	post := func(func()) {} // No glyph jobs are submitted by this probe.
	d, err := ui.OpenDynamicMSDF(path, want, post)
	if err != nil {
		return err
	}
	d.Close()
	if _, err = ui.OpenDynamicMSDF(path, strings.Repeat("0", 64), post); err == nil {
		return fmt.Errorf("incorrect SHA was accepted")
	}
	if _, err = ui.OpenDynamicMSDF(path+".missing", want, post); err == nil {
		return fmt.Errorf("missing DLL was accepted")
	}
	fmt.Println("PASS: SHA256, ABI1/revision4, runtime open, wrong-SHA/missing-DLL rejection")
	return nil
}
