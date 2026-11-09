# AGENTS.md

## Code map

- `main.go` starts `internal/app`. The UI is MyGo, an immediate-mode GPU framework: every frame is built again from the app's state.
- `internal/netease`: the NetEase Cloud Music API (eapi encryption, login, playlists, song URLs, cloud drive). Pure Go but for `auth.go` (the keyring) and `eapi.go` (the device ID).
- `internal/lyrics`: parsing LRC and word-by-word lyrics, merging translations. `internal/local`: reading audio files' tags. `internal/download`: downloading a song with its tags, cover and lyrics.
- `internal/player`: playback, the queue, streaming and the cache, fades, loudness, the system's media controls.
- `internal/speech`: podcast captions, through sherpa-onnx loaded with purego. The C structs in `sherpa.go` mirror sherpa-onnx's layout: keep fields even when they look unused.
- `internal/app`: everything else, in one package around the `app` struct (`app.go`). Files are named for what they do; `ui_*.go` build views:
  - Pages: `ui_pages.go` routes; `ui_discovery.go`, `ui_daily.go`, `ui_style_daily.go`, `ui_playlist.go`, `ui_album.go`, `ui_artist.go`, `ui_user.go`, `ui_mine.go`, `ui_search.go`, `ui_podcast.go`, `ui_radio.go`, `ui_recent.go`, `ui_cloud.go`, `ui_downloads.go`.
  - Player: `ui_player.go` (the bar), `ui_nowplaying.go` and `ui_stage*.go` (Now Playing and its lyric styles), `fullscreen.go`, `ui_float.go` (desktop lyrics and the mini window).
  - Downloads: `downloads.go`, `downloads_more.go`. Cloud uploads: `cloud_upload.go`. Both queues are a `jobQueue` (`jobqueue.go`: one job at a time, failures kept to retry, saved across launches), shown by one window, `ui_job_queue.go`.
  - Settings: `settings.go` (the stored choices) and `ui_settings.go`; `session.go` and `history.go` keep the queue and what was played. Write such files with `writeFileAtomic`.
  - Windows integration: `tray.go`, `thumbbar.go`, `taskbar_progress.go`, `keys.go` and `ui_shortcuts.go` (global shortcuts), `menu.go`, `updates.go` and `ui_update.go`.
  - Account: `login.go`, `ui_account.go` (`signedIn` resets what is personal; `accountGen` drops replies meant for the account before).
  - macOS only, kept from the original project: `ui_notch.go`, `notch_*.go`.

### Threads

The app's state is read and changed on the main thread only, so it has no locks. Work that blocks (the network, the disk) runs in a goroutine and hands its result back with `a.post(func() { ... })`, which runs on the main thread and redraws. Bump a generation counter (`accountGen`, `jobQueue.gen`, ...) when starting work that a later action can make stale, and drop the result if it changed.

### The framework

MyGo is our fork, github.com/nya-owo/mygo, pinned by the `replace` in `go.mod`; the Windows taskbar thumbnail, tray, Windows 10 frame and update mirrors live there. Upstream egoist/mygo has moved far ahead: take single fixes from it by cherry-pick, don't merge it.

## Tests

Only write a test for behaviour that can actually break. If the code under test could change without the test noticing, or the test would fail on a harmless rewording, don't write it.

**Write tests for:**

- What input does: clicks, keys, drags, gestures and scrolling, and the state or effect they lead to (seeking when the progress bar is let go, Space after opening Now Playing, back and forward, flinging the cover, liking a song, leaving a radio).
- Logic that doesn't need a view: parsing lyrics, formatting times and counts, the player's queue, seeking, fading and cache, decoding streams and images.
- A regression you just fixed. Say in the test's comment what went wrong.
- Frame budgets, in `perf_test.go`, so idle views don't start asking for frames they don't need.

**Don't write tests that:**

- Check that a list of labels shows on screen. That repeats the view code and breaks whenever the wording changes.
- Check default settings or the order of rows in a form.
- Test MyGo itself, for example that `AnimateTo` or a spring moves from 0 to 1.
- Depend on the wall clock. `ui.NewTester` starts its clock at the current time, so anything with a phase taken from the clock passes or fails depending on when the test runs. Also, `FramePending` ignores frames waiting on `After` or `Painter.After`.

**How:**

- Build views with `ui.NewTester` and set up the app with `newTestApp` (in `internal/app/helpers_test.go`). Find elements by their accessibility label, then `Click`, `RightClick`, `Press`/`Move`/`Release` or `Key`, and check the app's state afterwards.
- Swap out the network and the clipboard through package variables such as `likeSong` and `copyText`, and restore the real one with `defer`.
- Live tests that call NetEase skip unless `LIVE` is set.
- Look at visuals instead of asserting on them. The `NM_FRAMES` tests save PNGs into that directory and skip when it isn't set. Check pixels only for coverage, such as whether a layer still covers the window.
- Start each test with a one-sentence comment saying the behaviour it checks, in the style of the code.
- Run `go test ./internal/...` before committing, and run a new UI test with `-count=5`. Fix or delete a flaky test; don't retry it.
