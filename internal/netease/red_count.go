package netease

import (
	"fmt"
	"net/url"
	"strconv"
)

// RedCount is how many listeners have given a song a heart.
type RedCount struct {
	Count       int    `json:"count"`
	Description string `json:"countDesc"`
}

// SongRedCount reads the public song count, independently of the current
// listener's own heart.
func SongRedCount(id int64) (RedCount, error) {
	var res struct {
		Code int      `json:"code"`
		Data RedCount `json:"data"`
	}
	if err := get("/api/song/red/count", url.Values{"songId": {strconv.FormatInt(id, 10)}}, &res); err != nil {
		return RedCount{}, err
	}
	if res.Code != 200 || res.Data.Count < 0 {
		return RedCount{}, fmt.Errorf("netease: red count: code %d", res.Code)
	}
	return res.Data, nil
}
