package netease

import (
	"bytes"
	"strconv"
	"strings"
)

// Num is a number NetEase sends as a number or as a string, as "01" or
// "1/2" for a disc, read as 0 when it is neither.
type Num int

func (n *Num) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	s := strings.Trim(string(b), `"`)
	s, _, _ = strings.Cut(strings.TrimSpace(s), "/")
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 0 {
		v = 0
	}
	*n = Num(v)
	return nil
}
