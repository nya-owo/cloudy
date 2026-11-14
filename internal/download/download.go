// Package download keeps the songs the listener saves on disk: where each
// one is, and how to fetch another.
package download

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"netease-music-mygo/internal/netease"
)

// Item is a song saved on disk, and what the app knew of it when it was.
type Item struct {
	Track netease.Track `json:"track"`
	Path  string        `json:"path"`
	Level string        `json:"level,omitempty"`
	Size  int64         `json:"size,omitempty"`
	At    time.Time     `json:"at"`
	// Tagged says its file carries its title, cover, lyrics and NetEase ID,
	// which Cloudy wrote into it.
	Tagged bool `json:"tagged,omitempty"`
	// Numbered says the file's track and disc numbers were read into
	// Track (No, Cd), which indexes before 1.2.13 lack: such a file is
	// read once more.
	Numbered bool `json:"numbered,omitempty"`
}

// Store is where the index of the saved songs is kept.
type Store struct {
	Path string
}

// Load returns the songs the index lists; a missing index is no songs.
func (s Store) Load() ([]Item, error) {
	b, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	out := items[:0]
	for _, it := range items {
		if strings.TrimSpace(it.Path) != "" && it.Track.Name != "" {
			out = append(out, it)
		}
	}
	return out, nil
}

// Save writes the index, replacing the one there atomically enough that a
// crash does not leave half of it.
func (s Store) Save(items []Item) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".downloads-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return moveFile(name, s.Path)
}

// Progress reports how much of a download has arrived, and how much is
// expected when that is known.
type Progress func(done, total int64)

// These let tests keep a download off the network.
var (
	songURL = netease.SongURLContext
	client  = &http.Client{Timeout: 2 * time.Hour}
)

// Fetch downloads tr at level into dir, and returns what was saved. A file
// that is not the audio it claims to be is thrown away rather than kept.
func Fetch(ctx context.Context, tr netease.Track, level, dir string, progress Progress) (Item, error) {
	if tr.LocalPath != "" {
		return Item{}, fmt.Errorf("这首歌已经是本地文件")
	}
	if tr.ProgramID != 0 {
		return Item{}, fmt.Errorf("播客节目不能作为歌曲下载")
	}
	st, err := songURL(ctx, tr.ID, level)
	if err != nil {
		return Item{}, err
	}
	ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(st.Type)), ".")
	if ext == "" {
		ext = extensionOf(st.URL)
	}
	if ext != "mp3" && ext != "flac" {
		return Item{}, fmt.Errorf("不支持的下载格式：%s", ext)
	}
	u, err := url.Parse(st.URL)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return Item{}, fmt.Errorf("下载地址不可用")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Item{}, err
	}
	path, err := uniquePath(dir, FileName(tr, ext), ext)
	if err != nil {
		return Item{}, err
	}
	tmp, err := os.CreateTemp(dir, ".Cloudy-*.part")
	if err != nil {
		return Item{}, err
	}
	tmpName := tmp.Name()
	kept := false
	defer func() {
		tmp.Close()
		if !kept {
			os.Remove(tmpName)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, st.URL, nil)
	if err != nil {
		return Item{}, err
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	req.Header.Set("Referer", "https://music.163.com/")
	// The progress is of the file, not of a compressed stream of it.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		return Item{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Item{}, fmt.Errorf("下载请求失败（%d）", resp.StatusCode)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = st.Size
	}
	var done int64
	last := time.Now()
	buf := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := tmp.Write(buf[:n]); err != nil {
				return Item{}, err
			}
			done += int64(n)
			if progress != nil && time.Since(last) >= 100*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return Item{}, readErr
		}
	}
	if progress != nil {
		progress(done, total)
	}
	if total > 0 && done != total {
		return Item{}, fmt.Errorf("下载不完整")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return Item{}, err
	}
	head := make([]byte, 12)
	n, _ := io.ReadFull(tmp, head)
	if !audioHead(head[:n], ext) {
		return Item{}, fmt.Errorf("下载的内容不是有效的音频")
	}
	if err := tmp.Close(); err != nil {
		return Item{}, err
	}
	if err := moveFile(tmpName, path); err != nil {
		return Item{}, err
	}
	kept = true
	return Item{Track: tr, Path: path, Level: st.Level, Size: done, At: time.Now()}, nil
}

// FileName is what a song is called on disk: its artist, then its title, with
// what a file system cannot hold taken out.
func FileName(tr netease.Track, ext string) string {
	artist := strings.TrimSpace(tr.ArtistNames())
	if artist == "" {
		artist = "未知歌手"
	}
	title := strings.TrimSpace(tr.Name)
	if title == "" {
		title = "未知歌曲"
	}
	name := clean(artist) + " - " + clean(title)
	name = strings.Trim(name, " ._")
	if name == "" || name == " - " {
		name = "未知歌曲"
	}
	return truncate(name, 120) + "." + strings.TrimPrefix(strings.ToLower(ext), ".")
}

// clean takes out the characters Windows, macOS and Linux refuse in a name,
// and the control characters none of them want.
func clean(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '-'
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
}

// truncate keeps at most n runes, so a long title does not pass what a file
// system takes.
func truncate(name string, n int) string {
	if utf8.RuneCountInString(name) <= n {
		return name
	}
	runes := []rune(name)
	return strings.TrimRight(string(runes[:n]), " .")
}

func extensionOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "mp3"
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(u.Path)), ".")
	if ext != "mp3" && ext != "flac" {
		return "mp3"
	}
	return ext
}

// uniquePath keeps a second song of the same name from writing over the
// first: it takes the next free " (2)", " (3)".
func uniquePath(dir, name, ext string) (string, error) {
	base := strings.TrimSuffix(name, "."+ext)
	path := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", err
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d).%s", base, i, ext))
	}
}

// audioHead reports whether the first bytes are those of the format asked
// for: an MP3 frame or its ID3 tag, or FLAC's mark.
func audioHead(head []byte, ext string) bool {
	switch ext {
	case "flac":
		return bytes.HasPrefix(head, []byte("fLaC"))
	case "mp3":
		if bytes.HasPrefix(head, []byte("ID3")) {
			return true
		}
		return len(head) >= 2 && head[0] == 0xff && head[1]&0xe0 == 0xe0
	}
	return false
}

// moveFile puts a finished file where it belongs. Rename is the way; where
// the file system or a sandbox refuses it, the bytes are copied instead, so
// a download still lands.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		in.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	inErr := in.Close()
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(to)
		return copyErr
	}
	if inErr != nil {
		os.Remove(to)
		return inErr
	}
	if closeErr != nil {
		os.Remove(to)
		return closeErr
	}
	return os.Remove(from)
}
