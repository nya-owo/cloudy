//go:build !windows

package app

import "image"

// Only Windows draws its own tray menu: these are its platform parts.

func trayHostAtCursor() (host uintptr, overflow bool)                         { return 0, false }
func watchTrayPopup(hwnd, host uintptr, done <-chan struct{}, dismiss func()) {}
func captureBehind() func(hwnd uintptr) image.Image                           { return nil }
