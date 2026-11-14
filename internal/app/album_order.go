package app

import (
	"path/filepath"
	"strings"
	"unicode"

	"netease-music-mygo/internal/netease"
)

// albumOrder orders two songs of one album as the album does: by disc,
// then by number, and those the tags leave unnumbered by their files'
// names, or else their own, numbers in them read as numbers (02 before 10).
func albumOrder(x, y *netease.Track) int {
	if x.Cd != y.Cd {
		return cmpUnknownLast(int(x.Cd), int(y.Cd))
	}
	if x.No != y.No {
		return cmpUnknownLast(int(x.No), int(y.No))
	}
	if c := naturalCompare(sortName(x), sortName(y)); c != 0 {
		return c
	}
	return strings.Compare(x.Name, y.Name)
}

// cmpUnknownLast compares two numbers, 0, unknown, after the others.
func cmpUnknownLast(x, y int) int {
	switch {
	case x == y:
		return 0
	case x == 0:
		return 1
	case y == 0:
		return -1
	case x < y:
		return -1
	}
	return 1
}

// sortName is what names t for its order in an album: its file's name for
// a file, else its title.
func sortName(t *netease.Track) string {
	if t.LocalPath != "" {
		return strings.TrimSuffix(filepath.Base(t.LocalPath), filepath.Ext(t.LocalPath))
	}
	return t.Name
}

// naturalCompare compares x and y with the runs of digits in them read as
// numbers, ignoring case: "2 b" before "10 a".
func naturalCompare(x, y string) int {
	xs, ys := []rune(strings.ToLower(x)), []rune(strings.ToLower(y))
	i, j := 0, 0
	for i < len(xs) && j < len(ys) {
		if unicode.IsDigit(xs[i]) && unicode.IsDigit(ys[j]) {
			si, sj := i, j
			for i < len(xs) && unicode.IsDigit(xs[i]) {
				i++
			}
			for j < len(ys) && unicode.IsDigit(ys[j]) {
				j++
			}
			a := strings.TrimLeft(string(xs[si:i]), "0")
			b := strings.TrimLeft(string(ys[sj:j]), "0")
			if len(a) != len(b) {
				if len(a) < len(b) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(a, b); c != 0 {
				return c
			}
			continue
		}
		if xs[i] != ys[j] {
			if xs[i] < ys[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case len(xs)-i < len(ys)-j:
		return -1
	case len(xs)-i > len(ys)-j:
		return 1
	}
	return 0
}
